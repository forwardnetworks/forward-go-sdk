package forward

import (
	"bytes"
	"context"
	"encoding/json"
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
	// Forward >= 26.9 (NewLicenseKeyStatus) folds what used to arrive as a
	// separate licenseStatus/used pair into the one status:
	LicenseKeyExpired            LicenseKeyStatus = "EXPIRED"
	LicenseKeyUnsupportedType    LicenseKeyStatus = "UNSUPPORTED_TYPE"
	LicenseKeyStartTimeCollision LicenseKeyStatus = "START_TIME_COLLISION"
	LicenseKeyCCHDebtUnpaid      LicenseKeyStatus = "CCH_DEBT_UNPAID"
	LicenseKeyAlreadyUsed        LicenseKeyStatus = "ALREADY_USED"
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
// license in its canonical ascii encoding, signature included. The field is
// `signedLicenseKey` -- Forward's AsciiCodedSignedLicenseKey has ONE
// @JsonCreator parameter of that name and requireNonNull()s it, on 26.8.x and
// 26.9 alike, so any other name is a 400 "'signedLicenseKey' is required".
// This client sent `key` from 2026-09-08 until 2026-09-17, so Decode and Apply
// never reached the verifier; measured live against 26.9.0-09.
type AsciiCodedSignedLicenseKey struct {
	SignedLicenseKey string `json:"signedLicenseKey"`
}

// License is the decoded body of a key.
//
// The capacity fields and Replaces are the tier licence's (TierLicense: tier, replaces,
// standardDeviceCap, liteDeviceCap, cloudCreditHours, edgeAssetCap, with the licence
// metadata flattened beside them). A licence type that does not carry one leaves it zero,
// and Replaces nil when the server did not state it.
type License struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
	// Tier is Forward's LicenseTier name (LicenseTierB ... LicenseTierNSP); Facets() says what it includes. Tier and tier-trial licences carry one.
	Tier      string `json:"tier,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	StartsAt  string `json:"startsAt,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	// LicensedTo, PurchaseOrder and SupportEndsOn (a perpetual licence's end of support, "2027-06-30") are LicenseMeta's, flattened.
	LicensedTo    string `json:"licensedTo,omitempty"`
	PurchaseOrder string `json:"purchaseOrder,omitempty"`
	SupportEndsOn string `json:"supportEndsOn,omitempty"`
	// Replaces is true when using the licence ends every other active licence of the org
	// at once, and false when its capacity is added to them (it "stacks"). A stacking tier
	// licence is only accepted when its tier equals the org's tier (that of its active
	// non-trial licences when it takes effect; none means no check), else "License tier
	// mismatch". A replacing one skips that tier check, not the cloud-credit-debt checks.
	Replaces *bool `json:"replaces,omitempty"`
	// Stackable is !Replaces, filled in when decoding a licence that states replaces. The
	// server has no field of this name; it is kept for callers that already read it. Only
	// tier licences state replaces; for the other types Replaces stays nil and Stackable false (the server treats TIER_TRIAL,
	// DEVICE, CLOUD and ENDPOINT as stacking and LEGACY as replacing).
	Stackable         bool `json:"stackable,omitempty"`
	StandardDeviceCap int  `json:"standardDeviceCap,omitempty"`
	LiteDeviceCap     int  `json:"liteDeviceCap,omitempty"`
	CloudCreditHours  int  `json:"cloudCreditHours,omitempty"`
	EdgeAssetCap      int  `json:"edgeAssetCap,omitempty"`
}

// LicenseTier names (Forward's LicenseTier). Each is a set of facets; see Facets.
const (
	LicenseTierB   = "B"   // base
	LicenseTierN   = "N"   // base + network
	LicenseTierNP  = "NP"  // base + network + predict
	LicenseTierS   = "S"   // base + security
	LicenseTierNS  = "NS"  // base + network + security
	LicenseTierNSP = "NSP" // base + network + security + predict
)

// LicenseFacet is one product area a tier covers (Forward's LicenseFacet). CLOUD is a license TYPE, not a facet: cloud credit
// hours are a capacity that any tier licence can carry.
type LicenseFacet string

const (
	LicenseFacetBase     LicenseFacet = "BASE"
	LicenseFacetNetwork  LicenseFacet = "NETWORK"
	LicenseFacetSecurity LicenseFacet = "SECURITY"
	LicenseFacetPredict  LicenseFacet = "PREDICT"
)

// TierFacets returns the facets a tier name includes, in Forward's order; nil for a name this SDK does not know.
func TierFacets(tier string) []LicenseFacet {
	switch tier {
	case LicenseTierB:
		return []LicenseFacet{LicenseFacetBase}
	case LicenseTierN:
		return []LicenseFacet{LicenseFacetBase, LicenseFacetNetwork}
	case LicenseTierNP:
		return []LicenseFacet{LicenseFacetBase, LicenseFacetNetwork, LicenseFacetPredict}
	case LicenseTierS:
		return []LicenseFacet{LicenseFacetBase, LicenseFacetSecurity}
	case LicenseTierNS:
		return []LicenseFacet{LicenseFacetBase, LicenseFacetNetwork, LicenseFacetSecurity}
	case LicenseTierNSP:
		return []LicenseFacet{LicenseFacetBase, LicenseFacetNetwork, LicenseFacetSecurity, LicenseFacetPredict}
	}
	return nil
}

// Facets returns the facets the license's tier includes (see TierFacets); nil when it has no tier.
func (l License) Facets() []LicenseFacet { return TierFacets(l.Tier) }

// UnmarshalJSON derives Stackable from replaces, which is the only one the server sends.
func (l *License) UnmarshalJSON(data []byte) error {
	type plain License
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Replaces != nil {
		decoded.Stackable = !*decoded.Replaces
	}
	*l = License(decoded)
	return nil
}

// DecodedLicenseKey is what a preview returns (web/.../json/license/
// DecodedLicenseKey.java). The verdict field is `status` on every version.
// 26.8.x also sent `licenseStatus` and `used`; 26.9 dropped both and sends
// `started` (whether the licence period has begun) with the reasons that
// used to live in licenseStatus/used folded into Status (WRONG_TIER,
// ALREADY_USED, EXPIRED, ...). Read the verdict through Status and
// AlreadyUsed(), not the raw fields, so both shapes read the same.
type DecodedLicenseKey struct {
	Status        LicenseKeyStatus `json:"status"`
	License       *License         `json:"license,omitempty"`
	LicenseStatus string           `json:"licenseStatus,omitempty"` // 26.8.x only
	Used          bool             `json:"used,omitempty"`          // 26.8.x only
	Started       *bool            `json:"started,omitempty"`       // 26.9+
}

// AlreadyUsed reports that the org already holds this exact licence -- "would
// work" and "already applied" otherwise both look like success. 26.8.x said it
// with `used:true`, 26.9 with status ALREADY_USED.
func (d *DecodedLicenseKey) AlreadyUsed() bool {
	return d != nil && (d.Used || d.Status == LicenseKeyAlreadyUsed)
}

// LicenseDetails is a licence as the org holds it: ForwardSupportLicenseController's
// LicenseDetailsWithStatus (ListForOrg) or LicenseController's (List).
//
// The server sends no top-level id: the id is License.ID, and ID is filled from it when the
// body carries none (a body that does state one keeps it). The signed key itself IS sent
// (signedLicenseKey, with the instance fingerprint) once a licence is activated; it is
// deliberately not carried here, so it cannot leak through a %+v log line. Signed records
// that it was there.
type LicenseDetails struct {
	ID      string   `json:"id,omitempty"`
	License *License `json:"license,omitempty"`
	// Status is the licence's state by its dates (NOT_ACTIVE_YET, ACTIVE, EXPIRED) or
	// REPLACED once a replacement licence ended it. For a licence nobody has activated it is
	// date-based only, so an ACTIVE status does not mean the org is using the licence.
	Status string `json:"status,omitempty"`
	// Invalidated is true when support invalidated the licence, which prevents it from ever
	// being activated. Absent from the body when false.
	Invalidated bool `json:"invalidated,omitempty"`
	// Signed is true when the body carried a signedLicenseKey: the licence has been
	// activated. This is the test Forward itself refuses an activation on
	// (LicenseDetails.isSigned). Releases before 23.6 cleared the key of an old licence when
	// its renewal was activated, so false is not proof for an old expired licence.
	Signed bool `json:"signed,omitempty"`
	// UsedAt is when the licence was activated (ISO-8601, like StartsAt). Forward leaves it
	// null for a licence not yet activated, one in a Software Central org and most licences
	// activated before 23.6, and an on-prem upload sets it too. Empty when absent.
	UsedAt string `json:"usedAt,omitempty"`
}

// UnmarshalJSON fills ID from the inner licence when the body states none, and records
// whether a signed key was present without keeping the key.
func (d *LicenseDetails) UnmarshalJSON(data []byte) error {
	type plain LicenseDetails
	aux := struct {
		plain
		SignedLicenseKey json.RawMessage `json:"signedLicenseKey"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	decoded := aux.plain
	if decoded.ID == "" && decoded.License != nil {
		decoded.ID = decoded.License.ID
	}
	if k := bytes.TrimSpace(aux.SignedLicenseKey); len(k) > 0 && string(k) != "null" {
		decoded.Signed = true
	}
	*d = LicenseDetails(decoded)
	return nil
}

// Available reports that the licence is stored for the org and a support operator could
// activate it: not invalidated, not signed, not EXPIRED, and a type that can still be
// activated (not LEGACY, DEVICE, CLOUD or ENDPOINT, which Forward no longer supports). It is
// the test the support UI uses for its "Available" label (gui .../saas-org-licenses-drawer.ts
// getStatus), so it is Forward's own inference and not a field the server sends. It reads
// the ListForOrg listing; on List it is meaningless, because an ordinary org's own listing
// holds only signed licences. Even an Available licence can be refused at activation, for
// example: a stacking tier licence whose tier differs from the org's (see License.Replaces), an
// unmet cloud-credit debt, a pending replacement, a tier-trial licence outside the tier's
// period, or a licence that has expired since the listing.
func (d LicenseDetails) Available() bool {
	if d.Invalidated || d.Signed || d.Status == "EXPIRED" {
		return false
	}
	if d.License != nil {
		switch d.License.Type {
		case "LEGACY", "DEVICE", "CLOUD", "ENDPOINT":
			return false
		}
	}
	return true
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
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses?action=decode", AsciiCodedSignedLicenseKey{SignedLicenseKey: key})
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
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/licenses", AsciiCodedSignedLicenseKey{SignedLicenseKey: key})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.Apply")
	out := new(LicenseDetails)
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
// (@RequiresForwardAdmin). SaaS deployments only: an on-prem appserver does not map the
// route (ErrEndpointNotServed), see ListForOrg.
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
// PLATFORM-scoped (@RequiresForwardSupport), and SaaS deployments only (on-prem answers
// ErrEndpointNotServed, see ListForOrg). Prefer this over RemoveAllForOrg
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
func (s *LicensingService) ActivateForOrg(ctx context.Context, orgID, licenseID, fingerprint string) (*LicenseDetails, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	licenseID = strings.TrimSpace(licenseID)
	if orgID == "" {
		return nil, nil, ErrLicenseOrgIDRequired
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
	out := new(LicenseDetails)
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
func (s *LicensingService) List(ctx context.Context) ([]LicenseDetails, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/licenses", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.List")
	result := listResponse[LicenseDetails]{Keys: []string{"licenses"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// LicenseTierAndStatus is the organization's license tier and state. Both are
// empty for an unlicensed organization: the response is still a JSON object,
// never a 404. The values are Forward's LicenseTier and OrgLicenseStatus names,
// which are version-dependent, so they stay strings.
type LicenseTierAndStatus struct {
	LicenseTier   string `json:"licenseTier,omitempty"`
	LicenseStatus string `json:"licenseStatus,omitempty"`
}

// TierAndStatus returns the signed-in user's organization's license tier and
// status. GET /api/licenses?view=status (LicenseController; any signed-in
// user; served on every deployment).
func (s *LicensingService) TierAndStatus(ctx context.Context) (*LicenseTierAndStatus, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/licenses?view=status", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Licensing.TierAndStatus")
	out := new(LicenseTierAndStatus)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
