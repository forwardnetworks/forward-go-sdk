package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// RefusedOnly is for a caller that wants a bounded read: only the two
// statuses that mean "turned away unprocessed" are ridden out, and anything
// that says the appserver is unhealthy comes straight back, even on a GET.
func TestRetryRefusedOnly(t *testing.T) {
	t.Parallel()

	for status, wantCalls := range map[int]int32{
		http.StatusTooManyRequests:     2,
		http.StatusServiceUnavailable:  2,
		http.StatusInternalServerError: 1,
		http.StatusBadGateway:          1,
		http.StatusGatewayTimeout:      1,
	} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(status)
				return
			}
			_, _ = io.WriteString(w, `{"build":"1"}`)
		}))
		client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p",
			Retry: RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond, RefusedOnly: true}})
		if err != nil {
			t.Fatal(err)
		}
		_, _, _ = client.Version.Get(context.Background())
		server.Close()
		if calls.Load() != wantCalls {
			t.Errorf("status %d: %d calls, want %d", status, calls.Load(), wantCalls)
		}
	}
}

// A dropped connection is an unknown outcome, not a refusal.
func TestRetryRefusedOnlyDoesNotRetryTransportErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p",
		Retry: RetryPolicy{MaxAttempts: 3, Delay: time.Millisecond, RefusedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Version.Get(context.Background()); err == nil || calls.Load() != 1 {
		t.Fatalf("err = %v after %d calls, want a transport error after one", err, calls.Load())
	}
}

// Jitter only shortens a backoff, within its fraction, and never touches a
// server's Retry-After.
func TestRetryJitterBounds(t *testing.T) {
	t.Parallel()

	for i := 0; i < 200; i++ {
		wait, ok := retryWait(nil, time.Second, 3, time.Minute, 0.5)
		if !ok || wait < 2*time.Second || wait > 4*time.Second {
			t.Fatalf("jittered wait = %s, want within [2s, 4s]", wait)
		}
	}
	after := &http.Response{Header: http.Header{"Retry-After": []string{"3"}}}
	if wait, _ := retryWait(after, time.Second, 1, time.Minute, 1); wait != 3*time.Second {
		t.Fatalf("Retry-After with jitter = %s, want exactly 3s", wait)
	}
	if wait, _ := retryWait(nil, time.Second, 1, time.Minute, 7); wait < 0 || wait > time.Second {
		t.Fatalf("jitter above 1 must clamp: wait = %s", wait)
	}
}

// The published Aliases envelope; members vary by type, so the whole object
// is kept alongside the common fields.
func TestAliasesList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/snapshots/1021/aliases" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"aliases":[
		  {"name":"dmz-hosts","type":"HOSTS","createdAt":"2022-04-06T20:34:45.118Z","creatorId":"12","values":["10.1.0.0/16"],"locations":["atl"]},
		  {"name":"web","type":"HEADERS","createdAt":"2022-04-06T20:34:45.118Z","creatorId":12,"values":{"tp_dst":["80","443"]}}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Aliases.List(context.Background(), "1021")
	if err != nil {
		t.Fatalf("Aliases.List() error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "dmz-hosts" || got[0].Type != "HOSTS" || got[1].CreatorID != "12" {
		t.Fatalf("Aliases.List() = %+v", got)
	}
	if !bytes.Contains(got[0].Definition, []byte(`"locations":["atl"]`)) || !bytes.Contains(got[1].Definition, []byte(`"tp_dst"`)) {
		t.Fatalf("definitions lost members: %s / %s", got[0].Definition, got[1].Definition)
	}
}

func TestChecksListPredefinedAndL7Applications(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/predefinedChecks":
			_, _ = io.WriteString(w, `[{"name":"BGP neighbors established","description":"Every configured BGP session is up","predefinedCheckType":"BGP_NEIGHBOR_ADJACENCY"}]`)
		case "/api/l7-applications":
			_, _ = io.WriteString(w, `[{"id":"http"},{"id":"ssh"}]`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	checks, _, err := client.Checks.ListPredefined(context.Background())
	if err != nil || len(checks) != 1 || checks[0].PredefinedCheckType != "BGP_NEIGHBOR_ADJACENCY" {
		t.Fatalf("ListPredefined() = %+v, %v", checks, err)
	}
	apps, _, err := client.Networks.L7Applications(context.Background())
	if err != nil || len(apps) != 2 || apps[1].ID != "ssh" {
		t.Fatalf("L7Applications() = %+v, %v", apps, err)
	}
}

// Forward has no diff-text route: Files says which files changed, and the
// caller downloads both sides. DeviceFileName converts StoredFileName.format's
// stored name into DeviceFile.getName's download name.
func TestDiffsFilesListsChangedFiles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/diffs/100/101/files" || r.URL.Query().Get("type") != "CONFIG" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"deviceInfos":[{"name":"leaf-1","vendor":"ARISTA","files":[
		  {"name":"leaf-1,CONFIGURATION.txt"},
		  {"name":"leaf-1,CUSTOM_CLI,,,2.txt","command":"show clock"}]}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Diffs.Files(context.Background(), "100", "101", "CONFIG")
	if err != nil {
		t.Fatalf("Diffs.Files() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "leaf-1" || len(got[0].Files) != 2 || got[0].Files[1].Command != "show clock" {
		t.Fatalf("Diffs.Files() = %+v", got)
	}
	for stored, want := range map[string]string{
		"leaf-1,CONFIGURATION.txt":  "CONFIGURATION.txt",
		"leaf-1,CUSTOM_CLI,,,2.txt": "CUSTOM_CLI,2.txt",
		"leaf-1,SHOW_VERSION":       "SHOW_VERSION",
		"leaf-1,BGP_TAB,,tag1.txt":  "",
		"global-file":               "",
	} {
		name, ok := DiffFile{Name: stored}.DeviceFileName()
		if name != want || ok != (want != "") {
			t.Errorf("DeviceFileName(%q) = %q, %v; want %q", stored, name, ok, want)
		}
	}
}

// Forward serves the whole file regardless of Range, so the cap is ours: stop
// at maxBytes, say it was truncated, and do not call that an error.
func TestDevicesDownloadFileHead(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("interface Ethernet1\n", 10_000) // 200 KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/networks/net-1/devices/leaf-1/files/CONFIGURATION.txt" || r.URL.Query().Get("snapshotId") != "1021" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	var head bytes.Buffer
	n, truncated, _, err := client.Devices.DownloadFileHead(context.Background(), "net-1", "leaf-1", "CONFIGURATION.txt", "1021", 1000, &head)
	if err != nil || !truncated || n != 1000 || head.String() != body[:1000] {
		t.Fatalf("head = %d bytes, truncated=%v, err=%v", n, truncated, err)
	}
	var whole bytes.Buffer
	n, truncated, _, err = client.Devices.DownloadFileHead(context.Background(), "net-1", "leaf-1", "CONFIGURATION.txt", "1021", int64(len(body)), &whole)
	if err != nil || truncated || n != int64(len(body)) {
		t.Fatalf("exact cap: %d bytes, truncated=%v, err=%v", n, truncated, err)
	}
	if _, _, _, err := client.Devices.DownloadFileHead(context.Background(), "net-1", "leaf-1", "CONFIGURATION.txt", "1021", 0, &whole); err == nil {
		t.Fatal("a zero cap must be refused")
	}
}
