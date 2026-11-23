package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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

	if _, _, err := short.Snapshots.Export(ctx, "S1", ports.SnapshotExportOptions{}, io.Discard); err == nil {
		t.Fatal("the client's own 50ms timeout must still apply when Timeout is zero")
	}
	if _, _, err := short.Snapshots.Export(ctx, "S1", ports.SnapshotExportOptions{Timeout: 5 * time.Second}, io.Discard); err != nil {
		t.Fatalf("a positive Timeout must replace the client's: %v", err)
	}
	if _, _, err := short.Snapshots.Export(ctx, "S1", ports.SnapshotExportOptions{Timeout: -1}, io.Discard); err != nil {
		t.Fatalf("a negative Timeout must remove it: %v", err)
	}
	upload := func(timeout time.Duration) error {
		_, _, err := short.Snapshots.Upload(ctx, "N1", []ports.SnapshotUploadFile{{Name: "s.zip", Reader: strings.NewReader("zip")}}, ports.SnapshotUploadOptions{Timeout: timeout})
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
