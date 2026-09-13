package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// List must hit the plain org-scoped route and decode the bare array Forward
// actually returns (TrustedCertificateController#getCertificates returns
// List<StoredTrustedCertificate>, not an envelope object).
func TestTrustedCertificatesListDecodesBareArray(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/trusted-certificates" {
			t.Fatalf("got %s %s, want GET /api/trusted-certificates", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"skyforge-cloudproxy-ca","certificate":"-----BEGIN CERTIFICATE-----\nMII...\n-----END CERTIFICATE-----","expiresAt":"2030-01-01T00:00:00Z","createdBy":"svc-skyforge"}]`))
	})
	certs, _, err := c.TrustedCertificates.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("got %d certificates, want 1", len(certs))
	}
	if certs[0].Name != "skyforge-cloudproxy-ca" {
		t.Fatalf("got name %q", certs[0].Name)
	}
	if certs[0].CreatedBy != "svc-skyforge" {
		t.Fatalf("got createdBy %q", certs[0].CreatedBy)
	}
}

// Add must POST exactly {name, certificate} -- the shape of NewTrustedCertificate
// (app/src/main/java/com/forwardnetworks/cv/client/cert/NewTrustedCertificate.java)
// -- and refuse locally before sending when either field is blank, since the
// appserver's own BadRequestException for that case is indistinguishable from
// a dozen other 400s.
func TestTrustedCertificatesAddSendsExactShapeAndValidatesLocally(t *testing.T) {
	var gotBody map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/trusted-certificates" {
			t.Fatalf("got %s %s, want POST /api/trusted-certificates", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	})
	stored, resp, err := c.TrustedCertificates.Add(context.Background(), NewTrustedCertificateRequest{
		Name:        "skyforge-cloudproxy-ca",
		Certificate: "-----BEGIN CERTIFICATE-----\nMII...\n-----END CERTIFICATE-----",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if len(gotBody) != 2 {
		t.Fatalf("request body had %d fields, want exactly {name, certificate}: %v", len(gotBody), gotBody)
	}
	if gotBody["name"] != "skyforge-cloudproxy-ca" {
		t.Fatalf("got name %v", gotBody["name"])
	}
	if stored.Name != "skyforge-cloudproxy-ca" {
		t.Fatalf("decoded stored cert name = %q", stored.Name)
	}

	unreached := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be sent for an invalid request, got %s %s", r.Method, r.URL.Path)
	})
	if _, _, err := unreached.TrustedCertificates.Add(context.Background(), NewTrustedCertificateRequest{Certificate: "x"}); err == nil {
		t.Fatal("a blank name must be refused locally")
	}
	if _, _, err := unreached.TrustedCertificates.Add(context.Background(), NewTrustedCertificateRequest{Name: "x"}); err == nil {
		t.Fatal("blank certificate content must be refused locally")
	}
}

// Apply must hit action=apply (not a bare POST, which would try to create a
// certificate named "") and decode the bare array of CollectorTaskIds
// TrustedCertificateTaskService#applyCertificates returns.
func TestTrustedCertificatesApplyHitsActionApplyAndDecodesTaskIDs(t *testing.T) {
	var gotQuery string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/trusted-certificates" {
			t.Fatalf("got %s %s, want POST /api/trusted-certificates", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["P123","P456"]`))
	})
	taskIDs, _, err := c.TrustedCertificates.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotQuery != "action=apply" {
		t.Fatalf("query = %q, want action=apply", gotQuery)
	}
	if len(taskIDs) != 2 || taskIDs[0] != "P123" || taskIDs[1] != "P456" {
		t.Fatalf("got task IDs %v", taskIDs)
	}
}

// A 409 on Apply means "an apply task is already queued or running for every
// supported collector" (TrustedCertificateTaskService throws ConflictException
// only from this handler): the classifier must find it without any string
// matching, so a caller can treat a repeated Apply during reconcile as
// already-in-progress rather than a hard failure.
func TestTrustedCertificatesApplyConflictClassifiesAsInProgress(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"Apply certificates task is in progress for all the collectors"}`))
	})
	_, _, err := c.TrustedCertificates.Apply(context.Background())
	if err == nil {
		t.Fatal("want an error from a 409")
	}
	if !IsTrustedCertificateApplyInProgress(err) {
		t.Fatalf("IsTrustedCertificateApplyInProgress(%v) = false, want true", err)
	}
	if !IsErrorKind(err, ErrorKindTrustedCertificateApplyInProgress) {
		t.Fatalf("IsErrorKind = false, want ErrorKindTrustedCertificateApplyInProgress")
	}
}

// A 409 that is NOT the apply-in-progress route must not be misclassified --
// pinning both directions the way the org-admin nil/empty test does for ACGs.
func TestTrustedCertificatesConflictOnAddIsNotMisclassified(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"unrelated conflict"}`))
	})
	_, _, err := c.TrustedCertificates.Add(context.Background(), NewTrustedCertificateRequest{Name: "n", Certificate: "c"})
	if err == nil {
		t.Fatal("want an error from a 409")
	}
	if IsTrustedCertificateApplyInProgress(err) {
		t.Fatal("a 409 on plain Add must not classify as apply-in-progress")
	}
}

// ApplyOperations must reuse CollectorTasksService.Get for each task ID
// rather than inventing a second poll path -- proven by pointing both routes
// at the same fake server and checking the poller resolves to SUCCEEDED via
// the standard /api/collector-tasks/{id} lookup.
func TestTrustedCertificatesApplyOperationsReusesCollectorTaskLookup(t *testing.T) {
	var gotTaskPaths []string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/trusted-certificates":
			_, _ = w.Write([]byte(`["P123"]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/collector-tasks/P123":
			gotTaskPaths = append(gotTaskPaths, r.URL.Path)
			_, _ = w.Write([]byte(`{"id":"P123","type":"APPLY_CERTIFICATES","status":"SUCCEEDED"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	pollers, _, err := c.TrustedCertificates.ApplyOperations(context.Background())
	if err != nil {
		t.Fatalf("ApplyOperations: %v", err)
	}
	if len(pollers) != 1 {
		t.Fatalf("got %d pollers, want 1", len(pollers))
	}
	task, _, err := pollers[0].Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if task.Status != CollectorTaskSucceeded {
		t.Fatalf("status = %s, want SUCCEEDED", task.Status)
	}
	if len(gotTaskPaths) != 1 || gotTaskPaths[0] != "/api/collector-tasks/P123" {
		t.Fatalf("collector task lookup did not go through the shared route: %v", gotTaskPaths)
	}
}

// Delete is idempotent: a 404 means the certificate is already absent, which
// is the desired end state, not a failure -- same contract as
// AccessControlService's device-access-label and group deletes.
func TestTrustedCertificatesDeleteIsIdempotent(t *testing.T) {
	var gotMethod, gotPath string
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if _, err := c.TrustedCertificates.Delete(context.Background(), "skyforge-cloudproxy-ca"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/trusted-certificates/skyforge-cloudproxy-ca" {
		t.Fatalf("got %s %s", gotMethod, gotPath)
	}

	missing := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Certificate 'x' is not found"}`))
	})
	if _, err := missing.TrustedCertificates.Delete(context.Background(), "x"); err != nil {
		t.Fatalf("404 must be success, got %v", err)
	}

	broken := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := broken.TrustedCertificates.Delete(context.Background(), "x"); err == nil {
		t.Fatal("a real failure must still surface as an error")
	}

	if _, err := c.TrustedCertificates.Delete(context.Background(), "   "); err == nil {
		t.Fatal("a blank name must be refused locally")
	}
}
