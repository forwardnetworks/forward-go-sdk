package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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

// OrgUsage reads one org's device usage and CCH total (GET /api/orgs/{orgId}/platform-usage). Needs a Forward-support login and an org
// that is not Software Central (else 400). SaaS only: an on-prem appserver answers ErrEndpointNotServed. Count Devices by Licensed and Lite
// for standard and lite use.
func (s *Licensing) OrgUsage(ctx context.Context, orgID string) (*ports.PlatformUsage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := orgUsagePath(orgID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.PlatformUsage)
	resp, err := s.usageGet(ctx, path, out, "Licensing.OrgUsage")
	return out, resp, err
}

// OrgCloudUsage reads one org's cloud accounting, including the CCH used under each license (GET /api/orgs/{orgId}/platform-usage?view=vpc).
// Needs a Forward-support login; not for a Software Central org. This is the way to read another org's per-license CCH without
// impersonating; CCHHistory only reads the credential's own org. Period.EndReason is LICENSE_STARTED, LICENSE_EXPIRED, LICENSE_REPLACED,
// LICENSE_CONSUMED or GRACE_PERIOD_ELAPSED.
func (s *Licensing) OrgCloudUsage(ctx context.Context, orgID string) (*ports.CloudUsage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := orgUsagePath(orgID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.CloudUsage)
	resp, err := s.usageGet(ctx, path+"?view=vpc", out, "Licensing.OrgCloudUsage")
	return out, resp, err
}

func orgUsagePath(orgID string) (string, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return "", ports.ErrLicenseOrgIDRequired
	}
	return "/api/orgs/" + url.PathEscape(orgID) + "/platform-usage", nil
}

func (s *Licensing) usageGet(ctx context.Context, path string, out any, operation string) (*ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// AllOrgUsage reads the counted usage of every enabled SaaS org the login supports (GET /api/admin/platform-usage; Forward-support login;
// a Forward admin sees all). SaaS only: an on-prem appserver answers ErrEndpointNotServed (ForwardSupportLicenseController is not mapped
// there). It is the one read that gives edge-asset use for another org. Pair it with SaaSOrgInfos for the caps.
func (s *Licensing) AllOrgUsage(ctx context.Context) ([]ports.OrgUsageSummary, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.OrgUsageSummary]{Keys: []string{"usage"}}
	resp, err := s.usageGet(ctx, "/api/admin/platform-usage", &out, "Licensing.AllOrgUsage")
	return out.Items, resp, err
}

// SaaSOrgInfos reads the tier, status and summed caps of every enabled SaaS org the login supports (GET /api/admin/licenses?view=saas;
// Forward-support login; SaaS only, ErrEndpointNotServed on-prem). Status is Forward's OrgLicenseStatus: ACTIVE, EXEMPT, NOT_ACTIVE_YET, EXPIRED_RECENTLY or EXPIRED.
func (s *Licensing) SaaSOrgInfos(ctx context.Context) ([]ports.SaaSOrgInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.SaaSOrgInfo]{Keys: []string{"orgs"}}
	resp, err := s.usageGet(ctx, "/api/admin/licenses?view=saas", &out, "Licensing.SaaSOrgInfos")
	return out.Items, resp, err
}

// Usage reads the CALLER'S OWN org's device usage and CCH total (GET /api/platform-usage; VIEW_LICENSING; not Software Central).
func (s *Licensing) Usage(ctx context.Context) (*ports.PlatformUsage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.PlatformUsage)
	resp, err := s.usageGet(ctx, "/api/platform-usage", out, "Licensing.Usage")
	return out, resp, err
}

// CCHUsage reads the caller's own org's CCH used and licensed (GET /api/platform-usage?view=cch; any signed-in user).
func (s *Licensing) CCHUsage(ctx context.Context) (*ports.CCHUsage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.CCHUsage)
	resp, err := s.usageGet(ctx, "/api/platform-usage?view=cch", out, "Licensing.CCHUsage")
	return out, resp, err
}

// CCHUsedSince reads the CCH the caller's own org used in the last days days (GET /api/platform-usage?view=cch&days=).
func (s *Licensing) CCHUsedSince(ctx context.Context, days int) (int64, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return 0, nil, err
	}
	if days <= 0 {
		return 0, nil, errors.New("forward: days must be positive")
	}
	var out struct {
		CCH int64 `json:"cch"`
	}
	resp, err := s.usageGet(ctx, fmt.Sprintf("/api/platform-usage?view=cch&days=%d", days), &out, "Licensing.CCHUsedSince")
	return out.CCH, resp, err
}

// CCHHistory reads the CALLER'S OWN org's CCH per license (GET /api/platform-usage?view=cch-history; VIEW_LICENSING; not Software Central).
// A zero start or end is left out: end defaults to now, start to the beginning. It cannot be pointed at another org by id; see OrgCloudUsage.
func (s *Licensing) CCHHistory(ctx context.Context, start, end time.Time) (*ports.CCHHistory, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.CCHHistory)
	resp, err := s.usageGet(ctx, "/api/platform-usage?"+query.Encode(), out, "Licensing.CCHHistory")
	return out, resp, err
}

// CCHDebug returns everything Forward holds to reproduce the caller's CCH history calculation (GET /api/platform-usage?view=cch-debug):
// updatedAt, licenses, periods and counts, left raw because Forward builds it for support to read.
func (s *Licensing) CCHDebug(ctx context.Context) (map[string]json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var out map[string]json.RawMessage
	resp, err := s.usageGet(ctx, "/api/platform-usage?view=cch-debug", &out, "Licensing.CCHDebug")
	return out, resp, err
}

// DeviceCounts reads the caller's own org's licensed and unlicensed device counts and caps (GET /api/licensed-devices?view=count;
// VIEW_LICENSE_CAPACITY; not Software Central).
func (s *Licensing) DeviceCounts(ctx context.Context) (*ports.DeviceCounts, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.DeviceCounts)
	resp, err := s.usageGet(ctx, "/api/licensed-devices?view=count", out, "Licensing.DeviceCounts")
	return out, resp, err
}

// LicensedDevices lists the caller's own org's licensed and unlicensed devices (GET /api/licensed-devices; VIEW_LICENSING).
func (s *Licensing) LicensedDevices(ctx context.Context) (*ports.LicensedDevices, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.LicensedDevices)
	resp, err := s.usageGet(ctx, "/api/licensed-devices", out, "Licensing.LicensedDevices")
	return out, resp, err
}

// UsageHistory reads the caller's own org's device and edge-asset use against caps over a period (GET /api/metrics/license-usage?start=&end=;
// VIEW_LICENSE_CAPACITY). Both bounds are required.
func (s *Licensing) UsageHistory(ctx context.Context, start, end time.Time) ([]ports.UsagePoint, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil, nil, errors.New("forward: start and end are required, with end after start")
	}
	query := url.Values{"start": []string{start.UTC().Format(time.RFC3339Nano)}, "end": []string{end.UTC().Format(time.RFC3339Nano)}}
	out := listResponse[ports.UsagePoint]{Keys: []string{"events"}}
	resp, err := s.usageGet(ctx, "/api/metrics/license-usage?"+query.Encode(), &out, "Licensing.UsageHistory")
	return out.Items, resp, err
}

// EdgeAssetCounts reads the caller's own org's licensed and unlicensed edge-asset counts and cap (GET /api/licensed-edge-assets?view=count;
// VIEW_LICENSE_CAPACITY; not Software Central). For another org's count use AllOrgUsage (SaaS) or impersonate.
func (s *Licensing) EdgeAssetCounts(ctx context.Context) (*ports.EdgeAssetCounts, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	out := new(ports.EdgeAssetCounts)
	resp, err := s.usageGet(ctx, "/api/licensed-edge-assets?view=count", out, "Licensing.EdgeAssetCounts")
	return out, resp, err
}
