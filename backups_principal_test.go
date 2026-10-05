package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Forward requires the ADMINISTER_SYSTEM system permission for the backup routes, not a service principal, so a
// per-user client's request must be sent and a refusal must come back as the typed 403 rather than a client-side
// "service principal" error that hid the real reason.
func TestBackupsDoNotRequireAServicePrincipal(t *testing.T) {
	t.Parallel()

	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		if user, _, _ := r.BasicAuth(); user == "admin" {
			_, _ = io.WriteString(w, `{"enabled":true,"backupTime":"01:00"}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Missing permission: SystemOperation.ADMINISTER_SYSTEM"}`)
	}))
	defer server.Close()
	ctx := context.Background()

	person, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _, err := person.Backups.GetSettings(ctx, StorageTypeS3)
	if err != nil || !settings.Enabled || sent.Load() != 1 {
		t.Fatalf("a person's login with the permission must work: %+v, %v (requests sent: %d)", settings, err, sent.Load())
	}

	denied, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "viewer", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = denied.Backups.GetSettings(ctx, StorageTypeS3)
	if !errors.Is(err, ErrPermissionDenied) || sent.Load() != 2 {
		t.Fatalf("a refusal must be Forward's typed 403 after a real request: %v (requests sent: %d)", err, sent.Load())
	}
	if operation, ok := MissingPermission(err); !ok || operation != "SystemOperation.ADMINISTER_SYSTEM" {
		t.Fatalf("MissingPermission = %q, %v", operation, ok)
	}
	var nilService *BackupsService
	if _, _, err := nilService.GetSettings(ctx, StorageTypeS3); err == nil {
		t.Fatal("a nil service must still be refused")
	}
}
