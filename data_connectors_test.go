package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// status and testResult are opt-in (?with=); the status object omits error on
// success, and an absent status means the latest snapshot has no record.
func TestDataConnectorsReads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/networks/N1/data-connectors?with=status%2CtestResult":
			_, _ = io.WriteString(w, `{"connectors":[
			  {"name":"ipam","baseUrl":"https://ipam.example.com","credentialId":"C1","extraHeaders":{"accept":"application/json"},
			   "endpoints":[{"name":"subnets","path":"/api/v2/subnets","paginationModel":{"type":"OFFSET","parameterName":"page"}}],
			   "testResult":{"startedAt":"2026-10-02T10:00:00Z","endedAt":"2026-10-02T10:00:03Z","error":"AUTHENTICATION_FAILED","errorDesc":"401"},
			   "status":{},"createdBy":"mary","createdAt":"2026-10-01T09:00:00Z"},
			  {"name":"cmdb","baseUrl":"http://cmdb","endpoints":[{"name":"ci","path":"/ci"}],"collect":false}],
			  "snapshotId":"S9"}`)
		case "/api/networks/N1/data-connectors/cmdb":
			_, _ = io.WriteString(w, `{"name":"cmdb","baseUrl":"http://cmdb","endpoints":[{"name":"ci","path":"/ci"}],"collect":false}`)
		case "/api/networks/N1/data-connectors/gone":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	connectors := newTestClient(t, server.URL).DataConnectors
	ctx := context.Background()

	list, _, err := connectors.List(ctx, "N1", DataConnectorReadOptions{Status: true, TestResult: true})
	if err != nil || list.SnapshotID != "S9" || len(list.Connectors) != 2 {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	ipam := list.Connectors[0]
	if ipam.CredentialID != "C1" || len(ipam.Endpoints[0].PaginationModel) == 0 || ipam.TestResult.Error != "AUTHENTICATION_FAILED" || ipam.Status == nil || ipam.Status.Error != "" || !ipam.Collects() || ipam.CreatedBy != "mary" {
		t.Fatalf("ipam = %+v", ipam)
	}
	if cmdb := list.Connectors[1]; cmdb.Collects() || cmdb.Status != nil {
		t.Fatalf("cmdb = %+v", cmdb)
	}
	if got, _, err := connectors.Get(ctx, "N1", "cmdb", DataConnectorReadOptions{}); err != nil || got.Name != "cmdb" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if got, _, err := connectors.Get(ctx, "N1", "gone", DataConnectorReadOptions{}); got != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", got, err)
	}
}

// A patch sends only what it states; a cleared reference is an explicit
// null (HttpDataConnectorPatch: "Null if ..."), which omitting cannot say.
func TestDataConnectorsWrites(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch r.Method {
		case http.MethodPost:
			if r.URL.Query().Get("action") == "test" {
				_, _ = io.WriteString(w, `{"startedAt":"2026-10-02T10:00:00Z","endedAt":"2026-10-02T10:00:02Z","error":"CONNECTION_TIMEOUT","errorDesc":"timed out"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"name":"ipam","baseUrl":"https://ipam","endpoints":[{"name":"s","path":"/s"}]}`)
		case http.MethodPatch:
			_, _ = io.WriteString(w, `{"name":"ipam","baseUrl":"https://ipam","endpoints":[{"name":"s","path":"/s"}],"collect":false}`)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	connectors := newTestClient(t, server.URL).DataConnectors
	ctx := context.Background()

	if _, _, err := connectors.Add(ctx, "N1", NewDataConnector{Name: "ipam", BaseURL: "https://ipam", Endpoints: []HTTPEndpoint{{Name: "s", Path: "/s"}}}); err != nil ||
		calls[0] != `POST /api/networks/N1/data-connectors {"name":"ipam","baseUrl":"https://ipam","endpoints":[{"name":"s","path":"/s"}]}` {
		t.Fatalf("Add: %v %q", err, calls)
	}
	if _, _, err := connectors.Update(ctx, "N1", "ipam", DataConnectorPatch{Collect: Ptr(false), CredentialID: Ptr(""), CollectorID: Ptr("COL2")}); err != nil ||
		calls[1] != `PATCH /api/networks/N1/data-connectors/ipam {"collect":false,"collectorId":"COL2","credentialId":null}` {
		t.Fatalf("Update: %v %q", err, calls[1])
	}
	result, _, err := connectors.Test(ctx, "N1", "ipam")
	if err != nil || result.Error != "CONNECTION_TIMEOUT" || calls[2] != "POST /api/networks/N1/data-connectors/ipam?action=test " {
		t.Fatalf("Test() = %+v, %v; %q", result, err, calls[2])
	}
	if _, err := connectors.Delete(ctx, "N1", "ipam"); err != nil {
		t.Fatalf("Delete of an absent connector must succeed: %v", err)
	}
	if _, _, err := connectors.Update(ctx, "N1", "ipam", DataConnectorPatch{}); err == nil {
		t.Fatal("an empty patch must be refused locally")
	}
	if _, _, err := connectors.Add(ctx, "N1", NewDataConnector{Name: "x", BaseURL: "https://x"}); err == nil {
		t.Fatal("a connector without endpoints must be refused locally")
	}
	if len(calls) != 4 {
		t.Fatalf("refused calls must not reach the wire: %q", calls)
	}
}

// Test blocks server-side for up to 60 s; a short client timeout is raised so
// a slow success does not read as a failure, but an unlimited one stays.
func TestDataConnectorsTestTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, `{"startedAt":"2026-10-02T10:00:00Z","endedAt":"2026-10-02T10:00:01Z"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	if result, _, err := client.DataConnectors.Test(context.Background(), "N1", "ipam"); err != nil || result.Error != "" {
		t.Fatalf("Test() under a 50ms client timeout = %+v, %v", result, err)
	}
}
