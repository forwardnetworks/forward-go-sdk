package forward

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSnapshotListUploadAndDownload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/network-1/snapshots":
			if got := r.URL.Query().Get("skipUnprocessed"); got != "true" {
				t.Errorf("skipUnprocessed = %q", got)
			}
			if got := r.URL.Query().Get("futureFlag"); got != "enabled" {
				t.Errorf("futureFlag = %q", got)
			}
			_, _ = io.WriteString(w, `{"snapshots":[{"id":101,"state":"PROCESSED"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/snapshots":
			query := r.URL.Query()
			for key, want := range map[string]string{
				"async": "true", "excludeFailedDevices": "true", "skipSnapshotProcessing": "true", "note": "import",
			} {
				if got := query.Get(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatalf("MultipartReader(): %v", err)
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Fatalf("NextPart(): %v", err)
			}
			if strings.ContainsAny(part.FileName(), "\r\n") {
				t.Errorf("unsafe filename = %q", part.FileName())
			}
			body, _ := io.ReadAll(part)
			if string(body) != "zip bytes" {
				t.Errorf("upload body = %q", body)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"snapshot-2","state":"PROCESSING"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/snapshots/snapshot-2":
			if got := r.Header.Get("Accept"); got != "application/zip" {
				t.Errorf("Accept = %q", got)
			}
			w.Header().Set("Content-Type", "application/zip")
			_, _ = io.WriteString(w, "export bytes")
		case r.Method == http.MethodPost && r.URL.Path == "/api/snapshots/snapshot-2":
			if r.URL.Query().Get("action") != "invalidate" || r.URL.Query().Get("reprocess") != "true" {
				t.Errorf("reprocess query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"previousState":"FAILED","state":"PROCESSING"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	yes := true
	snapshots, _, err := client.Snapshots.List(ctx, "network-1", SnapshotListOptions{
		SkipUnprocessed: &yes,
		ExtraQuery:      url.Values{"futureFlag": []string{"enabled"}},
	})
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != "101" {
		t.Fatalf("Snapshots.List() = %#v, %v", snapshots, err)
	}
	uploaded, _, err := client.Snapshots.Upload(ctx, "network-1", []SnapshotUploadFile{{
		Name: "unsafe\r\nname.zip", Reader: strings.NewReader("zip bytes"),
	}}, SnapshotUploadOptions{Note: "import", Async: true, ExcludeFailedDevices: true, SkipSnapshotProcessing: true})
	if err != nil || uploaded.ID != "snapshot-2" {
		t.Fatalf("Snapshots.Upload() = %#v, %v", uploaded, err)
	}
	var exported bytes.Buffer
	if _, err := client.Snapshots.Download(ctx, "snapshot-2", &exported); err != nil || exported.String() != "export bytes" {
		t.Fatalf("Snapshots.Download() = %q, %v", exported.String(), err)
	}
	transition, _, err := client.Snapshots.Reprocess(ctx, "snapshot-2")
	if err != nil || transition.PreviousState != "FAILED" || transition.State != "PROCESSING" {
		t.Fatalf("Snapshots.Reprocess() = %#v, %v", transition, err)
	}
}

func TestPropertiesRoutes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/orgs/22/config":
			if r.URL.Query().Get("filter") != "CONFIGURED" {
				t.Errorf("filter = %q", r.URL.Query().Get("filter"))
			}
			_, _ = io.WriteString(w, `{"ai_allowed":true,"maximum_devices":1000}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/orgs/22/config/ai_allowed":
			if r.URL.Query().Get("value") != "true" {
				t.Errorf("value = %q", r.URL.Query().Get("value"))
			}
			_, _ = io.WriteString(w, `{"ai_allowed":true}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/orgs/22/config/ai_allowed":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	aiAllowed := OrgProperty("ai_allowed")
	values, _, err := client.Properties.Organization(ctx, "22", PropertyFilterConfigured)
	if err != nil || string(values[aiAllowed]) != "true" || string(values[OrgProperty("maximum_devices")]) != "1000" {
		t.Fatalf("Properties.Organization() = %#v, %v", values, err)
	}
	values, _, err = client.Properties.SetOrganization(ctx, "22", aiAllowed, "true")
	if err != nil || string(values[aiAllowed]) != "true" {
		t.Fatalf("Properties.SetOrganization() = %#v, %v", values, err)
	}
	if _, err := client.Properties.ClearOrganization(ctx, "22", aiAllowed); err != nil {
		t.Fatalf("Properties.ClearOrganization() error = %v", err)
	}
}

// Keep imports that document the formats used by Snapshot Upload/Download
// compile-checked as the implementation evolves.
var (
	_ = zip.Store
	_ = json.Valid
	_ = multipart.ErrMessageTooLarge
)
