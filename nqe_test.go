package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNQESynchronousAndAsynchronousRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/nqe":
			if got := r.URL.Query().Get("networkId"); got != "network-1" {
				t.Errorf("networkId = %q", got)
			}
			var body NQEQueryRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query == "" {
				t.Errorf("query body = %#v, %v", body, err)
			}
			_, _ = io.WriteString(w, `{"snapshotId":"snapshot-1","items":[{"Name":"r1"}],"totalNumItems":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/nqe-executions":
			if got := r.URL.Query().Get("snapshotId"); got != "snapshot-1" {
				t.Errorf("snapshotId = %q", got)
			}
			_, _ = io.WriteString(w, `{"executionKey":"exec/1","status":"SUBMITTED"}`)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/networks/network-1/nqe-executions/exec%2F1":
			_, _ = io.WriteString(w, `{"status":"COMPLETED","outcome":"OK","rowsProduced":1}`)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/networks/network-1/nqe-executions/exec%2F1/result":
			if r.Header.Get("Accept") == "application/jsonl" {
				_, _ = io.WriteString(w, "{\"Name\":\"r1\"}\n")
				return
			}
			if r.URL.Query().Get("offset") != "2" || r.URL.Query().Get("limit") != "10" {
				t.Errorf("result query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"snapshotId":"snapshot-1","items":[{"Name":"r1"}],"totalNumItems":1}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	result, _, err := client.NQE.Run(ctx, "network-1", "", NQEQueryRequest{Query: "foreach d in network.devices select { Name: d.name }"})
	if err != nil || result.TotalNumItems != 1 || len(result.Items) != 1 {
		t.Fatalf("NQE.Run() = %#v, %v", result, err)
	}
	execution, _, err := client.NQE.Start(ctx, "network-1", "snapshot-1", NQEExecutionRequest{QueryID: "FQ_1"})
	if err != nil || execution.ExecutionKey != "exec/1" {
		t.Fatalf("NQE.Start() = %#v, %v", execution, err)
	}
	execution, _, err = client.NQE.Status(ctx, "network-1", execution.ExecutionKey)
	if err != nil || execution.ExecutionKey != "exec/1" || execution.Outcome != "OK" || execution.RowsProduced == nil || *execution.RowsProduced != 1 {
		t.Fatalf("NQE.Status() = %#v, %v", execution, err)
	}
	offset, limit := int32(2), int32(10)
	result, _, err = client.NQE.Result(ctx, "network-1", "exec/1", NQEResultOptions{Offset: &offset, Limit: &limit})
	if err != nil || result.SnapshotID != "snapshot-1" {
		t.Fatalf("NQE.Result() = %#v, %v", result, err)
	}
	var jsonLines bytes.Buffer
	if _, err := client.NQE.ResultJSONLines(ctx, "network-1", "exec/1", NQEResultOptions{}, &jsonLines); err != nil {
		t.Fatalf("NQE.ResultJSONLines() error = %v", err)
	}
	if got := jsonLines.String(); got != "{\"Name\":\"r1\"}\n" {
		t.Fatalf("JSON Lines = %q", got)
	}
}

func TestNQEDiffAndLibraryRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nqe-diffs/before/after":
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, exists := body["parameters"]; exists {
				t.Error("published NQE diff body must not include parameters")
			}
			_, _ = io.WriteString(w, `{"rows":[{"type":"ADDED","after":{"Name":"r1"}}],"totalNumRows":1}`)
		case "/api/nqe/queries":
			if got := r.URL.Query().Get("dir"); got != "/L3/" {
				t.Errorf("dir = %q", got)
			}
			_, _ = io.WriteString(w, `[{"queryId":"FQ_1","repository":"ORG","path":"/L3/Routes","intent":"routes"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	diff, _, err := client.NQE.Diff(context.Background(), "before", "after", NQEDiffRequest{QueryID: "FQ_1"})
	if err != nil || diff.TotalNumRows != 1 || diff.Rows[0].After == nil {
		t.Fatalf("NQE.Diff() = %#v, %v", diff, err)
	}
	queries, _, err := client.NQE.ListQueries(context.Background(), "/L3/")
	if err != nil || len(queries) != 1 || queries[0].QueryID != "FQ_1" {
		t.Fatalf("NQE.ListQueries() = %#v, %v", queries, err)
	}
}

func TestNQEInputValidation(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://fwd.example")
	ctx := context.Background()
	for _, request := range []NQEQueryRequest{{}, {Query: "q", QueryID: "id"}} {
		if _, _, err := client.NQE.Run(ctx, "network", "", request); err == nil {
			t.Fatalf("NQE.Run(%#v) error = nil", request)
		}
	}
}
