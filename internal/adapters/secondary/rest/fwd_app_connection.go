package rest

import (
	"context"
	"encoding/json"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// FwdAppConnectionService tests and reads an ON-PREMISES deployment's connection to fwd.app (Software Central), which the CVE index
// updater and software downloads use. FwdAppConnectionController, ProfileCriteria ON_PREM: a SaaS deployment does not map these routes, so
// the answer there is the 404 ErrEndpointNotServed. Preview: not in the published OpenAPI; the shapes are from Forward's source and were not
// run live by this SDK.
type FwdAppConnection service

// Test checks that this deployment can reach Software Central through the organization's configured proxy, with license-based authentication
// (POST /api/fwd-app-connection?action=test; MANAGE_FWD_CONNECTION_CONFIG). Forward answers by making an outbound HEAD /api/cve-index with
// the org's license id and deployment id to the org's SOFTWARE_CENTRAL_URL: it contacts an external service, so it is a write-class call (an
// outbound request), though it changes no stored setting. Status is SUCCESS, AUTH_FAILED (the license or deployment was refused) or
// CONNECTION_FAILED (the host or proxy could not be reached).
func (s *FwdAppConnection) Test(ctx context.Context) (*ports.FwdAppConnectionTest, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/fwd-app-connection?action=test", nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.FwdAppConnectionTest)
	resp, err := s.client.Do(markOperation(req, "FwdAppConnection.Test"), out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Proxy reads the organization's fwd.app proxy settings (GET /api/fwd-app-proxy; VIEW_FWD_CONNECTION_CONFIG). Read-only. A deployment that
// never configured one answers Forward's default (Enabled false).
func (s *FwdAppConnection) Proxy(ctx context.Context) (*ports.FwdAppProxy, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/fwd-app-proxy", nil)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.Do(markOperation(req, "FwdAppConnection.Proxy"), &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(ports.FwdAppProxy)
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, resp, err
		}
	}
	out.Raw = append(json.RawMessage(nil), raw...)
	return out, resp, nil
}

// UpdateFromSoftwareCentral asks the on-premises deployment to update its CVE index itself: it downloads from fwd.app with the org's active
// license id and deployment fingerprint and installs the index if it is newer than the one installed (POST /api/cve-index?action=update;
// MANAGE_CVE_INDEX; CveIndexUpdateService.updateCveIndex for the caller's org). It contacts an external service and can install an index, so
// it is a write-class call; it is not destructive (an older installed index is replaced only by a newer one). ON_PREM profile only. Check
// the result's Error: a failed update is not an HTTP error. Put and Download are the manual path for a deployment that cannot reach fwd.app.
func (s *CVEIndex) UpdateFromSoftwareCentral(ctx context.Context) (*ports.CVEIndexUpdate, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/cve-index?action=update", nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.CVEIndexUpdate)
	resp, err := s.client.Do(markOperation(req, "CVEIndex.UpdateFromSoftwareCentral"), out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
