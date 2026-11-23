package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The export body carries only what was asked for; the obfuscation key goes in
// the body (never the URL), and ?only= appears only when set.
func TestSnapshotsExportRequest(t *testing.T) {
	t.Parallel()

	var uri, accept, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uri, accept, body = r.URL.RequestURI(), r.Header.Get("Accept"), string(b)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04 some zip bytes"))
	}))
	defer server.Close()
	snapshots := newTestClient(t, server.URL).Snapshots
	ctx := context.Background()

	var out bytes.Buffer
	n, _, err := snapshots.Export(ctx, "S1", domain.SnapshotExportOptions{}, &out)
	if err != nil || n != int64(out.Len()) || n == 0 || uri != "/api/snapshots/S1" || accept != "application/zip" || body != `{}` {
		t.Fatalf("whole export: n=%d err=%v uri=%s accept=%s body=%s", n, err, uri, accept, body)
	}
	out.Reset()
	if _, _, err := snapshots.Export(ctx, "S1", domain.SnapshotExportOptions{IncludeDevices: []string{"leaf?", " "}, ObfuscationKey: "k3y", ObfuscateNames: true, Only: "config"}, &out); err != nil ||
		uri != "/api/snapshots/S1?only=CONFIG" || body != `{"includeDevices":["leaf?"],"obfuscateNames":true,"obfuscationKey":"k3y"}` {
		t.Fatalf("obfuscated subset: %v %s %s", err, uri, body)
	}
	if _, _, err := snapshots.Export(ctx, "S1", domain.SnapshotExportOptions{ExcludeDevices: []string{"spine1"}}, io.Discard); err != nil || body != `{"excludeDevices":["spine1"]}` {
		t.Fatalf("exclude: %v %s", err, body)
	}
}

func TestSnapshotsExportRefusals(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	snapshots := newTestClient(t, server.URL).Snapshots
	ctx := context.Background()

	for name, o := range map[string]domain.SnapshotExportOptions{
		"include and exclude": {IncludeDevices: []string{"a"}, ExcludeDevices: []string{"b"}},
		"blank include":       {IncludeDevices: []string{" "}},
		"names without key":   {ObfuscateNames: true},
		"unknown filter":      {Only: "LOGS"},
	} {
		if _, _, err := snapshots.Export(ctx, "S1", o, io.Discard); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, _, err := snapshots.Export(ctx, " ", domain.SnapshotExportOptions{}, io.Discard); err == nil {
		t.Error("a blank snapshot ID must be refused")
	}
	if _, _, err := snapshots.Export(ctx, "S1", domain.SnapshotExportOptions{}, nil); err == nil {
		t.Error("a nil writer must be refused")
	}
	if calls != 0 {
		t.Fatalf("refused exports reached the wire %d times", calls)
	}
}

// Forward answers a snapshot that is not ready with a body that is not a ZIP;
// that must not read as a successful export.
func TestSnapshotsExportRejectsNonZip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"status\":\"PROCESSING\"}\n")
	}))
	defer server.Close()

	n, _, err := newTestClient(t, server.URL).Snapshots.Export(context.Background(), "S1", domain.SnapshotExportOptions{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "did not return a ZIP") || !strings.Contains(err.Error(), "PROCESSING") || n == 0 {
		t.Fatalf("non-ZIP body: n=%d err=%v", n, err)
	}
}

// The key is a secret. It must not appear in printed options or in an error.
func TestSnapshotExportOptionsRedactTheKey(t *testing.T) {
	t.Parallel()

	options := domain.SnapshotExportOptions{ObfuscationKey: "s3cr3t-key", IncludeDevices: []string{"a"}}
	for _, text := range []string{options.String(), fmt.Sprintf("%v", options), fmt.Sprintf("%+v", options), fmt.Sprintf("%#v", options)} {
		if strings.Contains(text, "s3cr3t-key") {
			t.Fatalf("the obfuscation key leaked: %s", text)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad request"}`)
	}))
	defer server.Close()
	_, _, err := newTestClient(t, server.URL).Snapshots.Export(context.Background(), "S1", options, io.Discard)
	if err == nil || strings.Contains(err.Error(), "s3cr3t-key") {
		t.Fatalf("error must exist and not carry the key: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(`{}`), &sent)
}
