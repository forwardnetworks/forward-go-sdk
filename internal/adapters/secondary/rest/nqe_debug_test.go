package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A plan larger than the cap must fail rather than fill memory: the cap is
// checked on the path io.Copy really takes, not only on Write.
func TestExplainRefusesAPlanBeyondTheCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < maxExplainBytes>>20+2; i++ {
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer server.Close()
	plan, _, err := newTestClient(t, server.URL).NQE.ExplainQuery(context.Background(), "FQ_big", "", "", nil)
	if err == nil || plan != "" || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("an oversized plan must be refused: len=%d, %v", len(plan), err)
	}
	if _, _, err := newTestClient(t, server.URL).NQE.RunMetrics(context.Background(), ports.NQERunMetricsOptions{Limit: ports.Ptr(-1)}); err == nil {
		t.Fatal("a negative limit must fail before any request")
	}
}
