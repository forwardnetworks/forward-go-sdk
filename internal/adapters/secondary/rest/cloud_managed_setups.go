package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudManagedSetups manages CLOUD-managed setups: the record that
// tells Forward a SaaS wireless controller owns a set of access points, which
// Forward then collects through the vendor's cloud API rather than device by
// device. That is Juniper Mist (type=MIST) and Meraki (type=MERAKI); VeloCloud
// (type=VELO_CLOUD) is not covered here.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/CloudManagedSetupController.java (add/discover/test/
// delete, all gated by org property MIST_ONBOARDING, default true) and
// web/.../json/mist/CreateMistSetup.java for the request shape.
type CloudManagedSetups service

func (s *CloudManagedSetups) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/cloud-managed-setups", nil
}

// ListMist returns the network's Mist setups.
func (s *CloudManagedSetups) ListMist(ctx context.Context, networkID string) ([]ports.MistSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.MistSetup]{Keys: []string{"setups", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?type=MIST", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.ListMist")
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return result.Items, resp, err
	}
	// Forward's list route takes no type parameter and answers every cloud-managed setup (the ?type=MIST is ignored), so a
	// Meraki or VeloCloud setup would otherwise come back decoded as a Mist one. Keep the Mist ones; a setup that states no
	// type is kept, as before.
	out := []ports.MistSetup{}
	for _, setup := range result.Items {
		if setup.Type == "" || setup.Type == "MIST" {
			out = append(out, setup)
		}
	}
	return out, resp, nil
}

// CreateMist adds a Mist setup. Idempotency is the caller's: Forward refuses a
// duplicate name, so List first.
func (s *CloudManagedSetups) CreateMist(ctx context.Context, networkID string, input ports.NewMistSetup) (*ports.MistSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.MistSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// DiscoverMist asks the bound collector to enumerate the org's APs. Requires
// an ONLINE collector (400 offline, 404 unbound) and blocks up to a minute;
// pass a ctx with at least DiscoverTimeout. Best-effort for collection --
// an undiscovered setup with an empty Hosts filter still collects every AP.
func (s *CloudManagedSetups) DiscoverMist(ctx context.Context, networkID, setupName string) (*ports.MistDiscovery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.MistDiscovery)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// DeleteMist removes a setup by name. A 404 is success: the desired state,
// that the setup is absent, already holds.
func (s *CloudManagedSetups) DeleteMist(ctx context.Context, networkID, setupName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

// merakiType is how Forward marks a Meraki setup in a listing and in its type= query parameter.
const merakiType = "MERAKI"

// ListMeraki returns the network's Meraki setups. Forward's list route has no type parameter and answers every cloud-managed
// setup, so the listing is filtered by each setup's own type.
func (s *CloudManagedSetups) ListMeraki(ctx context.Context, networkID string) ([]ports.MerakiSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.MerakiSetup]{Keys: []string{"setups", "data", "items", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.ListMeraki")
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	out := []ports.MerakiSetup{}
	for _, setup := range result.Items {
		if setup.Type == merakiType {
			out = append(out, setup)
		}
	}
	return out, resp, nil
}

// GetMeraki returns one Meraki setup; a setup of another type is an error, never decoded as Meraki.
func (s *CloudManagedSetups) GetMeraki(ctx context.Context, networkID, setupName string) (*ports.MerakiSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(setupName) == "" {
		return nil, nil, errors.New("forward: meraki setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/"+url.PathEscape(setupName), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.GetMeraki")
	out := new(ports.MerakiSetup)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	if out.Type != merakiType {
		kind := out.Type
		if kind == "" {
			kind = "none"
		}
		return nil, resp, fmt.Errorf("forward: cloud-managed setup %q has type %s, not MERAKI", setupName, kind)
	}
	return out, resp, nil
}

// CreateMeraki adds a Meraki setup. Forward refuses a duplicate name, so list first. Forward requires a testResult with a
// savedAt; when input.TestResult is nil, the current time is sent with no error.
func (s *CloudManagedSetups) CreateMeraki(ctx context.Context, networkID string, input ports.NewMerakiSetup) (*ports.MerakiSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	if input.TestResult == nil {
		input.TestResult = &ports.MerakiTestResult{SavedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?type="+merakiType, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.CreateMeraki")
	out := new(ports.MerakiSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// PatchMeraki changes part of a Meraki setup (see MerakiSetupPatch for what absent, set and cleared mean).
func (s *CloudManagedSetups) PatchMeraki(ctx context.Context, networkID, setupName string, patch ports.MerakiSetupPatch) (*ports.MerakiSetup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(setupName) == "" {
		return nil, nil, errors.New("forward: meraki setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path+"/"+url.PathEscape(setupName)+"?type="+merakiType, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.PatchMeraki")
	out := new(ports.MerakiSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// TestMeraki tests Meraki connectivity through the collector. Forward answers 204 when the test passes, and an error otherwise.
func (s *CloudManagedSetups) TestMeraki(ctx context.Context, networkID string, test ports.MerakiConnectivityTest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(test.Name) == "" {
		return nil, errors.New("forward: meraki connectivity test needs the setup name")
	}
	if strings.TrimSpace(test.APIKeyID) == "" {
		return nil, errors.New("forward: meraki connectivity test needs apiKeyId")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+"/cloud-managed-setup-tests?type="+merakiType, test)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CloudManagedSetups.TestMeraki")
	return s.client.Do(req, nil)
}

// DiscoverMeraki asks the collector to enumerate the account's devices; it blocks until the collector task ends.
func (s *CloudManagedSetups) DiscoverMeraki(ctx context.Context, networkID, setupName string) (*ports.MerakiDiscovery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(setupName) == "" {
		return nil, nil, errors.New("forward: meraki setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/"+url.PathEscape(setupName)+"?action=discover&type="+merakiType, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudManagedSetups.DiscoverMeraki")
	out := new(ports.MerakiDiscovery)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// DeleteMeraki removes a Meraki setup by name. A 404 for the setup is success: it is already absent (a 404 because the network is
// missing is an error). Forward's delete route takes no type, so it removes the named setup whatever its type; call GetMeraki
// first when the name might belong to another kind.
func (s *CloudManagedSetups) DeleteMeraki(ctx context.Context, networkID, setupName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(setupName) == "" {
		return nil, errors.New("forward: meraki setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(setupName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CloudManagedSetups.DeleteMeraki")
	resp, err := s.client.Do(req, nil)
	// A 404 for the setup is success (it is already absent); a 404 because the network itself is missing is not.
	if ports.IsGone(err) && !errors.Is(err, ports.ErrNetworkNotFound) {
		return resp, nil
	}
	return resp, err
}
