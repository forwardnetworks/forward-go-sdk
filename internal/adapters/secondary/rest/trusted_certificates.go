package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
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
type TrustedCertificates service

// List returns every trusted certificate in the caller's org.
func (s *TrustedCertificates) List(ctx context.Context) ([]ports.TrustedCertificate, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.TrustedCertificate]{Keys: []string{"certificates", "items"}, AllowSingle: false}
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
func (s *TrustedCertificates) Add(ctx context.Context, request ports.NewTrustedCertificateRequest) (*ports.TrustedCertificate, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/trusted-certificates", request)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "TrustedCertificates.Add")
	out := new(ports.TrustedCertificate)
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
func (s *TrustedCertificates) Apply(ctx context.Context) ([]ports.Identifier, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/trusted-certificates?action=apply", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "TrustedCertificates.Apply")
	var taskIDs []ports.Identifier
	response, err := s.client.doRequired(req, &taskIDs)
	return taskIDs, response, err
}

// Delete removes a trusted certificate by name. It is idempotent: a 404 is
// success, matching AccessControlService's device-access-label and group
// deletes, because the desired state (the certificate is absent) already
// holds.
func (s *TrustedCertificates) Delete(ctx context.Context, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
