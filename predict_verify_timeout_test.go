package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Forward lets the base-model comparison run for up to 90 minutes, far past a
// client's usual HTTP timeout, so VerifyPredictModeling waits longer than the
// client would for one call. A request that is slower than the client's own
// 50ms limit succeeds here, while the same slow answer to an ordinary Predict
// read on that client times out, which is what shows the extension is what
// let it through.
func TestVerifyPredictModelingOutwaitsAShortClientTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, `{"diffs":{},"score":{"precision":1,"recall":2,"f1":3},"scoresByProtocol":{}}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL:    server.URL,
		Username:   "user",
		Password:   "pass",
		HTTPClient: &http.Client{Transport: privateHTTPClient().Transport, Timeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, _, err := client.Predict.ListPredictedSnapshots(ctx, "n1", "cs1"); err == nil {
		t.Fatal("control: an ordinary read slower than the client timeout succeeded")
	}
	v, _, err := client.Predict.VerifyPredictModeling(ctx, "s1", false)
	if err != nil {
		t.Fatalf("VerifyPredictModeling did not outwait the client timeout: %v", err)
	}
	if score, ok, err := v.OverallScore(); err != nil || !ok || score.F1 != 3 {
		t.Fatalf("score = %+v, %v, %v", score, ok, err)
	}

	// A caller's own context deadline still wins.
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, _, err := client.Predict.VerifyPredictModeling(short, "s1", false); err == nil {
		t.Fatal("a context deadline shorter than the response did not stop the call")
	}
}
