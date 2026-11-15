package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The route takes snapshotId and resultKey as query parameters and the paging options as the body itself (not wrapped); a nil
// options sends no body so Forward applies its own first page.
func TestNQECheckResult(t *testing.T) {
	var gotQuery, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/nqe/queryResults" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		gotQuery, gotBody = r.URL.RawQuery, string(raw)
		_, _ = w.Write([]byte(`{"snapshotId":"9","items":[{"Device":"a"}],"totalNumItems":5}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	result, _, err := client.NQE.CheckResult(context.Background(), "9", "abc123", nil)
	if err != nil || !result.Truncated() || len(result.Items) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if gotQuery != "resultKey=abc123&snapshotId=9" || gotBody != "" {
		t.Fatalf("query=%q body=%q", gotQuery, gotBody)
	}
	limit := int32(2)
	if _, _, err = client.NQE.CheckResult(context.Background(), "9", "abc123", &NQEOptions{Limit: &limit}); err != nil || gotBody != `{"limit":2}` {
		t.Fatalf("body=%q err=%v", gotBody, err)
	}
	if _, _, err = client.NQE.CheckResult(context.Background(), "9", " ", nil); err == nil {
		t.Fatal("a missing result key must be refused")
	}
}
