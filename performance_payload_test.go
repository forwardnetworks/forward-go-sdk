package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/forwardnetworks/forward-go-sdk/perfpayload"
)

// The typed upload sends exactly the bytes perfpayload encodes, as the collector, and a payload Forward would drop part of
// never reaches the wire.
func TestUploadPayloadSendsTheEncodedBytesAsTheCollector(t *testing.T) {
	t.Parallel()
	var body []byte
	var user, contentType string
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		user, _, _ = r.BasicAuth()
		contentType = r.Header.Get("Content-Type")
		body, _ = io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/networks/7/performance" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	perf := newTestClient(t, server.URL).Performance
	ctx := context.Background()
	payload := perfpayload.Payload{Time: time.Unix(1_800_000_000, 0), Devices: []perfpayload.DevicePerf{
		{DeviceName: "sw1", Time: time.Unix(1_799_999_990, 0), CPU: 0.4, Interfaces: map[string]perfpayload.Counters{"e1": {InOctets: 9}}},
	}}
	want, err := payload.Encode()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := perf.UploadPayload(ctx, "7", "collector-1", "key", payload); err != nil {
		t.Fatal(err)
	}
	if string(body) != string(want) || user != "collector-1" || contentType != "application/octet-stream" {
		t.Fatalf("body % X user %q type %q, want % X as collector-1", body, user, contentType, want)
	}

	requests = 0
	payload.Devices[0].CPU = 45 // a percent sent where a fraction belongs
	_, err = perf.UploadPayloadWithIdentity(ctx, "7", CollectorIdentity{Username: "collector-1", AuthorizationKey: "key"}, payload)
	if !errors.Is(err, perfpayload.ErrCPUOrMemoryAbove1) || requests != 0 {
		t.Fatalf("err = %v, requests = %d; want the validation error and no request", err, requests)
	}
}
