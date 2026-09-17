package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudManagedSetupsService manages CLOUD-managed setups: the record that
// tells Forward a SaaS wireless controller owns a set of access points, which
// Forward then collects through the vendor's cloud API rather than device by
// device. Today that is Juniper Mist (type=MIST); Meraki has its own shape and
// is not covered here.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/CloudManagedSetupController.java (add/discover/test/
// delete, all gated by org property MIST_ONBOARDING, default true) and
// web/.../json/mist/CreateMistSetup.java for the request shape.
type CloudManagedSetupsService service

// MistRegion is Forward's own enum of Mist regions; the collector derives the
// API hostname from it (MistRegion.getHostNameForApi) and there is NO base-URL
// override. GLOBAL_01 is api.mist.com.
type MistRegion string

const (
	MistRegionGlobal01 MistRegion = "GLOBAL_01" // api.mist.com
	MistRegionGlobal02 MistRegion = "GLOBAL_02" // api.gc1.mist.com
	MistRegionGlobal03 MistRegion = "GLOBAL_03" // api.ac2.mist.com
	MistRegionGlobal04 MistRegion = "GLOBAL_04" // api.gc2.mist.com
	MistRegionGlobal05 MistRegion = "GLOBAL_05" // api.gc4.mist.com
	MistRegionEMEA01   MistRegion = "EMEA_01"   // api.eu.mist.com
	MistRegionEMEA02   MistRegion = "EMEA_02"   // api.gc3.mist.com
	MistRegionEMEA03   MistRegion = "EMEA_03"   // api.ac6.mist.com
	MistRegionEMEA04   MistRegion = "EMEA_04"   // api.gc6.mist.com
	MistRegionAPAC01   MistRegion = "APAC_01"   // api.ac5.mist.com
	MistRegionAPAC02   MistRegion = "APAC_02"   // api.gc5.mist.com
	MistRegionAPAC03   MistRegion = "APAC_03"   // api.gc7.mist.com
)

// NewMistSetup is the body of POST .../cloud-managed-setups?type=MIST.
//
// There is no org id and no site list: Forward discovers the org from
// GET /api/v1/self with the API key, and walks every site. Hosts is an
// optional collect filter of AP MACs; discovery RESETS it to empty, and an
// empty filter collects every AP in the inventory, so leave it empty.
type NewMistSetup struct {
	Name string `json:"name"`
	// Region selects the hard-coded Mist API hostname. Required.
	Region MistRegion `json:"region"`
	// APIKeyID is the id of an HTTP credential of type API_KEY
	// (CredentialsService.CreateHTTP with Type "API_KEY") whose password is
	// the Mist API token. Required.
	APIKeyID string `json:"apiKeyId"`
	// Collect: nil/true participates in collections; false parks the setup.
	Collect *bool `json:"collect,omitempty"`
	// CollectorID pins the collector; empty means the network's default one.
	// It is Forward's NUMERIC collector id (the controller parses it as a
	// Long -- passing the collector UUID Skyforge records as
	// forwardCollectorId fails with `400 For input string: "<uuid>"`,
	// measured 2026-09-17). Creation does NOT need the collector online --
	// discovery does.
	CollectorID string   `json:"collectorId,omitempty"`
	Hosts       []string `json:"hosts,omitempty"`
	Concurrency *int     `json:"concurrency,omitempty"`
}

// MistSetup is a setup as Forward returns it.
type MistSetup struct {
	Name        string     `json:"name"`
	Type        string     `json:"type,omitempty"`
	Region      MistRegion `json:"region,omitempty"`
	APIKeyID    string     `json:"apiKeyId,omitempty"`
	Collect     *bool      `json:"collect,omitempty"`
	CollectorID string     `json:"collectorId,omitempty"`
	// Hosts on the READ side is the AP list Forward knows for this setup. It
	// is written as strings (MACs) but read back as OBJECTS -- measured live
	// 2026-09-17: after discover, GET returned hosts:[{name,model,displayName}]
	// and a []string decode failed. MistHostRef accepts either.
	Hosts []MistHostRef `json:"hosts,omitempty"`
	// TestResult is filled by discover/test; DiscoveredHosts is the AP list the
	// collector saw. Informational: collection does not depend on it.
	TestResult *MistTestResult `json:"testResult,omitempty"`
}

// MistHostRef is one AP as a setup lists it: a bare MAC string in requests
// and older responses, an object after discovery. Name is always the MAC.
type MistHostRef struct {
	Name        string `json:"name"`
	Model       string `json:"model,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// UnmarshalJSON accepts "0250aa010001" or {"name":"0250aa010001",...}.
func (h *MistHostRef) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*h = MistHostRef{Name: s}
		return nil
	}
	type raw MistHostRef
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*h = MistHostRef(r)
	return nil
}

// MistTestResult is the last discovery outcome as the setup persists it.
// Measured live 2026-09-17: {"savedAt":"...","discoveredHosts":[...]}.
type MistTestResult struct {
	SavedAt         string               `json:"savedAt,omitempty"`
	Status          string               `json:"status,omitempty"`
	Message         string               `json:"message,omitempty"`
	DiscoveredHosts []MistDiscoveredHost `json:"discoveredHosts,omitempty"`
}

// MistDiscovery is the body POST ...?action=discover returns: ONLY the APs the
// collector enumerated (measured live 2026-09-17: {"hosts":[{name,model,
// displayName}]}). It is not a MistSetup; the setup's own testResult is
// updated server-side and read back with ListMist.
type MistDiscovery struct {
	Hosts []MistDiscoveredHost `json:"hosts"`
}

// MistDiscoveredHost is one AP as discovery reports it; Name is the MAC.
type MistDiscoveredHost struct {
	Name        string `json:"name"`
	Model       string `json:"model,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// DiscoverTimeout is the HTTP timeout Discover and Test need: Forward answers
// these with a DeferredResult that blocks until the collector task finishes,
// with a 60-second server-side ceiling (CloudManagedConnectivityService
// DEFAULT_CONNECTIVITY_TIMEOUT). A client timeout below that reads a success
// as a failure.
const DiscoverTimeout = 90 * time.Second

func (s *CloudManagedSetupsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/cloud-managed-setups", nil
}

// ListMist returns the network's Mist setups.
func (s *CloudManagedSetupsService) ListMist(ctx context.Context, networkID string) ([]MistSetup, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[MistSetup]{Keys: []string{"setups", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?type=MIST", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.ListMist")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// CreateMist adds a Mist setup. Idempotency is the caller's: Forward refuses a
// duplicate name, so List first.
func (s *CloudManagedSetupsService) CreateMist(ctx context.Context, networkID string, input NewMistSetup) (*MistSetup, *Response, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, errors.New("forward: mist setup name is required")
	}
	if strings.TrimSpace(string(input.Region)) == "" {
		return nil, nil, errors.New("forward: mist setup region is required (e.g. GLOBAL_01)")
	}
	if strings.TrimSpace(input.APIKeyID) == "" {
		return nil, nil, errors.New("forward: mist setup apiKeyId is required (an API_KEY http credential)")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?type=MIST", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.CreateMist")
	out := new(MistSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// DiscoverMist asks the bound collector to enumerate the org's APs. Requires
// an ONLINE collector (400 offline, 404 unbound) and blocks up to a minute;
// pass a ctx with at least DiscoverTimeout. Best-effort for collection --
// an undiscovered setup with an empty Hosts filter still collects every AP.
func (s *CloudManagedSetupsService) DiscoverMist(ctx context.Context, networkID, setupName string) (*MistDiscovery, *Response, error) {
	if strings.TrimSpace(setupName) == "" {
		return nil, nil, errors.New("forward: mist setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/"+url.PathEscape(setupName)+"?action=discover&type=MIST", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.DiscoverMist")
	out := new(MistDiscovery)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// DeleteMist removes a setup by name. A 404 is success: the desired state,
// that the setup is absent, already holds.
func (s *CloudManagedSetupsService) DeleteMist(ctx context.Context, networkID, setupName string) (*Response, error) {
	if strings.TrimSpace(setupName) == "" {
		return nil, errors.New("forward: mist setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(setupName)+"?type=MIST", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CloudManagedSetups.DeleteMist")
	resp, err := s.client.Do(req, nil)
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return resp, nil
	}
	return resp, err
}
