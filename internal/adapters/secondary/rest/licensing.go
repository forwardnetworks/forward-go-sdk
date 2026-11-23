package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// LicensingService is the on-prem licensing surface.
//
// SCOPE, AND WHY IT IS SPLIT. Forward serves licensing from TWO controllers
// with different authority, and the split is the whole reason a caller has to
// think about credentials:
//
//   - OnPremLicenseController -- @Requires(org = MANAGE_LICENSING). An ORG
//     admin acting on THEIR OWN org: read the instance fingerprint, decode a
//     key to preview it, apply a key. The org id is taken from the session, NOT
//     from the path, so these can only ever touch the caller's own org.
//   - ForwardSupportLicenseController -- @RequiresForwardAdmin /
//     @RequiresForwardSupport. A PLATFORM operator acting on a NAMED org, id in
//     the path: list, remove, invalidate.
//
// So "apply" and "remove" are not symmetric. Applying is something an org can
// do to itself; removing is something only the platform can do to an org it
// names. A caller that has one credential does not automatically have the
// other, and the remove methods take an orgID precisely because the server
// does.
//
// The `/api` prefix is the servlet prefix every other service in this client
// uses; the controllers themselves map bare paths (verified against
// OnPremLicenseController.getFingerprint -> "/vm/instanceId" and
// CustomBannerController -> "/custom-banners", which this client reaches as
// "/api/custom-banners").
type Licensing service

// Fingerprint returns the running instance's fingerprint. Any authenticated
// user may read it (@RequiresUser); it is the value a license must be bound to.
func (s *Licensing) Fingerprint(ctx context.Context) (*ports.InstanceFingerprint, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/vm/instanceId", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Fingerprint")
	out := new(ports.InstanceFingerprint)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	if strings.TrimSpace(out.InstanceID) == "" {
		// The controller throws when it has no fingerprint rather than
		// returning an empty one, so an empty instanceId here means we parsed
		// something we did not understand. Saying so beats handing the caller a
		// blank string it will bind a license to.
		return nil, response, errors.New("forward: instance fingerprint response carried no instanceId")
	}
	return out, response, nil
}

// Decode previews a key WITHOUT applying it, against the caller's own org.
// This is the honest dry-run: it returns the same status the apply path would
// judge the key by, so a caller can show why a key would be refused without
// changing any state.
func (s *Licensing) Decode(ctx context.Context, key string) (*ports.DecodedLicenseKey, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil, ports.ErrLicenseKeyRequired
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses?action=decode", ports.AsciiCodedSignedLicenseKey{SignedLicenseKey: key})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Decode")
	out := new(ports.DecodedLicenseKey)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Apply activates a key on the CALLER'S OWN org. There is no org parameter and
// that is deliberate: the server reads the org from the session
// (@Requires(org = MANAGE_LICENSING)), so this method is structurally incapable
// of licensing somebody else's org.
func (s *Licensing) Apply(ctx context.Context, key string) (*ports.LicenseDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil, ports.ErrLicenseKeyRequired
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses", ports.AsciiCodedSignedLicenseKey{SignedLicenseKey: key})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Apply")
	out := new(ports.LicenseDetails)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// ListForOrg reads an org's licenses. PLATFORM-scoped (@RequiresForwardSupport).
//
// SaaS deployments only. ForwardSupportLicenseController, which serves this and the
// other /api/orgs/{id}/licenses methods below, is @ProfileCriteria(not = ON_PREM): on an
// on-prem appserver the route is not mapped and Forward answers 404 "No endpoint", as
// it does for any unmapped path; errors.Is(err, ErrEndpointNotServed) tells that apart
// from a missing org. The path is right and is served on both pinned builds; use List
// for your own org, which works everywhere.
func (s *Licensing) ListForOrg(ctx context.Context, orgID string) ([]ports.LicenseDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, ports.ErrLicenseOrgIDRequired
	}
	result := listResponse[ports.LicenseDetails]{Keys: []string{"licenses"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/orgs/%s/licenses", url.PathEscape(orgID)), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.ListForOrg")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// RemoveAllForOrg deletes every license from ONE NAMED org. PLATFORM-scoped
// (@RequiresForwardAdmin). SaaS deployments only: an on-prem appserver does not map the
// route (ErrEndpointNotServed), see ListForOrg.
//
// READ THE PATH. This is DELETE /api/orgs/{id}/licenses -- the org's LICENSE
// COLLECTION. It is not, and cannot become, a call that deletes the org: there
// is no code path from here to org deletion, the trailing segment is a literal,
// and orgID is escaped into one path element so a crafted id cannot climb out
// of it. That is the property to preserve if this method is ever edited.
func (s *Licensing) RemoveAllForOrg(ctx context.Context, orgID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, ports.ErrLicenseOrgIDRequired
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/orgs/%s/licenses", url.PathEscape(orgID)), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Licensing.RemoveAllForOrg")
	return s.client.Do(req, nil)
}

// InvalidateForOrg invalidates ONE license on a named org, leaving the rest.
// PLATFORM-scoped (@RequiresForwardSupport), and SaaS deployments only (on-prem answers
// ErrEndpointNotServed, see ListForOrg). Prefer this over RemoveAllForOrg
// whenever the intent is to undo a single license: "remove all" is a blunt
// instrument and its blast radius is the whole org's entitlement.
func (s *Licensing) InvalidateForOrg(ctx context.Context, orgID, licenseID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	licenseID = strings.TrimSpace(licenseID)
	if orgID == "" {
		return nil, ports.ErrLicenseOrgIDRequired
	}
	if licenseID == "" {
		return nil, errors.New("forward: license id is required")
	}
	path := fmt.Sprintf("/api/orgs/%s/licenses/%s?action=invalidate", url.PathEscape(orgID), url.PathEscape(licenseID))
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Licensing.InvalidateForOrg")
	return s.client.Do(req, nil)
}

// ActivateForOrg activates a stored license on ONE named org (POST /api/orgs/{orgId}/licenses/{licenseId}?action=activate).
// PLATFORM-scoped (@RequiresForwardSupport), and SaaS deployments only (on-prem answers ErrEndpointNotServed, see ListForOrg).
// It changes what the org is entitled to and CANNOT BE UNDONE one license at a time: Forward refuses (400) to invalidate an
// activated license, and a second activation is also a 400; only RemoveAllForOrg clears it, for the whole org. Name the org and
// the license deliberately.
//
// Forward has two forms. Spring picks the handler from the Content-Type the caller sends, and the handler then refuses a form
// that does not match the org's SOFTWARE_CENTRAL property (400). For an org with it off (a SaaS license) pass an empty
// fingerprint: no body and no Content-Type are sent. For an org with it on (an on-prem license) pass the fingerprint of the
// CUSTOMER'S on-prem deployment (Licensing.Fingerprint on a client pointed at that deployment, not this one); it goes as
// {"fingerprint": ...}. A fingerprint from the wrong deployment is accepted and the license is bound to it for good.
// Preview: the route is in the appserver controller, not the published spec. The answer is the stored license: Status is empty
// (no status is computed here) and Signed is true.
func (s *Licensing) ActivateForOrg(ctx context.Context, orgID, licenseID, fingerprint string) (*ports.LicenseDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	orgID = strings.TrimSpace(orgID)
	licenseID = strings.TrimSpace(licenseID)
	if orgID == "" {
		return nil, nil, ports.ErrLicenseOrgIDRequired
	}
	if licenseID == "" {
		return nil, nil, errors.New("forward: license id is required")
	}
	path := fmt.Sprintf("/api/orgs/%s/licenses/%s?action=activate", url.PathEscape(orgID), url.PathEscape(licenseID))
	var body any
	if fingerprint = strings.TrimSpace(fingerprint); fingerprint != "" {
		body = struct {
			Fingerprint string `json:"fingerprint"`
		}{fingerprint}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.ActivateForOrg")
	out := new(ports.LicenseDetails)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// List returns the licenses of the signed-in user's own organization, each
// with its status now (LicenseController.getLicenses; any signed-in user).
// Unlike ListForOrg it is served on every deployment, on-prem included. GET
// /api/licenses. With the SOFTWARE_CENTRAL org property on, invalidated
// licenses are left out.
func (s *Licensing) List(ctx context.Context) ([]ports.LicenseDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/licenses", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.List")
	result := listResponse[ports.LicenseDetails]{Keys: []string{"licenses"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// TierAndStatus returns the signed-in user's organization's license tier and
// status. GET /api/licenses?view=status (LicenseController; any signed-in
// user; served on every deployment).
func (s *Licensing) TierAndStatus(ctx context.Context) (*ports.LicenseTierAndStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/licenses?view=status", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.TierAndStatus")
	out := new(ports.LicenseTierAndStatus)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
