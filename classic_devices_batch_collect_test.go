package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The batch PUT is the only path Skyforge writes devices through, and it had no
// collect field at all -- so there was no way to onboard a device with
// collection off. These pin the three states, because a two-state boolean here
// would silently turn collection ON for every device on every sync.
func TestClassicDevicePutBatchCollectHasThreeStates(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewClient(Config{BaseURL: srv.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	no := false
	yes := true
	if _, err := c.ClassicDevices.PutBatch(context.Background(), "1", []ClassicDeviceBatchItem{
		{Name: "absent", Host: "h1"},
		{Name: "off", Host: "h2", Collect: &no},
		{Name: "on", Host: "h3", Collect: &yes},
	}); err != nil {
		t.Fatalf("putBatch: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("sent %d items, want 3", len(items))
	}
	// ABSENT must not appear at all. A device whose collect state we are not
	// changing must not have one asserted on its behalf -- that is the whole
	// reason this is a pointer.
	if _, present := items[0]["collect"]; present {
		t.Fatalf("an unset Collect was still sent as %v; absent must stay absent", items[0]["collect"])
	}
	if v, ok := items[1]["collect"].(bool); !ok || v {
		t.Fatalf("Collect=false was sent as %#v, want an explicit false", items[1]["collect"])
	}
	if v, ok := items[2]["collect"].(bool); !ok || !v {
		t.Fatalf("Collect=true was sent as %#v, want an explicit true", items[2]["collect"])
	}
}
