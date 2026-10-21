package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The failure summary is the one assist read that is a GET (the config and impact summaries are POSTs with no body), it takes the
// two snapshot ids in the path, and the answer is {"summary": text}.
func TestSummarizeFailuresIsAGetOnTheDiffPath(t *testing.T) {
	t.Parallel()

	var method, uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, uri = r.Method, r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"summary":"Two reachability checks regressed."}`))
	}))
	defer server.Close()
	text, _, err := newTestClient(t, server.URL).AIAssist.SummarizeFailures(context.Background(), "100", "101")
	if err != nil || text != "Two reachability checks regressed." {
		t.Fatalf("%q %v", text, err)
	}
	if method != http.MethodGet || uri != "/api/diffs/100/101/failure-summary-assist" {
		t.Errorf("wire: %s %s", method, uri)
	}
	if _, _, err := newTestClient(t, server.URL).AIAssist.SummarizeFailures(context.Background(), "100", " "); err == nil {
		t.Error("both snapshot ids are required")
	}
}
