package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// SaseSetupsService manages SASE collection sources: today only Zscaler
// (type=ZSCALER), which collects the ZPA tenant through its API and, per App
// Connector, over SSH. A setup is the tenant plus its connectors; Forward
// names the connectors' devices after them.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/SaseSetupController.java (identical route set at both
// pinned builds), cv/sources/sase/ZscalerSetup.java, AppConnectorSetup.java,
// ZscalerSetupPatch.java, ZscalerTestResult.java and ZscalerConnectivityTestRequest.java.
// Unverified live: nothing here has been run against a tenant.
type SaseSetupsService service

// SaseSetupTypeZscaler is the only setup type Forward has.
const SaseSetupTypeZscaler = "ZSCALER"

// AppConnectorCollectionType says how Forward collects an App Connector.
const (
	// AppConnectorCollectionSSH collects the connector's CLI over SSH; Port
	// and CredentialID are then expected.
	AppConnectorCollectionSSH = "SSH"
	// AppConnectorCollectionManual records the connector without probing it
	// (assumed always reachable); Forward drops Port for it.
	AppConnectorCollectionManual = "MANUAL"
)

// ScimConfig is a SCIM endpoint whose users and groups Forward reads. The
// credential must be an API_KEY HTTP credential (Forward refuses others when
// it validates the body).
type ScimConfig struct {
	// URL is absolute http(s), shaped .../scim/<cloudId>/<idpId>/v<N>.
	URL string `json:"url"`
	// HTTPCredID is the id of an API_KEY HTTP credential.
	HTTPCredID string `json:"httpCredId"`
}

// ConnectivityResult is the outcome of the latest test of a setup or one App
// Connector. Error is Forward's ConnectivityTestError constant name, for
// example AUTHENTICATION_FAILED or PING_FAILED, and empty on success. It is
// an enum name, not a message; it is kept verbatim.
type ConnectivityResult struct {
	// SavedAt is when the result was stored (RFC 3339).
	SavedAt string `json:"savedAt,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DiscoveredAppConnector is a connector the ZPA API reported. Latitude and
// Longitude are both set or both absent; LocationName is ZPA's own.
type DiscoveredAppConnector struct {
	ConnectorID  string   `json:"connectorId"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	LocationName string   `json:"locationName,omitempty"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
}

// ZscalerTestResult is the latest tenant-level test. DiscoveredConnectors is
// only reported when the test succeeded and found any.
type ZscalerTestResult struct {
	ConnectivityResult
	DiscoveredConnectors []DiscoveredAppConnector `json:"discoveredConnectors,omitempty"`
}

// AppConnectorSetup is one App Connector inside a setup.
type AppConnectorSetup struct {
	// ConnectorID is ZPA's id for the connector. Required.
	ConnectorID string `json:"connectorId"`
	// Name becomes the device name; Forward lower-cases it. Required.
	Name string `json:"name"`
	// Host is a hostname or IP. Required.
	Host string `json:"host"`
	// CollectionType is AppConnectorCollectionSSH or AppConnectorCollectionManual. Required.
	CollectionType string `json:"collectionType"`
	// Port is 1-65535 for SSH; Forward discards it for MANUAL.
	Port *int `json:"port,omitempty"`
	// CredentialID is a CLI credential id (SSH).
	CredentialID string `json:"credentialId,omitempty"`
	JumpServerID string `json:"jumpServerId,omitempty"`
	LocationID   string `json:"locationId,omitempty"`
	// Collect: nil reads as true on the server; false parks the connector.
	Collect *bool `json:"collect,omitempty"`
	// TestResult is read-only on Create (cleared). Patch replaces the whole
	// connector list and stores each TestResult as sent, so round-trip the
	// connectors from Get when patching or the saved results are lost.
	TestResult *ConnectivityResult `json:"testResult,omitempty"`
}

// ZscalerSetup is a Zscaler SASE setup, as sent to Create and as read back.
type ZscalerSetup struct {
	// Type is always SaseSetupTypeZscaler; the SDK fills it on send.
	Type string `json:"type"`
	// Name is lower-cased by Forward. Required.
	Name string `json:"name"`
	// Collect: nil participates in collections; false parks the setup.
	Collect *bool `json:"collect,omitempty"`
	// CollectorID is the preferred collector. Forward writes it as "C<n>" and
	// reads either "C<n>" or "<n>". Empty means the network's default.
	CollectorID Identifier `json:"collectorId,omitempty"`
	// BaseURL is the ZPA API base; absolute http or https; only its host is
	// kept. Required.
	BaseURL string `json:"baseUrl"`
	// HTTPCredID is the id of the HTTP credential used for ZPA signin. Forward's
	// device type for it declares credential type LOGIN (client id as the
	// username, client secret as the password).
	HTTPCredID string `json:"httpCredId,omitempty"`
	// AppConnectorSetups are the connectors; empty is allowed.
	AppConnectorSetups []AppConnectorSetup `json:"appConnectorSetups"`
	// ScimConfigs are the SCIM endpoints; empty is allowed.
	ScimConfigs []ScimConfig `json:"scimConfigs,omitempty"`
	// TestResult and Attribution are read-only; Create clears them.
	TestResult  *ZscalerTestResult `json:"testResult,omitempty"`
	Attribution *SaseAttribution   `json:"attribution,omitempty"`
}

// SaseAttribution is who created or last changed a setup (AttributionWithNames);
// the names are absent when the user was deleted, and every field may be.
type SaseAttribution struct {
	CreatedBy   string `json:"createdBy,omitempty"`
	CreatedByID string `json:"createdById,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedBy   string `json:"updatedBy,omitempty"`
	UpdatedByID string `json:"updatedById,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// ZscalerSetupPatch is the body of PATCH .../sase-setups/{name}. A nil field
// is left alone; the SDK cannot send the JSON null that Forward reads as "clear
// httpCredId / collectorId / collect". Changing Name renames the setup. The slices are replaced
// whole, so send the complete list; a non-nil pointer to an empty slice
// clears it.
type ZscalerSetupPatch struct {
	Name               *string              `json:"name,omitempty"`
	Collect            *bool                `json:"collect,omitempty"`
	CollectorID        *string              `json:"collectorId,omitempty"`
	BaseURL            *string              `json:"baseUrl,omitempty"`
	HTTPCredID         *string              `json:"httpCredId,omitempty"`
	AppConnectorSetups *[]AppConnectorSetup `json:"appConnectorSetups,omitempty"`
	ScimConfigs        *[]ScimConfig        `json:"scimConfigs,omitempty"`
}

// ZscalerConnectivityTest is the body of POST .../sase-setups?action=test: try
// a tenant before saving a setup. The result is not returned; it is saved only
// onto an existing setup.
type ZscalerConnectivityTest struct {
	// ID is the device name the probe runs under. Required.
	ID          string       `json:"id"`
	BaseURL     string       `json:"baseUrl"`
	HTTPCredID  string       `json:"httpCredId,omitempty"`
	ScimConfigs []ScimConfig `json:"scimConfigs,omitempty"`
	CollectorID string       `json:"collectorId,omitempty"`
}

// NewlyDiscoveredConnector is a connector the ZPA API reported that no
// setup's AppConnectorSetups lists yet, with the setup it was found under.
type NewlyDiscoveredConnector struct {
	SetupName string `json:"setupName"`
	DiscoveredAppConnector
}

func (s *SaseSetupsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/sase-setups", nil
}

func (s *SaseSetupsService) named(networkID, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("forward: sase setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(name), nil
}

// List returns the network's setups. The response is {"setups":[...]}.
func (s *SaseSetupsService) List(ctx context.Context, networkID string) ([]ZscalerSetup, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ZscalerSetup]{Keys: []string{"setups", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.List")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns one setup; a missing name is a 404.
func (s *SaseSetupsService) Get(ctx context.Context, networkID, name string) (*ZscalerSetup, *Response, error) {
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.Get")
	out := new(ZscalerSetup)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// NewlyDiscovered lists connectors the ZPA API reported that are not in any
// setup yet (GET ...?view=newlyDiscovered). Each connector's setup name says
// which setup's tenant reported it.
func (s *SaseSetupsService) NewlyDiscovered(ctx context.Context, networkID string) ([]NewlyDiscoveredConnector, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[NewlyDiscoveredConnector]{Keys: []string{"discoveredAppConnectors", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?view=newlyDiscovered", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.NewlyDiscovered")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Create adds a setup and returns it as saved (no test results yet).
// Forward saves the setup FIRST and then queues a tenant test and a test of
// every connector on the collector (ZscalerConnectivityService); the controller
// is not transactional, so when no collector is online or supported that second
// step answers 400 AFTER the setup exists. After any Create error, Get or List
// before retrying: a retry meets the duplicate-name refusal. The tests run
// asynchronously and the call does not wait for them; poll Get until
// TestResult.SavedAt appears. Create is not idempotent.
func (s *SaseSetupsService) Create(ctx context.Context, networkID string, setup ZscalerSetup) (*ZscalerSetup, *Response, error) {
	if strings.TrimSpace(setup.Name) == "" {
		return nil, nil, errors.New("forward: sase setup name is required")
	}
	if strings.TrimSpace(setup.BaseURL) == "" {
		return nil, nil, errors.New("forward: sase setup baseUrl is required")
	}
	for i, c := range setup.AppConnectorSetups {
		if strings.TrimSpace(c.ConnectorID) == "" || strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Host) == "" {
			return nil, nil, fmt.Errorf("forward: app connector %d needs connectorId, name and host", i)
		}
		if c.CollectionType != AppConnectorCollectionSSH && c.CollectionType != AppConnectorCollectionManual {
			return nil, nil, errors.New("forward: app connector " + c.ConnectorID + " collectionType must be SSH or MANUAL")
		}
	}
	setup.Type = SaseSetupTypeZscaler
	if setup.AppConnectorSetups == nil {
		setup.AppConnectorSetups = []AppConnectorSetup{}
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, setup)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.Create")
	out := new(ZscalerSetup)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Patch changes a setup and returns it. Renaming (a patch Name differing from
// name) replaces the setup under the new name.
func (s *SaseSetupsService) Patch(ctx context.Context, networkID, name string, patch ZscalerSetupPatch) (*ZscalerSetup, *Response, error) {
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	body := struct {
		Type string `json:"type"`
		ZscalerSetupPatch
	}{SaseSetupTypeZscaler, patch}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.Patch")
	out := new(ZscalerSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// Delete removes a setup. A 404 for the setup is success: the desired state,
// that it is absent, already holds. A 404 because this deployment does not
// serve the route (ErrEndpointNotServed) is still an error.
func (s *SaseSetupsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "SaseSetups.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

func (s *SaseSetupsService) postTest(ctx context.Context, op, path string, body any) (*Response, error) {
	var (
		req *http.Request
		err error
	)
	if body == nil {
		req, err = s.client.NewRequest(ctx, http.MethodPost, path, nil)
	} else {
		req, err = s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	}
	if err != nil {
		return nil, err
	}
	req = markOperation(req, op)
	return s.client.Do(req, nil)
}

// Test queues a tenant probe before any setup exists (POST ...?action=test,
// 204). The probe runs on the collector afterwards and nothing is stored or
// returned over REST (Forward reports it only as a server-sent event), so a
// 204 says the probe was queued, not that the tenant works. Errors are body
// validation and "no online or supported collector"; to learn whether a tenant
// works, Create the setup and read TestResult.
func (s *SaseSetupsService) Test(ctx context.Context, networkID string, input ZscalerConnectivityTest) (*Response, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.BaseURL) == "" {
		return nil, errors.New("forward: sase connectivity test needs id and baseUrl")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.Test", path+"?action=test", input)
}

// TestSetup queues a re-test of a saved setup's tenant (204). It returns before
// the probe runs; the collector saves the outcome onto the setup later, so poll
// Get until TestResult.SavedAt moves. Needs an online collector (400 otherwise).
func (s *SaseSetupsService) TestSetup(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestSetup", path+"?action=test", nil)
}

// TestConnectors queues a test of every SSH App Connector of a saved setup
// (204); like TestSetup it returns first and each outcome lands on its
// connector's TestResult later.
func (s *SaseSetupsService) TestConnectors(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestConnectors", path+"/connectors?action=test", nil)
}

// TestConnector queues a test of one App Connector (204), asynchronously. Forward answers 404 for an
// unknown connectorId and 400 when the connector is not SSH.
func (s *SaseSetupsService) TestConnector(ctx context.Context, networkID, name, connectorID string) (*Response, error) {
	if strings.TrimSpace(connectorID) == "" {
		return nil, errors.New("forward: app connector id is required")
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestConnector", path+"/connectors/"+url.PathEscape(connectorID)+"?action=test", nil)
}
