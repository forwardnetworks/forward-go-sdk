package forward

import (
	"context"
	"errors"
	"fmt"
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
type LicensingService service

// LicenseKeyStatus is the server's verdict on a key. VALID is the ONLY
// affirmative value; every other constant is a distinct reason it was refused,
// and they are worth surfacing individually because they send an operator to
// completely different places -- a signature problem is a trust-anchor
// question, a fingerprint problem is a wrong-instance question, and the tier
// values are entitlement arithmetic that has nothing to do with cryptography.
type LicenseKeyStatus string

const (
	LicenseKeyValid              LicenseKeyStatus = "VALID"
	LicenseKeyInvalidLicense     LicenseKeyStatus = "INVALID_LICENSE"
	LicenseKeyInvalidSignature   LicenseKeyStatus = "INVALID_SIGNATURE"
	LicenseKeyInvalidFingerprint LicenseKeyStatus = "INVALID_FINGERPRINT"
	LicenseKeyWrongTier          LicenseKeyStatus = "WRONG_TIER"
	LicenseKeyBadTierTrialTiming LicenseKeyStatus = "BAD_TIER_TRIAL_TIMING"
	LicenseKeyBadTrialTier       LicenseKeyStatus = "BAD_TRIAL_TIER"
)

// OK reports whether the server accepted the key. It exists so callers stop
// writing `status == "VALID"` string comparisons and so that "not VALID" is
// never mistaken for "unknown": an empty status is NOT ok.
func (s LicenseKeyStatus) OK() bool { return s == LicenseKeyValid }

// InstanceFingerprint identifies THIS appserver instance. A license is bound to
// one: LicenseEvaluator refuses a key whose fingerprint is not the running
// instance's, so a key minted for one appserver is inert on another.
type InstanceFingerprint struct {
	InstanceID string `json:"instanceId"`
}

// AsciiCodedSignedLicenseKey is the wire shape both decode and apply take: the
// license in its canonical ascii encoding, signature included.
type AsciiCodedSignedLicenseKey struct {
	Key string `json:"key"`
}

// License is the decoded body of a key.
type License struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Tier      string `json:"tier,omitempty"`
	StartsAt  string `json:"startsAt,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Stackable bool   `json:"stackable,omitempty"`
}

// DecodedLicenseKey is what a preview returns. `Used` reports that this org
// already holds this license, which is how a caller tells "would work" from
// "already applied" -- two states that otherwise both look like success.
type DecodedLicenseKey struct {
	KeyStatus     LicenseKeyStatus `json:"keyStatus"`
	License       *License         `json:"license,omitempty"`
	LicenseStatus string           `json:"licenseStatus,omitempty"`
	Used          bool             `json:"used,omitempty"`
}

// LicenseDetails is an applied license as the org holds it.
type LicenseDetails struct {
	ID      string   `json:"id,omitempty"`
	License *License `json:"license,omitempty"`
	Status  string   `json:"status,omitempty"`
}

// ErrLicenseKeyRequired is returned before any request is built. A blank key
// would otherwise reach the server and come back as a generic decode failure,
// which reads like a bad license rather than an empty form field.
var ErrLicenseKeyRequired = errors.New("forward: license key is required")

// ErrLicenseOrgIDRequired guards the PLATFORM-scoped calls. These take the org
// id in the PATH, so an empty one does not fail closed -- it would build
// "/api/orgs//licenses", and what that resolves to is the server's business,
// not something to find out by sending it. Refuse locally instead.
var ErrLicenseOrgIDRequired = errors.New("forward: org id is required for platform-scoped licensing calls")

// Fingerprint returns the running instance's fingerprint. Any authenticated
// user may read it (@RequiresUser); it is the value a license must be bound to.
func (s *LicensingService) Fingerprint(ctx context.Context) (*InstanceFingerprint, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/vm/instanceId", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Fingerprint")
	out := new(InstanceFingerprint)
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
func (s *LicensingService) Decode(ctx context.Context, key string) (*DecodedLicenseKey, *Response, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil, ErrLicenseKeyRequired
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses?action=decode", AsciiCodedSignedLicenseKey{Key: key})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Decode")
	out := new(DecodedLicenseKey)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Apply activates a key on the CALLER'S OWN org. There is no org parameter and
// that is deliberate: the server reads the org from the session
// (@Requires(org = MANAGE_LICENSING)), so this method is structurally incapable
// of licensing somebody else's org.
func (s *LicensingService) Apply(ctx context.Context, key string) (*LicenseDetails, *Response, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil, ErrLicenseKeyRequired
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses", AsciiCodedSignedLicenseKey{Key: key})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Apply")
	out := new(LicenseDetails)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// ListForOrg reads an org's licenses. PLATFORM-scoped (@RequiresForwardSupport).
func (s *LicensingService) ListForOrg(ctx context.Context, orgID string) ([]LicenseDetails, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, ErrLicenseOrgIDRequired
	}
	result := listResponse[LicenseDetails]{Keys: []string{"licenses"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("/api/orgs/%s/licenses", url.PathEscape(orgID)), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.ListForOrg")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// RemoveAllForOrg deletes every license from ONE NAMED org. PLATFORM-scoped
// (@RequiresForwardAdmin).
//
// READ THE PATH. This is DELETE /api/orgs/{id}/licenses -- the org's LICENSE
// COLLECTION. It is not, and cannot become, a call that deletes the org: there
// is no code path from here to org deletion, the trailing segment is a literal,
// and orgID is escaped into one path element so a crafted id cannot climb out
// of it. That is the property to preserve if this method is ever edited.
func (s *LicensingService) RemoveAllForOrg(ctx context.Context, orgID string) (*Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, ErrLicenseOrgIDRequired
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/orgs/%s/licenses", url.PathEscape(orgID)), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Licensing.RemoveAllForOrg")
	return s.client.Do(req, nil)
}

// InvalidateForOrg invalidates ONE license on a named org, leaving the rest.
// PLATFORM-scoped (@RequiresForwardSupport). Prefer this over RemoveAllForOrg
// whenever the intent is to undo a single license: "remove all" is a blunt
// instrument and its blast radius is the whole org's entitlement.
func (s *LicensingService) InvalidateForOrg(ctx context.Context, orgID, licenseID string) (*Response, error) {
	orgID = strings.TrimSpace(orgID)
	licenseID = strings.TrimSpace(licenseID)
	if orgID == "" {
		return nil, ErrLicenseOrgIDRequired
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
