package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const traceFacetsBody = `{"coveredTopo":{"nodes":[]},"facets":{"pathGroups":[
 {"traceType":"VALID","paths":[
  {"path":["a","b"],"groupSignatureHops":[0,1],"behaviorSample":{"processingStages":[["INPUT"],["INPUT"]],"extra":{"keep":1}}},
  {"path":["a","c"],"groupSignatureHops":[0,1],"behaviorSample":{"processingStages":[["INPUT"],["INPUT"]]}}]},
 {"traceType":"DROPPED","paths":[{"path":["a"],"behaviorSample":{"aclDropHops":[0]}}]}],
 "outcomes":{"DROPPED":["a"]}},"totalResults":{"value":3,"type":"EXACT"},"timedOut":false}`

const traceBody = `{"traceType":"VALID","statefulness":"STATELESS","behavior":{"processingStages":[["INPUT","IPV4","SNAT"]]},
 "ingressHeaders":{"IPV4":{"ipv4_src":"10.0.0.1"}},"pathDescriptor":"abc",
 "deviceHops":[{"device":"fw","inPorts":["fw g0"],"outPorts":["fw g1"],
  "headers":{"IPV4":{"ipv4_src":"1.2.3.4"}},
  "networkFunctions":{"NAT":[{"type":"SNAT","fileLines":{"fw,configuration.txt":[{"start":3,"end":4}]},
   "actionBuckets":[{"actions":[{"type":"SET_FIELD","field":"ipv4_src","value":"Egress Interface IP"}]}],"processingStageType":"SNAT"}]}}]}`

// A trace must send every header filter under from.headers (a destination under
// "to" returned no paths live), put the snapshot in the query string, and send a
// selected path's behavior sample back byte for byte, because Forward uses it to
// pick the path.
func TestTraceSendsFiltersAndSelectionVerbatim(t *testing.T) {
	var uris []string
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		uris, bodies = append(uris, r.Method+" "+r.URL.RequestURI()), append(bodies, body)
		if strings.Contains(r.URL.Path, "trace-facets") {
			_, _ = w.Write([]byte(traceFacetsBody))
			return
		}
		_, _ = w.Write([]byte(traceBody))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	tcp := 6
	q := TraceQuery{SrcIP: "10.0.0.1", DstIP: "8.8.8.8", IPProto: &tcp, DstPort: "443"}

	facets, _, err := c.Trace.Facets(context.Background(), "N1", "S1", q, TraceFacetsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(facets.PathGroups) != 2 || len(facets.PathGroups[0].Paths) != 2 || facets.Outcomes["DROPPED"][0] != "a" {
		t.Fatalf("facets decoded wrong: %+v", facets)
	}
	sel, ok := facets.PathGroups[0].Selection(0)
	if !ok {
		t.Fatal("no selection")
	}
	got, _, err := c.Trace.Trace(context.Background(), "N1", "S1", q, sel)
	if err != nil {
		t.Fatal(err)
	}

	if uris[0] != "POST /api/networks/N1/trace-facets?snapshotId=S1" || uris[1] != "POST /api/networks/N1/trace?snapshotId=S1" {
		t.Fatalf("uris: %v", uris)
	}
	from := bodies[0]["filters"].(map[string]any)["from"].(map[string]any)
	if from["location"].(map[string]any)["type"] != "SubnetLocationFilter" {
		t.Fatalf("location: %v", from["location"])
	}
	hdr, _ := json.Marshal(from["headers"])
	for _, want := range []string{`"ip_proto":["6"]`, `"tp_dst":["443"]`, `"ipv4_dst":["8.8.8.8"]`} {
		if !strings.Contains(string(hdr), want) {
			t.Fatalf("headers %s lack %s", hdr, want)
		}
	}
	if _, bad := bodies[0]["filters"].(map[string]any)["to"]; bad {
		t.Fatal("destination must not be sent under to")
	}
	picked := bodies[1]["selectedFacets"].(map[string]any)["paths"].([]any)[0].(map[string]any)
	behavior, _ := json.Marshal(picked["behavior"])
	if picked["traceType"] != "VALID" || !strings.Contains(string(behavior), `"extra":{"keep":1}`) {
		t.Fatalf("selection not sent back intact: %v", picked)
	}
	nat := got.DeviceHops[0].NetworkFunctions["NAT"][0]
	if nat.Type != "SNAT" || nat.ActionBuckets[0].Actions[0].Field != "ipv4_src" || nat.FileLines["fw,configuration.txt"][0].End != 4 {
		t.Fatalf("hop decoded wrong: %+v", nat)
	}
	if got.DeviceHops[0].Headers["IPV4"]["ipv4_src"] != "1.2.3.4" || got.IngressHeaders["IPV4"]["ipv4_src"] != "10.0.0.1" {
		t.Fatalf("headers: %+v", got)
	}
	if string(got.Raw) != traceBody {
		t.Fatal("Raw must be the response body unchanged")
	}
}

// With only a starting device the query must be a device filter, so the facets
// enumerate sources; with neither a source nor a device, or ports on a
// non-TCP/UDP flow, the call must be refused before any request.
func TestTraceQueryValidation(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(traceFacetsBody))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	icmp := 1
	for name, q := range map[string]TraceQuery{
		"no destination": {SrcIP: "10.0.0.1"},
		"no source":      {DstIP: "10.0.0.2"},
		"port on icmp":   {SrcIP: "10.0.0.1", DstIP: "10.0.0.2", IPProto: &icmp, DstPort: "1"},
		"ipv6":           {SrcIP: "10.0.0.1", DstIP: "2001:db8::1"},
	} {
		if _, _, err := c.Trace.Facets(context.Background(), "N1", "", q, TraceFacetsOptions{}); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if hits != 0 {
		t.Fatalf("%d requests reached the server", hits)
	}
	loc := TraceQuery{From: "dev1", DstIP: "8.8.8.8"}.filters()["from"].(map[string]any)["location"].(map[string]any)
	if loc["type"] != "DeviceFilter" {
		t.Fatalf("location: %v", loc)
	}
}

// Explain returns one entry per listed path, ECMP alternatives included, and one
// failing trace is reported on its own entry instead of losing the others.
func TestTraceExplainReturnsEveryPathAndKeepsPartialFailures(t *testing.T) {
	traces := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "trace-facets") {
			_, _ = w.Write([]byte(traceFacetsBody))
			return
		}
		traces++
		if traces == 2 {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(traceBody))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ex, _, err := c.Trace.Explain(context.Background(), "N1", "", TraceQuery{SrcIP: "10.0.0.1", DstIP: "8.8.8.8"})
	if err != nil {
		t.Fatal(err)
	}
	paths, truncated := ex.Paths, ex.Truncated
	if truncated || len(paths) != 3 || paths[0].ID != "g0.p0" || paths[1].ID != "g0.p1" || paths[2].ID != "g1.p0" || paths[2].TraceType != "DROPPED" {
		t.Fatalf("paths: %+v", paths)
	}
	if paths[0].Trace == nil || paths[0].Descriptor != "abc" || paths[1].Trace != nil || paths[1].Error == "" || paths[2].Trace == nil {
		t.Fatalf("partial failure not kept: %+v", paths)
	}
}

func manyPathsFacets(n int) string {
	paths := make([]string, n)
	for i := range paths {
		paths[i] = fmt.Sprintf(`{"path":["a","d%d"],"behaviorSample":{}}`, i)
	}
	return `{"facets":{"pathGroups":[{"traceType":"VALID","paths":[` + strings.Join(paths, ",") + `]}]},"totalResults":{"value":1,"type":"LOWER_BOUND"},"timedOut":true}`
}

// Exactly ExplainLimit paths is complete; one more is reported as truncated. Forward's own
// partial-result signals (a timed-out or capped facets answer) must reach the caller instead
// of passing for a full list.
func TestTraceExplainLimitAndPartialSignals(t *testing.T) {
	for n, wantTruncated := range map[int]bool{ExplainLimit: false, ExplainLimit + 1: true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "trace-facets") {
				_, _ = w.Write([]byte(manyPathsFacets(n)))
				return
			}
			_, _ = w.Write([]byte(traceBody))
		}))
		c := newTestClient(t, server.URL)
		ex, _, err := c.Trace.Explain(context.Background(), "N1", "", TraceQuery{SrcIP: "10.0.0.1", DstIP: "8.8.8.8"})
		server.Close()
		if err != nil || len(ex.Paths) != ExplainLimit || ex.Truncated != wantTruncated || !ex.FacetsTimedOut || !ex.ResultsLowerBound {
			t.Fatalf("n=%d: %+v %v", n, ex, err)
		}
	}
}

// A source that is at several places comes back with possibleIpLocations and no path groups; that is an
// instruction to name a device, not "no path", and a trace for which Forward found no flow is an error.
func TestTraceAmbiguousSourceAndEmptyTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "trace-facets") {
			_, _ = w.Write([]byte(`{"possibleIpLocations":[{"device":"a"},{"device":"b"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"timedOut":true}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	q := TraceQuery{SrcIP: "10.0.0.1", DstIP: "8.8.8.8"}
	if _, _, err := c.Trace.Explain(context.Background(), "N1", "", q); !errors.Is(err, ErrTraceAmbiguousSource) {
		t.Fatalf("want ErrTraceAmbiguousSource, got %v", err)
	}
	if _, _, err := c.Trace.Trace(context.Background(), "N1", "", q, TraceSelection{Path: []string{"a"}, TraceType: "VALID"}); !errors.Is(err, ErrTraceEmpty) {
		t.Fatalf("want ErrTraceEmpty, got %v", err)
	}
}

// An error that would hit every path (no permission) stops Explain instead of sending up to 50 more requests.
func TestTraceExplainStopsOnErrorsThatHitEveryPath(t *testing.T) {
	traces := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "trace-facets") {
			_, _ = w.Write([]byte(manyPathsFacets(5)))
			return
		}
		traces++
		http.Error(w, `{"message":"no"}`, http.StatusForbidden)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	if _, _, err := c.Trace.Explain(context.Background(), "N1", "", TraceQuery{SrcIP: "10.0.0.1", DstIP: "8.8.8.8"}); !IsStatus(err, http.StatusForbidden) || traces != 1 {
		t.Fatalf("err=%v traces=%d", err, traces)
	}
}

// The options reach the wire as the server reads them: maxResults in the body, maxFacetValues in the query,
// and a source plus a starting device resolve the address on that device.
func TestTraceFacetsOptionsAndDeviceScopedSource(t *testing.T) {
	var uri string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(traceFacetsBody))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	_, _, err := c.Trace.Facets(context.Background(), "N1", "", TraceQuery{SrcIP: "10.0.0.1", From: "dev1", DstIP: "8.8.8.8"}, TraceFacetsOptions{MaxResults: 7, MaxFacetValues: 9})
	if err != nil {
		t.Fatal(err)
	}
	loc := body["filters"].(map[string]any)["from"].(map[string]any)["location"].(map[string]any)
	if uri != "/api/networks/N1/trace-facets?maxFacetValues=9" || body["maxResults"] != float64(7) || loc["device"] != "dev1" {
		t.Fatalf("uri=%s body=%v", uri, body)
	}
}
