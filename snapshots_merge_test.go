package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Merging must hit action=merge with the chosen mergeType, stream the ZIP as multipart "file", and hand back the id from Location.
func TestMergeIntoLatestWire(t *testing.T) {
	var query, filename, content string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Path + "?" + r.URL.RawQuery
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err == nil {
			filename, _ = part.FileName(), 0
			b, _ := io.ReadAll(part)
			content = string(b)
		}
		w.Header().Set("Location", "/api/snapshots/777")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"776","state":"UNPACKING"}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	out, _, err := c.Snapshots.MergeIntoLatest(context.Background(), "N1", domain.SnapshotUploadFile{Name: "extra.zip", Reader: strings.NewReader("PKzip")},
		domain.SnapshotMergeOptions{Note: "n", MergeType: domain.SnapshotMergeReplaceExisting})
	if err != nil {
		t.Fatal(err)
	}
	if query != "/api/networks/N1/snapshots?action=merge&mergeType=REPLACE_EXISTING&note=n" || filename != "extra.zip" || content != "PKzip" {
		t.Errorf("query=%q file=%q content=%q", query, filename, content)
	}
	if out.ID != "777" || out.Snapshot.State != "UNPACKING" {
		t.Errorf("result = %+v (the id must come from Location)", out)
	}
	// The zero merge type is NONE, Forward's own default, sent explicitly.
	_, _, _ = c.Snapshots.MergeIntoLatest(context.Background(), "N1", domain.SnapshotUploadFile{Reader: strings.NewReader("PK")}, domain.SnapshotMergeOptions{})
	if !strings.Contains(query, "mergeType=NONE") {
		t.Errorf("default merge type: %q", query)
	}
}

// The 409 for devices in both must be typed with the sorted names, so a caller can pick REPLACE_EXISTING or PRESERVE_EXISTING.
func TestMergeIntoLatestConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"Conflicting devices","conflictDevices":["r1","r2"]}`)
	}))
	defer server.Close()
	c := newTestClient(t, server.URL)
	_, _, err := c.Snapshots.MergeIntoLatest(context.Background(), "N1", domain.SnapshotUploadFile{Reader: strings.NewReader("PK")}, domain.SnapshotMergeOptions{})
	var apiErr *ports.ErrorResponse
	if !errors.Is(err, domain.ErrSnapshotMergeConflict) || !errors.As(err, &apiErr) || strings.Join(apiErr.ConflictDevices, ",") != "r1,r2" {
		t.Fatalf("err = %v (%+v)", err, apiErr)
	}
}

// A bad merge type, a missing reader and an empty file on disk must be refused without sending a request.
func TestMergeIntoLatestRefusesBeforeSending(t *testing.T) {
	sent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sent++ }))
	defer server.Close()
	c := newTestClient(t, server.URL)
	ctx := context.Background()
	file := domain.SnapshotUploadFile{Reader: strings.NewReader("PK")}
	if _, _, err := c.Snapshots.MergeIntoLatest(ctx, "N1", file, domain.SnapshotMergeOptions{MergeType: "replace_existing"}); err == nil {
		t.Error("an unknown merge type must be refused")
	}
	if _, _, err := c.Snapshots.MergeIntoLatest(ctx, "N1", domain.SnapshotUploadFile{}, domain.SnapshotMergeOptions{}); err == nil {
		t.Error("a missing reader must be refused")
	}
	empty := filepath.Join(t.TempDir(), "empty.zip")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Snapshots.MergeFileIntoLatest(ctx, "N1", empty, domain.SnapshotMergeOptions{}); err == nil {
		t.Error("an empty ZIP must be refused")
	}
	if sent != 0 {
		t.Errorf("%d requests were sent", sent)
	}
}
