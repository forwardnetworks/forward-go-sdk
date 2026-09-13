package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TrustedCertificatesService manages org-wide trusted certificates and
// dispatches the per-collector task that imports them into each collector's
// Java truststore.
//
// Real appserver routes (verified against forward/fwd @
// web/src/main/java/com/forwardnetworks/cv/web/controller/TrustedCertificateController.java):
//
//	GET    /api/trusted-certificates               list (getCertificates)
//	POST   /api/trusted-certificates               add one (addCertificate)
//	POST   /api/trusted-certificates?action=apply  dispatch ApplyCertificatesTask
//	                                                to every collector that
//	                                                supports it (applyCertificates)
//	DELETE /api/trusted-certificates/{name}        remove one (deleteCertificate)
//
// Apply is NOT a single org-wide operation: TrustedCertificateTaskService.
// applyCertificates (web/src/main/java/.../collector/ts/handler/cert/
// TrustedCertificateTaskService.java) enqueues one ApplyCertificatesTask per
// collector in the org that supports custom certificates and returns the list
// of enqueued CollectorTaskIds. It already fans out correctly; there is no
// separate per-collector endpoint to find. Skyforge previously believed this
// route could not be called safely -- the real defect was in the collector's
// keytool import (missing -storepass on the freshly copied cacerts
// truststore), which is fixed separately and out of scope here.
//
// Applying certificates makes every affected collector restart to pick up the
// new truststore. Do not call Apply on every reconcile pass: check List first
// and only Add+Apply when the named certificate is missing or its content
// changed.
type TrustedCertificatesService service

// TrustedCertificate is a stored trusted certificate (StoredTrustedCertificate
// on the wire, with TrustedCertificate and Attribution JSON-unwrapped into one
// object per @JsonUnwrapped on both fields). CreatedBy/UpdatedBy are usernames
// (augmented server-side); they are empty when the acting user was deleted or
// no update has happened yet, matching AttributionWithNames' NON_NULL fields.
type TrustedCertificate struct {
	Name        string    `json:"name"`
	Certificate string    `json:"certificate"`
	ExpiresAt   time.Time `json:"expiresAt,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	UpdatedBy   string    `json:"updatedBy,omitempty"`
}

// NewTrustedCertificateRequest is the POST body (NewTrustedCertificate.java):
// exactly {name, certificate}, both required.
type NewTrustedCertificateRequest struct {
	Name        string `json:"name"`
	Certificate string `json:"certificate"`
}

func (r NewTrustedCertificateRequest) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("forward: trusted certificate name is required")
	}
	if strings.TrimSpace(r.Certificate) == "" {
		return errors.New("forward: trusted certificate PEM content is required")
	}
	return nil
}

// List returns every trusted certificate in the caller's org.
func (s *TrustedCertificatesService) List(ctx context.Context) ([]TrustedCertificate, *Response, error) {
	result := listResponse[TrustedCertificate]{Keys: []string{"certificates", "items"}, AllowSingle: false}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/trusted-certificates", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "TrustedCertificates.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// Add stores a new trusted certificate. Forward rejects a duplicate name AND
// a certificate whose content already exists under another name (both are
// validated server-side, TrustedCertificateService#validateUniqueness) with a
// 400. Callers that reconcile a well-known certificate name (such as a
// MITM/cloud-proxy CA) should List first and skip Add when an entry with the
// same name and content is already present, so a repeat reconcile pass never
// needlessly errors or forces a redundant Apply.
func (s *TrustedCertificatesService) Add(ctx context.Context, request NewTrustedCertificateRequest) (*TrustedCertificate, *Response, error) {
	if err := request.validate(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/trusted-certificates", request)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "TrustedCertificates.Add")
	out := new(TrustedCertificate)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Apply dispatches an ApplyCertificatesTask to every collector in the org
// that supports trusted certificates and does not already have one queued or
// running, and returns the resulting collector task IDs. It fans out
// per-collector on the appserver; there is nothing further to scope here.
//
// A 409 means an apply task is already in progress for every supported
// collector -- IsTrustedCertificateApplyInProgress classifies it so callers
// can treat a repeat Apply as "already underway" rather than a failure.
// Applying restarts every collector it reaches, so callers must not call this
// on every reconcile tick; call it only after Add confirms new or changed
// content (see Add's doc).
func (s *TrustedCertificatesService) Apply(ctx context.Context) ([]Identifier, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/trusted-certificates?action=apply", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "TrustedCertificates.Apply")
	var taskIDs []Identifier
	response, err := s.client.doRequired(req, &taskIDs)
	return taskIDs, response, err
}

// ApplyOperations calls Apply and wraps each returned collector task ID in a
// Poller, reusing CollectorTasksService.Get -- the same generic collector-task
// lookup ApplyCertificatesTask is enqueued through on the appserver
// (TrustedCertificateTaskService delegates to the same CollectorTaskService
// that backs network collection) -- instead of a second, invented poll loop.
// Callers that only need to fire-and-forget can call Apply directly and
// observe completion later (for example from a periodic reconcile pass)
// without ever constructing a Poller.
func (s *TrustedCertificatesService) ApplyOperations(ctx context.Context) ([]*Poller[CollectorTask], *Response, error) {
	taskIDs, response, err := s.Apply(ctx)
	if err != nil {
		return nil, response, err
	}
	pollers := make([]*Poller[CollectorTask], 0, len(taskIDs))
	for _, taskID := range taskIDs {
		id := taskID.String()
		initial := &CollectorTask{ID: taskID, Type: "APPLY_CERTIFICATES", Status: CollectorTaskQueued}
		poller, pollerErr := NewPoller(initial, func(ctx context.Context) (*CollectorTask, *Response, error) {
			return s.client.CollectorTasks.Get(ctx, id)
		}, collectorTaskDone)
		if pollerErr != nil {
			return nil, response, pollerErr
		}
		pollers = append(pollers, poller)
	}
	return pollers, response, nil
}

// Delete removes a trusted certificate by name. It is idempotent: a 404 is
// success, matching AccessControlService's device-access-label and group
// deletes, because the desired state (the certificate is absent) already
// holds.
func (s *TrustedCertificatesService) Delete(ctx context.Context, name string) (*Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: trusted certificate name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/trusted-certificates/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "TrustedCertificates.Delete")
	return s.client.doAccepted(req, nil, true, func(code int) bool { return code == http.StatusNotFound || (code >= 200 && code < 300) })
}
