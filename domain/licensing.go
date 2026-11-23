package domain

import (
	"bytes"
	"encoding/json"
	"errors"
)

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

// LicenseTierAndStatus is the organization's license tier and state. Both are
// empty for an unlicensed organization: the response is still a JSON object,
// never a 404. The values are Forward's LicenseTier and OrgLicenseStatus names,
// which are version-dependent, so they stay strings.
type LicenseTierAndStatus struct {
	LicenseTier   string `json:"licenseTier,omitempty"`
	LicenseStatus string `json:"licenseStatus,omitempty"`
}
