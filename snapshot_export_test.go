package forward

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
	"time"
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
	n, _, err := snapshots.Export(ctx, "S1", SnapshotExportOptions{}, &out)
	if err != nil || n != int64(out.Len()) || n == 0 || uri != "/api/snapshots/S1" || accept != "application/zip" || body != `{}` {
		t.Fatalf("whole export: n=%d err=%v uri=%s accept=%s body=%s", n, err, uri, accept, body)
	}
	out.Reset()
	if _, _, err := snapshots.Export(ctx, "S1", SnapshotExportOptions{IncludeDevices: []string{"leaf?", " "}, ObfuscationKey: "k3y", ObfuscateNames: true, Only: "config"}, &out); err != nil ||
		uri != "/api/snapshots/S1?only=CONFIG" || body != `{"includeDevices":["leaf?"],"obfuscateNames":true,"obfuscationKey":"k3y"}` {
		t.Fatalf("obfuscated subset: %v %s %s", err, uri, body)
	}
	if _, _, err := snapshots.Export(ctx, "S1", SnapshotExportOptions{ExcludeDevices: []string{"spine1"}}, io.Discard); err != nil || body != `{"excludeDevices":["spine1"]}` {
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

	for name, o := range map[string]SnapshotExportOptions{
		"include and exclude": {IncludeDevices: []string{"a"}, ExcludeDevices: []string{"b"}},
		"blank include":       {IncludeDevices: []string{" "}},
		"names without key":   {ObfuscateNames: true},
		"unknown filter":      {Only: "LOGS"},
	} {
		if _, _, err := snapshots.Export(ctx, "S1", o, io.Discard); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, _, err := snapshots.Export(ctx, " ", SnapshotExportOptions{}, io.Discard); err == nil {
		t.Error("a blank snapshot ID must be refused")
	}
	if _, _, err := snapshots.Export(ctx, "S1", SnapshotExportOptions{}, nil); err == nil {
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

	n, _, err := newTestClient(t, server.URL).Snapshots.Export(context.Background(), "S1", SnapshotExportOptions{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "did not return a ZIP") || !strings.Contains(err.Error(), "PROCESSING") || n == 0 {
		t.Fatalf("non-ZIP body: n=%d err=%v", n, err)
	}
}

// The key is a secret. It must not appear in printed options or in an error.
func TestSnapshotExportOptionsRedactTheKey(t *testing.T) {
	t.Parallel()

	options := SnapshotExportOptions{ObfuscationKey: "s3cr3t-key", IncludeDevices: []string{"a"}}
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

// A large export or upload outlasts the client's default timeout; Timeout
// overrides it per call (negative removes it) without touching the client.
func TestSnapshotTransfersHonourPerCallTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(200 * time.Millisecond)
		if r.URL.Path == "/api/snapshots/S1" {
			_, _ = w.Write([]byte("PK\x03\x04"))
			return
		}
		_, _ = io.WriteString(w, `{"id":"501"}`)
	}))
	defer server.Close()
	short, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, _, err := short.Snapshots.Export(ctx, "S1", SnapshotExportOptions{}, io.Discard); err == nil {
		t.Fatal("the client's own 50ms timeout must still apply when Timeout is zero")
	}
	if _, _, err := short.Snapshots.Export(ctx, "S1", SnapshotExportOptions{Timeout: 5 * time.Second}, io.Discard); err != nil {
		t.Fatalf("a positive Timeout must replace the client's: %v", err)
	}
	if _, _, err := short.Snapshots.Export(ctx, "S1", SnapshotExportOptions{Timeout: -1}, io.Discard); err != nil {
		t.Fatalf("a negative Timeout must remove it: %v", err)
	}
	upload := func(timeout time.Duration) error {
		_, _, err := short.Snapshots.Upload(ctx, "N1", []SnapshotUploadFile{{Name: "s.zip", Reader: strings.NewReader("zip")}}, SnapshotUploadOptions{Timeout: timeout})
		return err
	}
	if err := upload(0); err == nil {
		t.Fatal("Upload with no Timeout must keep the client's 50ms")
	}
	if err := upload(5 * time.Second); err != nil {
		t.Fatalf("Upload Timeout must replace the client's: %v", err)
	}
	if short.httpClient.Timeout != 50*time.Millisecond {
		t.Fatalf("the shared client's timeout was changed: %v", short.httpClient.Timeout)
	}
}
