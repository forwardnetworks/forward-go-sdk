package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// forward-skills' requirement: tags written from a snapshot (snapshotId on the query, devices validated against that snapshot) show on that
// snapshot at once, and the read at a snapshot sends snapshotId together with with=devices (the only form that takes it).
func TestDeviceTagsSnapshotScoped(t *testing.T) {
	var method, uri string
	var body map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), nil
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"tags":[{"name":"b-tag","devices":["r2"]},{"name":"a-tag","devices":["r1","r3"]}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, err := client.DeviceTags.AddBatchToAt(ctx, "n1", " 7 ", []string{"r1", "vsys1"}, []string{"edge"}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || uri != "/api/networks/n1/device-tags?action=addBatchTo&snapshotId=7" || len(body["devices"]) != 2 || body["tags"][0] != "edge" {
		t.Fatalf("add: %s %s %v", method, uri, body)
	}
	if _, err := client.DeviceTags.RemoveBatchFromAt(ctx, "n1", "7", []string{"r1"}, []string{"edge"}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || uri != "/api/networks/n1/device-tags?action=removeBatchFrom&snapshotId=7" || body["devices"][0] != "r1" {
		t.Fatalf("remove: %s %s %v", method, uri, body)
	}
	tags, _, err := client.DeviceTags.ListAt(ctx, "n1", "7")
	if err != nil || len(tags) != 2 || tags[0].Name != "a-tag" || len(tags[0].Devices) != 2 {
		t.Fatalf("list: %+v %v", tags, err)
	}
	if method != http.MethodGet || uri != "/api/networks/n1/device-tags?snapshotId=7&with=devices" {
		t.Fatalf("list sent %s %s", method, uri)
	}
	// The SDK never sends validateDevices, and the staging-instant forms keep sending no snapshotId.
	if _, err := client.DeviceTags.AddBatchTo(ctx, "n1", []string{"r1"}, []string{"edge"}); err != nil || uri != "/api/networks/n1/device-tags?action=addBatchTo" {
		t.Fatalf("AddBatchTo must stay snapshot-free: %s %v", uri, err)
	}
	for name, call := range map[string]func() error{
		"add without snapshot": func() error {
			_, err := client.DeviceTags.AddBatchToAt(ctx, "n1", "", []string{"r1"}, []string{"t"})
			return err
		},
		"remove without snapshot": func() error {
			_, err := client.DeviceTags.RemoveBatchFromAt(ctx, "n1", " ", []string{"r1"}, []string{"t"})
			return err
		},
		"list without snapshot": func() error { _, _, err := client.DeviceTags.ListAt(ctx, "n1", ""); return err },
		"add without devices":   func() error { _, err := client.DeviceTags.AddBatchToAt(ctx, "n1", "7", nil, []string{"t"}); return err },
	} {
		if call() == nil {
			t.Errorf("%s must be refused before a request", name)
		}
	}
}
