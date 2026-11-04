package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Every call must hit the exact Forward route, including the query that picks
// the handler: Spring chooses between GET .../sase-setups and its
// ?view=newlyDiscovered twin, and between POST create and POST ?action=test,
// by params, so a dropped query would silently run the wrong handler.
func TestSaseSetupsWirePaths(t *testing.T) {
	var seen []string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch {
		case r.Method == http.MethodPost && r.URL.RawQuery == "action=test", r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.RawQuery == "view=newlyDiscovered":
			_, _ = w.Write([]byte(`{"discoveredAppConnectors":[{"setupName":"z1","connectorId":"c9","name":"ac-9","host":"10.0.0.9","locationName":"DC","latitude":1.5,"longitude":-2.5}]}`))
		case r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "/sase-setups/"):
			_, _ = w.Write([]byte(`{"setups":[{"type":"ZSCALER","name":"z1","baseUrl":"https://config.private.zscaler.com","appConnectorSetups":[]}]}`))
		default:
			_, _ = w.Write([]byte(`{"type":"ZSCALER","name":"z1","baseUrl":"https://config.private.zscaler.com","appConnectorSetups":[]}`))
		}
	})
	ctx := context.Background()
	if got, _, err := c.SaseSetups.List(ctx, "n1"); err != nil || len(got) != 1 || got[0].Name != "z1" {
		t.Fatalf("list: %v %+v", err, got)
	}
	nd, _, err := c.SaseSetups.NewlyDiscovered(ctx, "n1")
	if err != nil || len(nd) != 1 || nd[0].SetupName != "z1" || nd[0].ConnectorID != "c9" || nd[0].Latitude == nil || *nd[0].Longitude != -2.5 {
		t.Fatalf("newly discovered: %v %+v", err, nd)
	}
	if _, _, err := c.SaseSetups.Get(ctx, "n1", "z 1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SaseSetups.Create(ctx, "n1", ZscalerSetup{Name: "z1", BaseURL: "https://x"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.SaseSetups.Patch(ctx, "n1", "z1", ZscalerSetupPatch{Collect: Ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaseSetups.Test(ctx, "n1", ZscalerConnectivityTest{ID: "z1", BaseURL: "https://x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaseSetups.TestSetup(ctx, "n1", "z1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaseSetups.TestConnectors(ctx, "n1", "z1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaseSetups.TestConnector(ctx, "n1", "z1", "c/1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaseSetups.Delete(ctx, "n1", "z1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /api/networks/n1/sase-setups?",
		"GET /api/networks/n1/sase-setups?view=newlyDiscovered",
		"GET /api/networks/n1/sase-setups/z%201?",
		"POST /api/networks/n1/sase-setups?",
		"PATCH /api/networks/n1/sase-setups/z1?",
		"POST /api/networks/n1/sase-setups?action=test",
		"POST /api/networks/n1/sase-setups/z1?action=test",
		"POST /api/networks/n1/sase-setups/z1/connectors?action=test",
		"POST /api/networks/n1/sase-setups/z1/connectors/c%2F1?action=test",
		"DELETE /api/networks/n1/sase-setups/z1?",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

// Forward picks the setup subtype by "type" and fills a missing appConnectorSetups
// itself, but Patch and Create must send type or the body is refused; and a
// patch must leave unset fields out entirely, since an absent key means "keep".
func TestSaseSetupsBodies(t *testing.T) {
	var bodies []map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		_, _ = w.Write([]byte(`{"type":"ZSCALER","name":"z1","baseUrl":"https://x","appConnectorSetups":[]}`))
	})
	ctx := context.Background()
	port := 22
	if _, _, err := c.SaseSetups.Create(ctx, "n1", ZscalerSetup{
		Name: "z1", BaseURL: "https://x", HTTPCredID: "H-1", CollectorID: "C7",
		AppConnectorSetups: []AppConnectorSetup{{ConnectorID: "c1", Name: "ac1", Host: "10.0.0.1", CollectionType: AppConnectorCollectionSSH, Port: &port, CredentialID: "CL-1"}},
		ScimConfigs:        []ScimConfig{{URL: "https://scim.zscaler.com/scim/1/2/v2", HTTPCredID: "AK-2"}},
	}); err != nil {
		t.Fatal(err)
	}
	empty := []AppConnectorSetup{}
	if _, _, err := c.SaseSetups.Patch(ctx, "n1", "z1", ZscalerSetupPatch{AppConnectorSetups: &empty}); err != nil {
		t.Fatal(err)
	}
	cr := bodies[0]
	if cr["type"] != "ZSCALER" || cr["collectorId"] != "C7" || cr["httpCredId"] != "H-1" {
		t.Fatalf("create body %v", cr)
	}
	ac := cr["appConnectorSetups"].([]any)[0].(map[string]any)
	if ac["collectionType"] != "SSH" || ac["port"] != float64(22) || ac["credentialId"] != "CL-1" {
		t.Fatalf("connector body %v", ac)
	}
	if _, has := ac["testResult"]; has {
		t.Fatalf("unset testResult must not be sent: %v", ac)
	}
	pb := bodies[1]
	if len(pb) != 2 || pb["type"] != "ZSCALER" || len(pb["appConnectorSetups"].([]any)) != 0 {
		t.Fatalf("patch must send only type and the cleared list, got %v", pb)
	}
}

// A read carries the test results the collector saved. The error is Forward's
// enum constant name and must survive verbatim.
func TestSaseSetupsDecodeTestResults(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"ZSCALER","name":"z1","baseUrl":"https://x","collectorId":"C7",
"testResult":{"savedAt":"2026-11-04T10:00:00Z","error":"AUTHENTICATION_FAILED"},
"appConnectorSetups":[{"connectorId":"c1","name":"ac1","host":"h","collectionType":"SSH","collect":true,
"testResult":{"savedAt":"2026-11-04T10:00:01Z"}}],
"attribution":{"createdBy":"craig","createdById":"U1","createdAt":"2026-11-04T09:00:00Z"}}`))
	})
	got, _, err := c.SaseSetups.Get(context.Background(), "n1", "z1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TestResult == nil || got.TestResult.Error != "AUTHENTICATION_FAILED" || got.CollectorID != "C7" {
		t.Fatalf("setup %+v", got)
	}
	if r := got.AppConnectorSetups[0].TestResult; r == nil || r.Error != "" || r.SavedAt == "" {
		t.Fatalf("connector result %+v", r)
	}
	if got.Attribution == nil || got.Attribution.CreatedBy != "craig" {
		t.Fatalf("attribution %+v", got.Attribution)
	}
}

// Delete treats 404 as success; every other refusal stays an error.
func TestSaseSetupsDeleteAndValidation(t *testing.T) {
	requests := 0
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/api/networks/n1/sase-setups/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/api/networks/n1/sase-setups/unserved":
			// Spring's answer for a route this deployment does not map: the setup
			// was not "already absent", the call did nothing.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No endpoint DELETE /api/networks/n1/sase-setups/unserved."}`))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	ctx := context.Background()
	if _, err := c.SaseSetups.Delete(ctx, "n1", "unserved"); err == nil {
		t.Fatal("a route-not-served 404 must stay an error")
	}
	if _, err := c.SaseSetups.Delete(ctx, "n1", "gone"); err != nil {
		t.Fatalf("404 must be success: %v", err)
	}
	if _, err := c.SaseSetups.Delete(ctx, "n1", "kept"); err == nil {
		t.Fatal("403 must stay an error")
	}
	requests = 0
	for name, err := range map[string]error{
		"no name":    func() error { _, _, e := c.SaseSetups.Create(ctx, "n1", ZscalerSetup{BaseURL: "https://x"}); return e }(),
		"no baseUrl": func() error { _, _, e := c.SaseSetups.Create(ctx, "n1", ZscalerSetup{Name: "z"}); return e }(),
		"bad type": func() error {
			_, _, e := c.SaseSetups.Create(ctx, "n1", ZscalerSetup{Name: "z", BaseURL: "https://x", AppConnectorSetups: []AppConnectorSetup{{ConnectorID: "c", Name: "n", Host: "h", CollectionType: "TELNET"}}})
			return e
		}(),
		"test no id": func() error {
			_, e := c.SaseSetups.Test(ctx, "n1", ZscalerConnectivityTest{BaseURL: "https://x"})
			return e
		}(),
		"no connector": func() error { _, e := c.SaseSetups.TestConnector(ctx, "n1", "z", ""); return e }(),
	} {
		if err == nil {
			t.Errorf("%s: want a client-side refusal", name)
		}
	}
	// A refusal must happen before the wire: a server that answers 403 to
	// everything would otherwise make a missing check look like a working one.
	if requests != 0 {
		t.Fatalf("%d invalid calls reached the server", requests)
	}
}
