package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStageBGPAdvertisementsIsOneOrderedBulkAdd(t *testing.T) {
	var calls int
	var path, query string
	var body []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		path, query = r.URL.Path, r.URL.RawQuery
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s content-type %q", r.Method, r.Header.Get("Content-Type"))
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not a JSON array: %s", raw)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	pref := int64(100)
	ads := []BGPAdvertisement{
		{Prefix: "10.200.0.0/24", ExternalPeer: "10.253.1.1", Type: "EBGP", Origin: "IGP", VRF: "default", LocalPref: &pref},
		{Prefix: "10.200.0.0/24", ExternalPeer: "10.253.2.1", Type: "EBGP", Origin: "IGP", VRF: "default"},
	}
	if _, err := c.Predict.StageBGPAdvertisements(context.Background(), "7", "cs-1", "core", ads); err != nil {
		t.Fatalf("StageBGPAdvertisements: %v", err)
	}
	if calls != 1 || path != "/api/networks/7/change-sets/cs-1/draft/devices/core/bgp-advertisements" || query != "action=bulkAdd" {
		t.Fatalf("%d calls, last %s?%s", calls, path, query)
	}
	if len(body) != 2 || body[0]["externalPeer"] != "10.253.1.1" || body[1]["externalPeer"] != "10.253.2.1" {
		t.Fatalf("body out of order or wrong: %v", body)
	}
	if _, present := body[0]["device"]; present {
		t.Fatalf("the receiving device belongs in the path, not the body: %v", body[0])
	}
	if asPath, ok := body[1]["asPath"].([]any); !ok || len(asPath) != 0 {
		t.Fatalf("nil asPath must be sent as [], got %v", body[1]["asPath"])
	}
	if _, err := c.Predict.StageBGPAdvertisements(context.Background(), "7", "cs-1", "core", nil); err != nil || calls != 1 {
		t.Fatalf("empty slice must send nothing: err=%v calls=%d", err, calls)
	}
	if _, err := c.Predict.StageBGPAdvertisements(context.Background(), "7", "cs-1", "", ads); err == nil || calls != 1 {
		t.Fatal("positive control: a missing device must be refused before any request")
	}
}

// A build without bulkAdd answers Spring's params-condition 400; it must come
// back as a status-typed ErrorResponse so a caller can choose its fallback.
func TestStageBGPAdvertisementsUnsupportedActionIsATyped400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `Parameter conditions "action=add" not met for actual request parameters: action={bulkAdd}`, http.StatusBadRequest)
	}))
	defer server.Close()
	_, err := newTestClient(t, server.URL).Predict.StageBGPAdvertisements(context.Background(), "7", "cs-1", "core",
		[]BGPAdvertisement{{Prefix: "10.0.0.0/8", ExternalPeer: "1.1.1.1"}})
	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response.StatusCode != http.StatusBadRequest || len(apiErr.Body) == 0 {
		t.Fatalf("err = %v, want a 400 ErrorResponse carrying the body", err)
	}
}

func TestListChangeSetChecksReadsOneSnapshot(t *testing.T) {
	var path, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"checks":[{"id":"CSC-7","name":"DC to branch","definition":{"checkType":"REACHABILITY"},"status":"FAIL","numViolations":3,"executionDurationMillis":41},{"id":8,"name":"pending"}]}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	rows, _, err := c.Predict.ListChangeSetChecks(context.Background(), "7", "cs-1", "2695")
	if err != nil {
		t.Fatalf("ListChangeSetChecks: %v", err)
	}
	if path != "/api/networks/7/change-sets/cs-1/checks" || query != "snapshotId=2695" {
		t.Fatalf("request = %s?%s", path, query)
	}
	if len(rows) != 2 || rows[0].ID != "CSC-7" || rows[0].Status != "FAIL" || rows[0].NumViolations == nil || *rows[0].NumViolations != 3 {
		t.Fatalf("rows decoded wrong: %+v", rows)
	}
	var def map[string]any
	if err := json.Unmarshal(rows[0].Definition, &def); err != nil || def["checkType"] != "REACHABILITY" {
		t.Fatalf("definition not kept raw: %s", rows[0].Definition)
	}
	if rows[1].ID != "8" || rows[1].Status != "" || rows[1].NumViolations != nil {
		t.Fatalf("an unexecuted check must carry no status and no violation count: %+v", rows[1])
	}

	if _, _, err := c.Predict.ListChangeSetChecks(context.Background(), "7", "cs-1", ""); err != nil || query != "" {
		t.Fatalf("empty snapshot must omit the parameter (Forward defaults to the base): err=%v query=%q", err, query)
	}
}

func TestListChangeSetChecksRefusalSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Predict verifications can only be run against base or any of its predicted snapshots"}`))
	}))
	defer server.Close()
	_, _, err := newTestClient(t, server.URL).Predict.ListChangeSetChecks(context.Background(), "7", "cs-1", "1")
	if !IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("err = %v, want the 400 refusal", err)
	}
}

func TestValidateCommandsSendsTheValidationRequest(t *testing.T) {
	var path, query string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commandErrors":[{"lineNumber":2,"startColumnNumber":1,"endColumnNumber":9,"errorType":"INVALID","errorMsg":"Invalid input","expectedArgs":[]}],"suggestions":[],"textMarks":[]}`))
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)

	got, _, err := c.Predict.ValidateCommands(context.Background(), "7", "cs-1", "br1", "interface Ethernet1\n shutdwn")
	if err != nil {
		t.Fatalf("ValidateCommands: %v", err)
	}
	if path != "/api/networks/7/change-sets/cs-1/devices/br1/commands" || query != "action=validate" {
		t.Fatalf("request = %s?%s", path, query)
	}
	if body["commands"] != "interface Ethernet1\n shutdwn" || body["cursorLineNum"] != float64(1) || body["cursorColumnNum"] != float64(1) {
		t.Fatalf("body = %v", body)
	}
	if len(got.CommandErrors) != 1 || got.CommandErrors[0].LineNumber != 2 || got.CommandErrors[0].ErrorType != "INVALID" || got.CommandErrors[0].ErrorMsg != "Invalid input" {
		t.Fatalf("errors decoded wrong: %+v", got.CommandErrors)
	}

	if _, _, err := c.Predict.ValidateCommands(context.Background(), "7", "cs-1", "br1", "hostname x", CommandValidationOptions{CursorLine: 1, CursorColumn: 4}); err != nil {
		t.Fatal(err)
	}
	if body["cursorColumnNum"] != float64(4) {
		t.Fatalf("cursor option not sent: %v", body)
	}
	for name, call := range map[string]func() error{
		"empty commands": func() error {
			_, _, err := c.Predict.ValidateCommands(context.Background(), "7", "cs-1", "br1", "  \n")
			return err
		},
		"empty device": func() error {
			_, _, err := c.Predict.ValidateCommands(context.Background(), "7", "cs-1", "", "hostname x")
			return err
		},
	} {
		if call() == nil {
			t.Errorf("positive control (%s): must be refused before any request", name)
		}
	}
}
