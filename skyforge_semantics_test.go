package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestForNetworkSharesTransportAndScopesServices(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/networks/tenant-2/snapshots" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `[{"id":12,"state":"PROCESSED"}]`)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "tenant-1"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := client.ForNetwork("tenant-2")
	if scoped == client || scoped.httpClient != client.httpClient || scoped.capabilities != client.capabilities {
		t.Fatal("ForNetwork must clone scope while sharing transport and capability state")
	}
	if client.Network() != "tenant-1" || scoped.Network() != "tenant-2" {
		t.Fatalf("network scopes = %q, %q", client.Network(), scoped.Network())
	}
	snapshots, _, err := scoped.Snapshots.List(context.Background(), "", SnapshotListOptions{})
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != "12" {
		t.Fatalf("scoped Snapshots.List() = %#v, %v", snapshots, err)
	}
}

func TestUnavailableClientFailsClosed(t *testing.T) {
	t.Parallel()

	cause := errors.New("credential lookup failed")
	client := NewUnavailable("tenant-9", cause)
	_, _, err := client.Snapshots.List(context.Background(), "", SnapshotListOptions{})
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || !errors.Is(err, cause) || unavailable.NetworkID != "tenant-9" {
		t.Fatalf("unavailable error = %#v, %v", unavailable, err)
	}
}

func TestTypedAPIErrorClassifications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		status int
		body   string
		want   error
	}{
		{"collection code", http.MethodPost, "/api/collector-tasks", http.StatusConflict, `{"errorCode":"COLLECTION_ALREADY_IN_PROGRESS"}`, ErrCollectionAlreadyInProgress},
		{"collection wording", http.MethodPost, "/api/collector-tasks", http.StatusBadRequest, `{"message":"A collection for network 77 is already in progress"}`, ErrCollectionAlreadyInProgress},
		{"snapshot", http.MethodPost, "/api/snapshots/55", http.StatusConflict, `{"reason":"SNAPSHOT_NOT_PROCESSED"}`, ErrSnapshotNotProcessed},
		{"network", http.MethodDelete, "/api/networks/missing", http.StatusNotFound, `{}`, ErrNetworkNotFound},
		{"network collection route", http.MethodGet, "/api/networks/missing/snapshots", http.StatusNotFound, `{}`, ErrNetworkNotFound},
		{"auth", http.MethodGet, "/api/networks", http.StatusUnauthorized, `{"message":"bad token"}`, ErrAuthentication},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			req, err := client.NewRequest(context.Background(), test.method, test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Do(req, nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, test.want)
			}
		})
	}
}

func TestChecksRequireNonVacuousScoringAndCreatePersistent(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/snapshots/100/checks":
			if listCalls.Add(1) == 1 {
				_, _ = io.WriteString(w, `{"checks":[]}`)
			} else {
				_, _ = io.WriteString(w, `[{"id":1,"name":"reach-app","status":"PASS","numViolations":0}]`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/snapshots/100/checks":
			if r.URL.Query().Get("persistent") != "true" {
				t.Errorf("persistent = %q", r.URL.Query().Get("persistent"))
			}
			_, _ = io.WriteString(w, `{"id":902}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	_, _, err := client.Checks.ForScoring(context.Background(), "100")
	if !errors.Is(err, ErrNoChecks) {
		t.Fatalf("empty ForScoring() error = %v", err)
	}
	set, _, err := client.Checks.ForScoring(context.Background(), "100")
	if err != nil || set.Len() != 1 || set.SnapshotID() != "100" || set.Checks()[0].Name != "reach-app" {
		t.Fatalf("ForScoring() = %#v, %v", set, err)
	}
	_, _, err = client.Checks.ForScoring(context.Background(), "100", CheckRequirements{
		RequiredPatterns: []string{`^chg_.+_reach_`},
	})
	if !errors.Is(err, ErrCheckCorpusIncomplete) {
		t.Fatalf("missing corpus error = %v", err)
	}
	id, _, err := client.Checks.CreatePersistent(context.Background(), "100", NewCheck{
		Name: "reach-app", Definition: map[string]any{"checkType": "Existential"}, Enabled: true,
	})
	if err != nil || id != "902" {
		t.Fatalf("CreatePersistent() = %q, %v", id, err)
	}
}

func TestChecksRetryOnlyClientPrimeTimeout(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(40 * time.Millisecond)
			return
		}
		_, _ = io.WriteString(w, `[{"name":"cached","status":"PASS"}]`)
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL: server.URL, Username: "user", Password: "pass",
		HTTPClient: &http.Client{Timeout: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	checks, _, err := client.Checks.List(context.Background(), "1")
	if err != nil || len(checks) != 1 || calls.Load() < 2 {
		t.Fatalf("Checks.List() = %#v, calls %d, %v", checks, calls.Load(), err)
	}
}

func TestSnapshotSubsetExportRetriesAndResolveSkipsPredicted(t *testing.T) {
	t.Parallel()

	var exports atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/snapshots/20":
			if exports.Add(1) == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"reason":"SNAPSHOT_NOT_PROCESSED"}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"includeDevices":["r1"]`) {
				t.Errorf("subset body = %s", body)
			}
			_, _ = io.WriteString(w, "PKzip")
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/n1/snapshots":
			_, _ = io.WriteString(w, `[{"id":30,"state":"PROCESSED","changeSetId":4},{"id":"20","state":"PROCESSED"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	archive, _, err := client.Snapshots.ExportSubset(context.Background(), "20", SnapshotSubsetRequest{
		IncludeDevices: []string{"r1"}, PollInterval: time.Millisecond, Attempts: 2,
	})
	if err != nil || string(archive) != "PKzip" || exports.Load() != 2 {
		t.Fatalf("ExportSubset() = %q, calls %d, %v", archive, exports.Load(), err)
	}
	id, _, err := client.Snapshots.ResolveID(context.Background(), "n1", "latest")
	if err != nil || id != "20" {
		t.Fatalf("ResolveID() = %q, %v", id, err)
	}
}

func TestSnapshotMultipartMergePreservesResultMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/networks/n1/snapshots" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("async") != "true" || r.URL.Query().Get("skipSnapshotProcessing") != "true" {
			t.Errorf("merge query = %s", r.URL.RawQuery)
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(part)
			parts = append(parts, string(body))
		}
		if len(parts) != 2 || parts[0] != "carried" || parts[1] != "fresh" {
			t.Errorf("multipart files = %#v", parts)
		}
		_, _ = io.WriteString(w, `{"id":88,"state":"PROCESSING","totalDevices":40,"message":"accepted"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	snapshot, _, err := client.Snapshots.Upload(context.Background(), "n1", []SnapshotUploadFile{
		{Name: "carried.zip", Reader: strings.NewReader("carried")},
		{Name: "fresh.zip", Reader: strings.NewReader("fresh")},
	}, SnapshotUploadOptions{Async: true, SkipSnapshotProcessing: true})
	if err != nil || snapshot.ID != "88" || snapshot.TotalDevices != 40 || snapshot.Message != "accepted" {
		t.Fatalf("merge Upload() = %#v, %v", snapshot, err)
	}
}

func TestCollectionProgressAndNumericTaskID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"taskId":44}`)
		case http.MethodGet:
			if r.URL.Query().Get("networkId") != "n1" {
				t.Errorf("networkId = %q", r.URL.Query().Get("networkId"))
			}
			_, _ = fmt.Fprint(w, `[{"id":44,"networkId":"n1","status":"RUNNING","progress":{"total":10,"queued":2,"running":1,"succeeded":6,"failed":1}}]`)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	taskID, _, err := client.CollectorTasks.Start(context.Background(), "")
	if err != nil || taskID != "44" {
		t.Fatalf("Start() = %q, %v", taskID, err)
	}
	progress, _, err := client.CollectorTasks.Progress(context.Background(), "")
	if err != nil || !progress.InProgress || progress.Total != 10 || progress.Active != 3 || progress.Finished != 7 {
		t.Fatalf("Progress() = %#v, %v", progress, err)
	}
}

func TestCapabilityProfileBlocksUnsupportedEndpoint(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL: server.URL, Username: "user", Password: "pass", NetworkID: "n1",
		Capabilities: CapabilityProfile{Track: "stable", Build: "stable-build", Features: map[Capability]CapabilitySupport{
			CapabilityStructuredBGPAdvertisements: CapabilityUnsupported,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Predict.StageBGPAdvertisement(context.Background(), "", "cs1", BGPAdvertisement{Device: "r1"})
	var unsupported *UnsupportedCapabilityError
	if !errors.As(err, &unsupported) || calls.Load() != 0 {
		t.Fatalf("StageBGPAdvertisement() error = %v, calls = %d", err, calls.Load())
	}
}

func TestDeviceTagsAndCollectorPerformance(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/n1/device-tags" && r.URL.Query().Get("action") == "addBatch":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"name":"branch"`) {
				t.Errorf("tag body = %s", body)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/n1/device-tags" && r.URL.Query().Get("action") == "addBatchTo":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"devices":["r1"]`) {
				t.Errorf("assignment body = %s", body)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/networks/n1/device-tags":
			_, _ = io.WriteString(w, `{"tags":[{"name":"z"},{"name":"branch","color":"#fff"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/n1/performance":
			user, password, ok := r.BasicAuth()
			if !ok || user != "collector" || password != "collector-secret" {
				t.Errorf("collector auth = %q, %q, %v", user, password, ok)
			}
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("content type = %q", r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != "performance" {
				t.Errorf("performance body = %q", body)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	ctx := context.Background()
	if _, err := client.DeviceTags.AddBatch(ctx, "", []DeviceTag{{Name: "branch", Color: "#fff"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeviceTags.AddBatchTo(ctx, "", []string{"r1"}, []string{"branch"}); err != nil {
		t.Fatal(err)
	}
	tags, _, err := client.DeviceTags.List(ctx, "", "devices")
	if err != nil || len(tags) != 2 || tags[0].Name != "branch" {
		t.Fatalf("DeviceTags.List() = %#v, %v", tags, err)
	}
	if _, err := client.Performance.Upload(ctx, "", "collector", "collector-secret", []byte("performance")); err != nil {
		t.Fatal(err)
	}
}

func TestNQEResultBuildShapesConvertInsideSDK(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/nqe" || r.URL.Query().Get("networkId") != "n1" || r.URL.Query().Get("snapshotId") != "20" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"rows":[{"device":"r1","count":2}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")
	result, _, err := client.NQE.Run(context.Background(), "", "20", NQEQueryRequest{Query: "foreach d in network.devices select d.name"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := result.RowsAny()
	if err != nil || len(rows) != 1 || rows[0]["device"] != "r1" || rows[0]["count"] != float64(2) {
		t.Fatalf("RowsAny() = %#v, %v", rows, err)
	}
}
