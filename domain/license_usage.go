package domain

import (
	"encoding/json"
	"time"
)

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

// OrgUsageSummary is one SaaS org's counted usage (OrgPlatformUsage): licensed standard and lite devices, edge assets and CCH used.
type OrgUsageSummary struct {
	OrgID                Identifier `json:"orgId"`
	StandardDeviceCount  int        `json:"standardDeviceCount"`
	LiteDeviceCount      int        `json:"liteDeviceCount"`
	EdgeAssetCount       int64      `json:"edgeAssetCount"`
	CloudCreditHoursUsed int64      `json:"cloudCreditHoursUsed"`
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

// EdgeAssetCounts is the caller's edge-asset use against its cap (LicensedEdgeAssetsCount). An asset over the cap is unlicensed.
type EdgeAssetCounts struct {
	EdgeAssetCount           int `json:"edgeAssetCount"`
	UnlicensedEdgeAssetCount int `json:"unlicensedEdgeAssetCount"`
	EdgeAssetCap             int `json:"edgeAssetCap"`
}
