package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCollectorRegistrationIdentityIsUsedForUpload(t *testing.T) {
	t.Parallel()
	var eventsMu sync.Mutex
	var events []Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/collectors":
			username, password, ok := r.BasicAuth()
			if !ok || username != "support" || password != "api-secret" {
				t.Errorf("registration auth = %q/%q/%v", username, password, ok)
			}
			if r.Method != http.MethodPost {
				t.Errorf("registration method = %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"c-1","name":"edge","username":"collector.edge","authorizationKey":"collector-secret"}`)
		case "/api/networks/n-1/performance":
			username, password, ok := r.BasicAuth()
			if !ok || username != "collector.edge" || password != "collector-secret" {
				t.Errorf("performance auth = %q/%q/%v; API credential leaked into collector lane", username, password, ok)
			}
			if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Errorf("performance content type = %q", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL: server.URL, Username: "support", Password: "api-secret", AuthMode: AuthModeService,
		Hooks: []Hook{func(_ context.Context, event Event) {
			eventsMu.Lock()
			events = append(events, event)
			eventsMu.Unlock()
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	registration, _, err := client.Collectors.Register(context.Background(), CollectorRegistrationRequest{CollectorName: "edge"})
	if err != nil {
		t.Fatal(err)
	}
	if registration.Identity() != (CollectorIdentity{Username: "collector.edge", AuthorizationKey: "collector-secret"}) {
		t.Fatalf("collector identity = %#v", registration.Identity())
	}
	if _, err := client.Performance.UploadWithIdentity(context.Background(), "n-1", registration.Identity(), []byte("payload")); err != nil {
		t.Fatal(err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()
	foundService, foundCollector := false, false
	for _, event := range events {
		if event.Type != EventRequest {
			continue
		}
		if event.Operation == "Collectors.Register" && event.AuthMode == AuthModeService {
			foundService = true
		}
		if event.Operation == "Performance.UploadWithIdentity" && event.AuthMode == AuthModeCollector {
			foundCollector = true
		}
	}
	if !foundService || !foundCollector {
		t.Fatalf("operation/auth hook events = %#v", events)
	}
}

func TestBrowserSessionAndUnauthenticatedReachability(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/public/csrf":
			if r.Header.Get("Authorization") != "" {
				t.Error("CSRF request carried authorization")
			}
			_, _ = io.WriteString(w, `{"headerName":"X-CSRF-TOKEN","parameterName":"_csrf","token":"csrf-value"}`)
		case "/api/auth/login":
			if got := r.Header.Get("X-CSRF-TOKEN"); got != "csrf-value" {
				t.Errorf("CSRF header = %q", got)
			}
			var input BrowserLoginRequest
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input.Username != "alice" || input.Password != "password" {
				t.Errorf("login = %#v", input)
			}
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "session-value", Path: "/"})
			_, _ = io.WriteString(w, `{"location":"/"}`)
		case "/api/users/current":
			cookie, err := r.Cookie("SESSION")
			if err != nil || cookie.Value != "session-value" {
				t.Errorf("session cookie = %#v, %v", cookie, err)
			}
			_, _ = io.WriteString(w, `{"user":{"id":"u-1","orgId":"o-1","username":"alice"}}`)
		case "/api/version":
			if r.Header.Get("Authorization") != "" {
				t.Error("reachability request carried authorization")
			}
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	browser, err := NewClient(Config{BaseURL: server.URL, Username: "alice", Password: "password", AuthMode: AuthModeBrowser})
	if err != nil {
		t.Fatal(err)
	}
	csrf, _, err := browser.Browser.PublicCSRFAPI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	login, _, err := browser.Browser.LoginAPI(context.Background(), BrowserLoginRequest{}, csrf)
	if err != nil {
		t.Fatal(err)
	}
	if len(login.Cookies) != 1 || login.Cookies[0].Name != "SESSION" {
		t.Fatalf("login cookies = %#v", login.Cookies)
	}
	user, _, err := browser.Browser.CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "u-1" || user.Username != "alice" {
		t.Fatalf("browser user = %#v", user)
	}

	probe, err := NewClient(Config{BaseURL: server.URL, AuthMode: AuthModeNone})
	if err != nil {
		t.Fatal(err)
	}
	reachable, response, err := probe.Version.Reachable(context.Background())
	if err != nil || !reachable || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reachable=%v response=%v err=%v", reachable, response, err)
	}
}

func TestBackupRootRoutesAndServicePrincipal(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "support" || password != "secret" {
			t.Errorf("backup auth = %q/%q/%v", username, password, ok)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /backup-settings":
			if r.URL.Query().Get("storageType") != "S3" {
				t.Errorf("settings query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"enabled":true,"backupTime":"01:00"}`)
		case "POST /backup-settings":
			if r.URL.Query().Get("storageType") != "S3" || r.URL.Query().Get("action") != "chown" {
				t.Errorf("ownership query = %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusNoContent)
		case "GET /backup-settings/storage":
			w.WriteHeader(http.StatusNoContent)
		case "GET /backups":
			if r.URL.Query().Get("view") != "lastBackupResult" {
				t.Errorf("last query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, "null")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Username: "support", Password: "secret", AuthMode: AuthModeService})
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := client.Backups.GetSettings(context.Background(), StorageTypeS3)
	if err != nil || !settings.Enabled {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
	if _, err := client.Backups.SetS3BucketOwnership(context.Background(), S3StorageSettings{BucketName: "bucket"}); err != nil {
		t.Fatal(err)
	}
	storage, _, err := client.Backups.GetS3Storage(context.Background())
	if err != nil || storage != nil {
		t.Fatalf("empty storage=%#v err=%v", storage, err)
	}
	last, _, err := client.Backups.Last(context.Background(), StorageTypeS3, BackupTriggerManual)
	if err != nil || last != nil {
		t.Fatalf("empty last=%#v err=%v", last, err)
	}

	userClient, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := userClient.Backups.GetSettings(context.Background(), StorageTypeS3); err == nil || !strings.Contains(err.Error(), "service principal") {
		t.Fatalf("user backup error = %v", err)
	}
}

func TestTransparentFacadeAndMutationNoRetry(t *testing.T) {
	t.Parallel()
	var attachCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/collector-tasks":
			w.Header().Set("X-Forward-Test", "preserved")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"collection already in progress"}`)
		case "PUT /api/networks/n-1/collector":
			attachCalls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"failed"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Username: "user", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := client.Compatibility.StartCollectorTask(context.Background(), "n-1")
	if err != nil {
		t.Fatal(err)
	}
	if document.StatusCode != http.StatusConflict || document.Header.Get("X-Forward-Test") != "preserved" || !strings.Contains(string(document.Body), "already in progress") {
		t.Fatalf("document = %#v body=%s", document, document.Body)
	}
	if _, err := client.Collectors.Attach(context.Background(), "n-1", CollectorAttachmentRequest{Username: "collector"}); err == nil {
		t.Fatal("attach unexpectedly succeeded")
	}
	if got := attachCalls.Load(); got != 1 {
		t.Fatalf("attach attempts = %d, want 1", got)
	}
}

func TestAdminSupportedOrganizationCorrectionRoute(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/users/u-1/supported-orgs" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("orgId") != "o-1" || r.URL.Query().Get("expiresIn") != "365" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, Username: "support", Password: "secret", AuthMode: AuthModeService})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Admin.AddSupportedOrganization(context.Background(), "u-1", "o-1", 365); err != nil {
		t.Fatal(err)
	}
}

func TestConnectivityStatusToleratesMeasuredNestedShapes(t *testing.T) {
	t.Parallel()
	var status SourceTestStatus
	if err := json.Unmarshal([]byte(`{"deviceName":"r1","testResult":{"error":{"code":"auth failed","message":"bad secret"},"phase":"LOGIN"},"snmpCollectionStatus":{"state":"FAILED"}}`), &status); err != nil {
		t.Fatal(err)
	}
	if status.Name != "r1" || !status.TestResultPresent || status.ConnectivityError != "AUTH_FAILED" || status.ConnectivityErrorRaw != "bad secret" || status.ErrorPhase != "LOGIN" || status.SNMPCollectionStatus != "FAILED" {
		t.Fatalf("status = %#v", status)
	}
}
