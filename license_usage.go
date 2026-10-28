package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// License usage: what an org has consumed against what its licenses grant, in the three units Forward licenses by: classic
// devices (standard and lite), cloud credit hours (CCH) and edge assets. Preview: appserver controllers
// (PlatformUsageController, ForwardSupportLicenseController, MetricController), none in the published spec.
//
// WHO CAN READ WHICH ORG. Only the /orgs/{orgId}/platform-usage reads (OrgUsage, OrgCloudUsage) and the /admin reads (AllOrgUsage,
// SaaSOrgInfos) take an org from the caller, and they need a Forward-support login. Every other read here acts on the org of the
// credential used: Forward takes the org from the session, so a support login cannot point CCHHistory or DeviceCounts at another
// org by id. It can reach one by impersonating a user of that org (Browser.ImpersonatedClient), and then they answer for that org.
// For another org's per-license CCH without impersonating, use OrgCloudUsage (its Periods carry the CCH used per license).
// The own-org reads other than UsageHistory answer 400 for a Software Central org (on-prem licensing).

// DeviceUsage is one device Forward counts toward a license (DeviceUsageInfo). The signature is left raw: its shape differs by
// vendor and this SDK does not model it.
type DeviceUsage struct {
	TimeOfFirstSnapshot string `json:"timeOfFirstSnapshot,omitempty"`
	TimeOfLastSnapshot  string `json:"timeOfLastSnapshot,omitempty"`
	Licensed            bool   `json:"licensed"`
	IneligibleReason    string `json:"ineligibleReason,omitempty"`
	// Lite is true for a lite (peripheral, e.g. access point) device and absent for a standard one.
	Lite            bool            `json:"lite,omitempty"`
	DeviceSignature json.RawMessage `json:"deviceSignature,omitempty"`
	Networks        json.RawMessage `json:"networks,omitempty"`
	Replacements    json.RawMessage `json:"replacements,omitempty"`
}

// PlatformUsage is an org's device usage and CCH total (GET .../platform-usage).
type PlatformUsage struct {
	Devices              []DeviceUsage `json:"devices"`
	CloudCreditHoursUsed int64         `json:"cloudCreditHoursUsed"`
}

// CCHPeriod is a span of CCH accounting under one license (CchPeriod). CCHUsed is cumulative inside the period. A new period starts
// when a license starts, ends or is replaced, which is how usage is attributed to a license.
type CCHPeriod struct {
	Start       string     `json:"start"`
	LicenseID   Identifier `json:"licenseId,omitempty"`
	GracePeriod bool       `json:"gracePeriod,omitempty"`
	CCHUsed     float64    `json:"cchUsed"`
	EndReason   string     `json:"endReason,omitempty"`
}

// CCHLicense is a license that grants (or replaces) cloud credit hours (CchLicense).
type CCHLicense struct {
	ID        Identifier `json:"id"`
	StartsAt  string     `json:"startsAt,omitempty"`
	StartedAt string     `json:"startedAt,omitempty"`
	ExpiresAt string     `json:"expiresAt,omitempty"`
	CCH       int        `json:"cch"`
	Replaces  bool       `json:"replaces,omitempty"`
	UsedAt    string     `json:"usedAt,omitempty"`
}

// CloudUsage is an org's cloud accounting (CloudUsage): per-license CCH periods plus the VPCs seen.
type CloudUsage struct {
	UpdatedAt string          `json:"updatedAt"`
	Periods   []CCHPeriod     `json:"periods"`
	Licenses  []CCHLicense    `json:"licenses"`
	VPCs      json.RawMessage `json:"vpcUsageInfos,omitempty"`
}

// OrgUsage reads one org's device usage and CCH total (GET /api/orgs/{orgId}/platform-usage). Needs a Forward-support login and an org
// that is not Software Central (else 400). SaaS only: an on-prem appserver answers ErrEndpointNotServed. Count Devices by Licensed and Lite
// for standard and lite use.
func (s *LicensingService) OrgUsage(ctx context.Context, orgID string) (*PlatformUsage, *Response, error) {
	path, err := orgUsagePath(orgID)
	if err != nil {
		return nil, nil, err
	}
	out := new(PlatformUsage)
	resp, err := s.usageGet(ctx, path, out, "Licensing.OrgUsage")
	return out, resp, err
}

// OrgCloudUsage reads one org's cloud accounting, including the CCH used under each license (GET /api/orgs/{orgId}/platform-usage?view=vpc).
// Needs a Forward-support login; not for a Software Central org. This is the way to read another org's per-license CCH without
// impersonating; CCHHistory only reads the credential's own org. Period.EndReason is LICENSE_STARTED, LICENSE_EXPIRED, LICENSE_REPLACED,
// LICENSE_CONSUMED or GRACE_PERIOD_ELAPSED.
func (s *LicensingService) OrgCloudUsage(ctx context.Context, orgID string) (*CloudUsage, *Response, error) {
	path, err := orgUsagePath(orgID)
	if err != nil {
		return nil, nil, err
	}
	out := new(CloudUsage)
	resp, err := s.usageGet(ctx, path+"?view=vpc", out, "Licensing.OrgCloudUsage")
	return out, resp, err
}

func orgUsagePath(orgID string) (string, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return "", ErrLicenseOrgIDRequired
	}
	return "/api/orgs/" + url.PathEscape(orgID) + "/platform-usage", nil
}

func (s *LicensingService) usageGet(ctx context.Context, path string, out any, operation string) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// OrgUsageSummary is one SaaS org's counted usage (OrgPlatformUsage): licensed standard and lite devices, edge assets and CCH used.
type OrgUsageSummary struct {
	OrgID                Identifier `json:"orgId"`
	StandardDeviceCount  int        `json:"standardDeviceCount"`
	LiteDeviceCount      int        `json:"liteDeviceCount"`
	EdgeAssetCount       int64      `json:"edgeAssetCount"`
	CloudCreditHoursUsed int64      `json:"cloudCreditHoursUsed"`
}

// AllOrgUsage reads the counted usage of every enabled SaaS org the login supports (GET /api/admin/platform-usage; Forward-support login;
// a Forward admin sees all). SaaS only: an on-prem appserver answers ErrEndpointNotServed (ForwardSupportLicenseController is not mapped
// there). It is the one read that gives edge-asset use for another org. Pair it with SaaSOrgInfos for the caps.
func (s *LicensingService) AllOrgUsage(ctx context.Context) ([]OrgUsageSummary, *Response, error) {
	out := listResponse[OrgUsageSummary]{Keys: []string{"usage"}}
	resp, err := s.usageGet(ctx, "/api/admin/platform-usage", &out, "Licensing.AllOrgUsage")
	return out.Items, resp, err
}

// SaaSOrgInfo is one SaaS org's tier, status and total caps (SaasOrgLicenseInfo). The caps are nil unless Status is ACTIVE; Tier and
// Status are empty for an org with no status (and Tier for an EXPIRED one).
type SaaSOrgInfo struct {
	OrgID                 Identifier `json:"orgId"`
	Tier                  string     `json:"tier"`
	Status                string     `json:"status"`
	StandardDeviceCap     *int       `json:"standardDeviceCap,omitempty"`
	LiteDeviceCap         *int       `json:"liteDeviceCap,omitempty"`
	CloudCreditHours      *int       `json:"cloudCreditHours,omitempty"`
	EdgeAssetCap          *int       `json:"edgeAssetCap,omitempty"`
	NextLicenseExpiration string     `json:"nextLicenseExpiration,omitempty"`
}

// Facets returns the facets the org's tier includes.
func (i SaaSOrgInfo) Facets() []LicenseFacet { return TierFacets(i.Tier) }

// SaaSOrgInfos reads the tier, status and summed caps of every enabled SaaS org the login supports (GET /api/admin/licenses?view=saas;
// Forward-support login; SaaS only, ErrEndpointNotServed on-prem). Status is Forward's OrgLicenseStatus: ACTIVE, EXEMPT, NOT_ACTIVE_YET, EXPIRED_RECENTLY or EXPIRED.
func (s *LicensingService) SaaSOrgInfos(ctx context.Context) ([]SaaSOrgInfo, *Response, error) {
	out := listResponse[SaaSOrgInfo]{Keys: []string{"orgs"}}
	resp, err := s.usageGet(ctx, "/api/admin/licenses?view=saas", &out, "Licensing.SaaSOrgInfos")
	return out.Items, resp, err
}

// Usage reads the CALLER'S OWN org's device usage and CCH total (GET /api/platform-usage; VIEW_LICENSING; not Software Central).
func (s *LicensingService) Usage(ctx context.Context) (*PlatformUsage, *Response, error) {
	out := new(PlatformUsage)
	resp, err := s.usageGet(ctx, "/api/platform-usage", out, "Licensing.Usage")
	return out, resp, err
}

// CCHUsage is the caller's CCH used against licensed (LicensedCchUsage). Used past Licensed is debt (see Over); Forward lets the org keep
// using CCH until GracePeriodEndTime, after which it stops (the critical states LICENSE_CCH_EXHAUSTED and LICENSE_CCH_GRACE_PERIOD_ENDED).
type CCHUsage struct {
	CCHUsed            int64  `json:"cchUsed"`
	CCHLicensed        int64  `json:"cchLicensed"`
	Instant            string `json:"instant,omitempty"`
	GracePeriodEndTime string `json:"gracePeriodEndTime,omitempty"`
}

// Over is how many CCH the org has used beyond what it is licensed for (zero when within).
func (u CCHUsage) Over() int64 {
	if u.CCHUsed > u.CCHLicensed {
		return u.CCHUsed - u.CCHLicensed
	}
	return 0
}

// Remaining is the licensed CCH not yet used (zero when over).
func (u CCHUsage) Remaining() int64 {
	if u.CCHLicensed > u.CCHUsed {
		return u.CCHLicensed - u.CCHUsed
	}
	return 0
}

// CCHUsage reads the caller's own org's CCH used and licensed (GET /api/platform-usage?view=cch; any signed-in user).
func (s *LicensingService) CCHUsage(ctx context.Context) (*CCHUsage, *Response, error) {
	out := new(CCHUsage)
	resp, err := s.usageGet(ctx, "/api/platform-usage?view=cch", out, "Licensing.CCHUsage")
	return out, resp, err
}

// CCHUsedSince reads the CCH the caller's own org used in the last days days (GET /api/platform-usage?view=cch&days=).
func (s *LicensingService) CCHUsedSince(ctx context.Context, days int) (int64, *Response, error) {
	if days <= 0 {
		return 0, nil, errors.New("forward: days must be positive")
	}
	var out struct {
		CCH int64 `json:"cch"`
	}
	resp, err := s.usageGet(ctx, fmt.Sprintf("/api/platform-usage?view=cch&days=%d", days), &out, "Licensing.CCHUsedSince")
	return out.CCH, resp, err
}

// CCHLicenseUsage is one CCH license with its cumulative CCH over time (CchLicenseWithUsage). Usage maps an instant, as Forward writes
// it, to the cumulative CCH used under this license by then; it never decreases. Replaced means a later license ended this one early.
type CCHLicenseUsage struct {
	CCHLicense
	Usage    map[string]int64 `json:"usage"`
	Replaced bool             `json:"replaced,omitempty"`
}

// LastUsed is the cumulative CCH at the newest instant in Usage (zero when none).
func (l CCHLicenseUsage) LastUsed() int64 {
	var used int64
	for _, v := range l.Usage { // cumulative, so the largest is the newest
		used = max(used, v)
	}
	return used
}

// Remaining is the license's CCH less LastUsed, never negative: the available hours Forward's chart draws for it.
func (l CCHLicenseUsage) Remaining() int64 { return max(int64(l.CCH)-l.LastUsed(), 0) }

// CCHHistory is the caller's CCH per license over time (CchHistory).
type CCHHistory struct {
	InstanceCounts map[string]json.RawMessage `json:"instanceCounts"`
	Licenses       []CCHLicenseUsage          `json:"licenses"`
}

// CCHHistory reads the CALLER'S OWN org's CCH per license (GET /api/platform-usage?view=cch-history; VIEW_LICENSING; not Software Central).
// A zero start or end is left out: end defaults to now, start to the beginning. It cannot be pointed at another org by id; see OrgCloudUsage.
func (s *LicensingService) CCHHistory(ctx context.Context, start, end time.Time) (*CCHHistory, *Response, error) {
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return nil, nil, errors.New("forward: start must be before end")
	}
	query := url.Values{"view": []string{"cch-history"}}
	if !start.IsZero() {
		query.Set("start", start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		query.Set("end", end.UTC().Format(time.RFC3339Nano))
	}
	out := new(CCHHistory)
	resp, err := s.usageGet(ctx, "/api/platform-usage?"+query.Encode(), out, "Licensing.CCHHistory")
	return out, resp, err
}

// CCHDebug returns everything Forward holds to reproduce the caller's CCH history calculation (GET /api/platform-usage?view=cch-debug):
// updatedAt, licenses, periods and counts, left raw because Forward builds it for support to read.
func (s *LicensingService) CCHDebug(ctx context.Context) (map[string]json.RawMessage, *Response, error) {
	var out map[string]json.RawMessage
	resp, err := s.usageGet(ctx, "/api/platform-usage?view=cch-debug", &out, "Licensing.CCHDebug")
	return out, resp, err
}

// DeviceCounts is the caller's classic-device use against caps (LicensedDevicesCount): licensed and unlicensed counts and the caps, each for
// standard and lite devices. A device over the cap is unlicensed.
type DeviceCounts struct {
	StandardDeviceCount           int `json:"standardDeviceCount"`
	LiteDeviceCount               int `json:"liteDeviceCount"`
	StandardUnlicensedDeviceCount int `json:"standardUnlicensedDeviceCount"`
	LiteUnlicensedDeviceCount     int `json:"liteUnlicensedDeviceCount"`
	StandardDeviceCap             int `json:"standardDeviceCap"`
	LiteDeviceCap                 int `json:"liteDeviceCap"`
}

// DeviceCounts reads the caller's own org's licensed and unlicensed device counts and caps (GET /api/licensed-devices?view=count;
// VIEW_LICENSE_CAPACITY; not Software Central).
func (s *LicensingService) DeviceCounts(ctx context.Context) (*DeviceCounts, *Response, error) {
	out := new(DeviceCounts)
	resp, err := s.usageGet(ctx, "/api/licensed-devices?view=count", out, "Licensing.DeviceCounts")
	return out, resp, err
}

// LicensedDevice is one device in the licensed-devices listing.
type LicensedDevice struct {
	Signature        json.RawMessage `json:"signature,omitempty"`
	Names            []string        `json:"names"`
	NetworkIDs       []Identifier    `json:"networkIds"`
	FirstSnapshotAt  string          `json:"firstSnapshotAt,omitempty"`
	LastSnapshotAt   string          `json:"lastSnapshotAt,omitempty"`
	IneligibleReason string          `json:"ineligibleReason,omitempty"`
	Lite             bool            `json:"lite,omitempty"`
}

// LicensedDevices lists which devices are and are not licensed (LicensedDevices).
type LicensedDevices struct {
	Devices           []LicensedDevice  `json:"devices"`
	UnlicensedDevices []LicensedDevice  `json:"unlicensedDevices"`
	NetworkNames      map[string]string `json:"networkNames"`
	CurrentTime       string            `json:"currentTime,omitempty"`
}

// LicensedDevices lists the caller's own org's licensed and unlicensed devices (GET /api/licensed-devices; VIEW_LICENSING).
func (s *LicensingService) LicensedDevices(ctx context.Context) (*LicensedDevices, *Response, error) {
	out := new(LicensedDevices)
	resp, err := s.usageGet(ctx, "/api/licensed-devices", out, "Licensing.LicensedDevices")
	return out, resp, err
}

// UsagePoint is the caller's use against caps at one moment (LicenseUsageEvent), for standard and lite devices and edge assets. Timestamp is
// Forward's numeric instant: whole milliseconds since the epoch (see Time). The caps move when licenses start or end, so the series is the
// per-moment answer to "how many were used against how many were licensed", not a per-license split.
type UsagePoint struct {
	Timestamp           int64 `json:"timestamp"`
	StandardDeviceCap   int   `json:"standardDeviceCap"`
	StandardDeviceUsage int   `json:"standardDeviceUsage"`
	LiteDeviceCap       int   `json:"liteDeviceCap"`
	LiteDeviceUsage     int   `json:"liteDeviceUsage"`
	EdgeAssetCap        int   `json:"edgeAssetCap"`
	EdgeAssetUsage      int   `json:"edgeAssetUsage"`
}

// Time is the point's instant.
func (p UsagePoint) Time() time.Time { return time.UnixMilli(p.Timestamp).UTC() }

// UsageHistory reads the caller's own org's device and edge-asset use against caps over a period (GET /api/metrics/license-usage?start=&end=;
// VIEW_LICENSE_CAPACITY). Both bounds are required.
func (s *LicensingService) UsageHistory(ctx context.Context, start, end time.Time) ([]UsagePoint, *Response, error) {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil, nil, errors.New("forward: start and end are required, with end after start")
	}
	query := url.Values{"start": []string{start.UTC().Format(time.RFC3339Nano)}, "end": []string{end.UTC().Format(time.RFC3339Nano)}}
	out := listResponse[UsagePoint]{Keys: []string{"events"}}
	resp, err := s.usageGet(ctx, "/api/metrics/license-usage?"+query.Encode(), &out, "Licensing.UsageHistory")
	return out.Items, resp, err
}

// EdgeAssetCounts is the caller's edge-asset use against its cap (LicensedEdgeAssetsCount). An asset over the cap is unlicensed.
type EdgeAssetCounts struct {
	EdgeAssetCount           int `json:"edgeAssetCount"`
	UnlicensedEdgeAssetCount int `json:"unlicensedEdgeAssetCount"`
	EdgeAssetCap             int `json:"edgeAssetCap"`
}

// EdgeAssetCounts reads the caller's own org's licensed and unlicensed edge-asset counts and cap (GET /api/licensed-edge-assets?view=count;
// VIEW_LICENSE_CAPACITY; not Software Central). For another org's count use AllOrgUsage (SaaS) or impersonate.
func (s *LicensingService) EdgeAssetCounts(ctx context.Context) (*EdgeAssetCounts, *Response, error) {
	out := new(EdgeAssetCounts)
	resp, err := s.usageGet(ctx, "/api/licensed-edge-assets?view=count", out, "Licensing.EdgeAssetCounts")
	return out, resp, err
}
