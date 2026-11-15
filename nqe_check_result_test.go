package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Seen live on 26.11.0-03: the answer is the internal run result with typed cells and totalNumValues. The cells must unwrap (scalar,
// list, one-of), the total must drive Truncated, the paging options must be the body itself, and ItemFormat, which Forward refuses on
// this route with a 400, must never be sent.
func TestNQECheckResult(t *testing.T) {
	var gotQuery, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/nqe/queryResults" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		gotQuery, gotBody = r.URL.RawQuery, string(raw)
		_, _ = w.Write([]byte(`{"type":{"Bag":{}},"diagnostics":[],"executionInfo":{"computeDurationMillis":3},"cklbExportable":false,
		 "result":{"values":[
		  {"Device":{"value":"a","metadata":{"type":"DEVICE"}},"OS":{"type":"OS","alternative":{"value":"NXOS"}},"Peers":[{"value":"10.0.2.2"},{"value":"10.0.2.3"}],"Empty":[],"Bytes":{"value":9007199254740993}}],
		  "totalNumValues":30,"totalNumValuesIsExact":true,"columnSummaries":{}}}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	result, _, err := client.NQE.CheckResult(context.Background(), "9", "abc123", nil)
	if err != nil || !result.Truncated() || len(result.Items) != 1 || result.TotalNumItems != 30 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if gotQuery != "resultKey=abc123&snapshotId=9" || gotBody != "" {
		t.Fatalf("query=%q body=%q", gotQuery, gotBody)
	}
	type row struct {
		Device string   `json:"Device"`
		OS     string   `json:"OS"`
		Peers  []string `json:"Peers"`
		Empty  []string `json:"Empty"`
		Bytes  int64    `json:"Bytes"`
	}
	rows, err := DecodeNQE[row](result.Items)
	if err != nil || rows[0].Device != "a" || rows[0].OS != "NXOS" || len(rows[0].Peers) != 2 || rows[0].Peers[1] != "10.0.2.3" || len(rows[0].Empty) != 0 || rows[0].Bytes != 9007199254740993 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}

	limit := int32(2)
	if _, _, err = client.NQE.CheckResult(context.Background(), "9", "abc123", &NQEOptions{Limit: &limit, ItemFormat: "JSON"}); err != nil || gotBody != `{"limit":2}` {
		t.Fatalf("ItemFormat must not be sent: body=%q err=%v", gotBody, err)
	}
	if _, _, err = client.NQE.CheckResult(context.Background(), "9", " ", nil); err == nil {
		t.Fatal("a missing result key must be refused")
	}
}

// A build that answers the published shape still decodes, and a record with a column named "value" is not mistaken for a cell.
func TestNQECheckResultOtherShapes(t *testing.T) {
	body := `{"snapshotId":"9","items":[{"Device":"a"}],"totalNumItems":5}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, _, err := client.NQE.CheckResult(context.Background(), "9", "k", nil)
	if err != nil || !result.Truncated() || len(result.Items) != 1 {
		t.Fatalf("published shape: %+v %v", result, err)
	}
	body = `{"result":{"values":[{"value":{"value":"x"},"Name":{"value":"n"}}],"totalNumValues":1}}`
	result, _, err = client.NQE.CheckResult(context.Background(), "9", "k", nil)
	if err != nil || result.Truncated() || string(result.Items[0]["Name"]) != `"n"` || string(result.Items[0]["value"]) != `"x"` {
		t.Fatalf("a column named value: %+v %v", result, err)
	}
}
