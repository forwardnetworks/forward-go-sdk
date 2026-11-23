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
type SaseSetups service

func (s *SaseSetups) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/sase-setups", nil
}

func (s *SaseSetups) named(networkID, name string) (string, error) {
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
func (s *SaseSetups) List(ctx context.Context, networkID string) ([]ports.ZscalerSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.ZscalerSetup]{Keys: []string{"setups", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.List")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns one setup; a missing name is a 404.
func (s *SaseSetups) Get(ctx context.Context, networkID, name string) (*ports.ZscalerSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.Get")
	out := new(ports.ZscalerSetup)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// NewlyDiscovered lists connectors the ZPA API reported that are not in any
// setup yet (GET ...?view=newlyDiscovered). Each connector's setup name says
// which setup's tenant reported it.
func (s *SaseSetups) NewlyDiscovered(ctx context.Context, networkID string) ([]ports.NewlyDiscoveredConnector, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.NewlyDiscoveredConnector]{Keys: []string{"discoveredAppConnectors", "data", "items", "results"}}
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
func (s *SaseSetups) Create(ctx context.Context, networkID string, setup ports.ZscalerSetup) (*ports.ZscalerSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
		if c.CollectionType != ports.AppConnectorCollectionSSH && c.CollectionType != ports.AppConnectorCollectionManual {
			return nil, nil, errors.New("forward: app connector " + c.ConnectorID + " collectionType must be SSH or MANUAL")
		}
	}
	setup.Type = ports.SaseSetupTypeZscaler
	if setup.AppConnectorSetups == nil {
		setup.AppConnectorSetups = []ports.AppConnectorSetup{}
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
	out := new(ports.ZscalerSetup)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Patch changes a setup and returns it. Renaming (a patch Name differing from
// name) replaces the setup under the new name.
func (s *SaseSetups) Patch(ctx context.Context, networkID, name string, patch ports.ZscalerSetupPatch) (*ports.ZscalerSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	body := struct {
		Type string `json:"type"`
		ports.ZscalerSetupPatch
	}{ports.SaseSetupTypeZscaler, patch}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SaseSetups.Patch")
	out := new(ports.ZscalerSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// Delete removes a setup. A 404 for the setup is success: the desired state,
// that it is absent, already holds. A 404 because this deployment does not
// serve the route (ErrEndpointNotServed) is still an error.
func (s *SaseSetups) Delete(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

func (s *SaseSetups) postTest(ctx context.Context, op, path string, body any) (*ports.Response, error) {
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
func (s *SaseSetups) Test(ctx context.Context, networkID string, input ports.ZscalerConnectivityTest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *SaseSetups) TestSetup(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestSetup", path+"?action=test", nil)
}

// TestConnectors queues a test of every SSH App Connector of a saved setup
// (204); like TestSetup it returns first and each outcome lands on its
// connector's TestResult later.
func (s *SaseSetups) TestConnectors(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestConnectors", path+"/connectors?action=test", nil)
}

// TestConnector queues a test of one App Connector (204), asynchronously. Forward answers 404 for an
// unknown connectorId and 400 when the connector is not SSH.
func (s *SaseSetups) TestConnector(ctx context.Context, networkID, name, connectorID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(connectorID) == "" {
		return nil, errors.New("forward: app connector id is required")
	}
	path, err := s.named(networkID, name)
	if err != nil {
		return nil, err
	}
	return s.postTest(ctx, "SaseSetups.TestConnector", path+"/connectors/"+url.PathEscape(connectorID)+"?action=test", nil)
}
