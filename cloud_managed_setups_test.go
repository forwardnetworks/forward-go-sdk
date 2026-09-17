package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestCloudManagedSetupsCreateMistSendsForwardsShape pins the wire shape
// CloudManagedSetupController accepts for type=MIST: the query param on the
// path, no org id in the body (Forward discovers it from GET /api/v1/self),
// and omitted optionals omitted rather than sent as zero values.
func TestCloudManagedSetupsCreateMistSendsForwardsShape(t *testing.T) {
	var gotPath, gotQuery string
	var gotBody map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"name":"skyforge-mist","type":"MIST","region":"GLOBAL_01","apiKeyId":"H-9"}`))
	})
	out, _, err := c.CloudManagedSetups.CreateMist(context.Background(), "net-1", NewMistSetup{
		Name: "skyforge-mist", Region: MistRegionGlobal01, APIKeyID: "H-9", CollectorID: "col-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/api/networks/net-1/cloud-managed-setups") || gotQuery != "type=MIST" {
		t.Fatalf("path %s?%s, want .../cloud-managed-setups?type=MIST", gotPath, gotQuery)
	}
	for k, want := range map[string]any{"name": "skyforge-mist", "region": "GLOBAL_01", "apiKeyId": "H-9", "collectorId": "col-1"} {
		if gotBody[k] != want {
			t.Errorf("body.%s = %v, want %v", k, gotBody[k], want)
		}
	}
	for _, absent := range []string{"orgId", "org_id", "hosts", "collect", "concurrency"} {
		if _, present := gotBody[absent]; present {
			t.Errorf("body carries %q; Forward derives the org itself and empty optionals must be omitted", absent)
		}
	}
	if out.Name != "skyforge-mist" || out.Region != MistRegionGlobal01 {
		t.Fatalf("decoded %+v", out)
	}
}

func TestCloudManagedSetupsCreateMistRefusesIncompleteInput(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("no request expected") })
	for name, in := range map[string]NewMistSetup{
		"no name":   {Region: MistRegionGlobal01, APIKeyID: "H-1"},
		"no region": {Name: "x", APIKeyID: "H-1"},
		"no key":    {Name: "x", Region: MistRegionGlobal01},
	} {
		if _, _, err := c.CloudManagedSetups.CreateMist(context.Background(), "net-1", in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestCloudManagedSetupsDiscoverAndDeletePaths(t *testing.T) {
	var seen []string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNotFound) // absent already == success
			return
		}
		// Verbatim live discover body, 2026-09-17.
		_, _ = w.Write([]byte(`{"hosts":[{"name":"0250aa010001","model":"AP43","displayName":"SMOKE-AP-1"}]}`))
	})
	out, _, err := c.CloudManagedSetups.DiscoverMist(context.Background(), "net-1", "skyforge-mist")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hosts) != 1 || out.Hosts[0].Name != "0250aa010001" || out.Hosts[0].DisplayName != "SMOKE-AP-1" {
		t.Fatalf("discover decoded %+v", out)
	}
	if _, err := c.CloudManagedSetups.DeleteMist(context.Background(), "net-1", "skyforge-mist"); err != nil {
		t.Fatalf("delete 404 must be success: %v", err)
	}
	want := []string{
		"POST /api/networks/net-1/cloud-managed-setups/skyforge-mist?action=discover&type=MIST",
		"DELETE /api/networks/net-1/cloud-managed-setups/skyforge-mist?type=MIST",
	}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("requests %v, want %v", seen, want)
	}
}

func TestCloudManagedSetupsListMistAcceptsEnvelopes(t *testing.T) {
	for name, body := range map[string]string{
		"setups wrapper": `{"setups":[{"name":"m","region":"GLOBAL_01"}]}`,
		"bare array":     `[{"name":"m","region":"GLOBAL_01"}]`,
	} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "type=MIST" {
				t.Errorf("%s: query %q, want type=MIST", name, r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(body))
		})
		got, _, err := c.CloudManagedSetups.ListMist(context.Background(), "net-1")
		if err != nil || len(got) != 1 || got[0].Name != "m" {
			t.Fatalf("%s: %v %+v", name, err, got)
		}
	}
}

// A setup's hosts filter is written as MAC strings; the flexible ref also
// accepts the discovered-host object shape so a future read-side change
// cannot break decoding. The verbatim live setup (2026-09-17) is pinned too.
func TestMistSetupHostsDecodeAsStringsOrObjects(t *testing.T) {
	var live MistSetup
	if err := json.Unmarshal([]byte(`{"name":"skyforge-mist-smoke","apiKeyId":"AK-0","region":"GLOBAL_01","collect":true,"hosts":[],"testResult":{"savedAt":"2026-09-17T11:47:14.021Z","discoveredHosts":[{"name":"0250aa010001","model":"AP43","displayName":"SMOKE-AP-1"}]},"type":"MIST"}`), &live); err != nil || live.TestResult == nil || live.TestResult.SavedAt == "" || len(live.TestResult.DiscoveredHosts) != 1 {
		t.Fatalf("live setup: %v %+v", err, live)
	}
	var a, b MistSetup
	if err := json.Unmarshal([]byte(`{"name":"m","hosts":["0250aa010001"]}`), &a); err != nil || len(a.Hosts) != 1 || a.Hosts[0].Name != "0250aa010001" {
		t.Fatalf("strings: %v %+v", err, a.Hosts)
	}
	if err := json.Unmarshal([]byte(`{"name":"m","hosts":[{"name":"0250aa010001","model":"AP43","displayName":"SMOKE-AP-1"}]}`), &b); err != nil || len(b.Hosts) != 1 || b.Hosts[0].DisplayName != "SMOKE-AP-1" {
		t.Fatalf("objects: %v %+v", err, b.Hosts)
	}
}
