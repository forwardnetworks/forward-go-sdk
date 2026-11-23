package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// OrganizationsService manages the current organization and the unpublished
// support/admin organization surface.
//
// Preview: /api/admin/orgs is permission-sensitive and version-dependent.
type Organizations service

func (s *Organizations) Current(ctx context.Context) (*ports.Organization, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/orgs/current", nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(ports.Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// List returns organizations visible to a Forward support principal.
func (s *Organizations) List(ctx context.Context) ([]ports.Organization, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/orgs", nil)
	if err != nil {
		return nil, nil, err
	}
	var orgs []ports.Organization
	resp, err := s.client.Do(req, &orgs)
	return orgs, resp, err
}

// Create creates an organization using the support-admin API.
func (s *Organizations) Create(ctx context.Context, input ports.OrganizationCreateRequest) (*ports.Organization, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if input.Name = strings.TrimSpace(input.Name); input.Name == "" {
		return nil, nil, errors.New("forward: organization name is required")
	}
	query := cloneValues(input.ExtraQuery)
	query.Set("name", input.Name)
	setString(query, "type", input.Type)
	setBool(query, "onPrem", input.OnPrem)
	setString(query, "sfdcAccountId", input.SFDCAccountID)
	setString(query, "sfdcContactId", input.SFDCContactID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/admin/orgs?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(ports.Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// Update renames an organization. Current updates the authenticated tenant;
// an explicit ID uses the Forward-admin route.
func (s *Organizations) Update(ctx context.Context, orgID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, errors.New("forward: organization name is required")
	}
	path := "/api/orgs/current"
	if orgID = strings.TrimSpace(orgID); orgID != "" {
		path = "/api/admin/orgs/" + url.PathEscape(orgID)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// SetEnabled enables or disables an organization.
func (s *Organizations) SetEnabled(ctx context.Context, orgID string, enabled bool) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	path := "/api/admin/orgs/" + url.PathEscape(orgID) + "?" + url.Values{"action": []string{action}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// Delete deletes an organization (DELETE /api/admin/orgs/{orgId}).
//
// READ THIS BEFORE CALLING IT. Forward removes the org's networks with a database cascade, NOT through its network deletion path: the
// networks' snapshot rows are deleted by SQL but SnapshotService.deleteAll does not run and the network deletion listeners do not, so the jobs-v2 FDB data stored under the networks
// (fdb_blob_list rows under n/<org>/<network>/ and their large objects) is never reclaimed, and the garbage collector only walks networks
// that still exist (FWD-20216, NetworkDeletionListener). Use DeleteOrgCleanly, which deletes each network properly first, unless the org
// has no networks. Forward also answers 400 when the org still has users, disabled ones included. Needs the internal EDIT_ORG permission,
// which only the system ADMIN role holds.
func (s *Organizations) Delete(ctx context.Context, orgID string) (*ports.Organization, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	path := "/api/admin/orgs/" + url.PathEscape(orgID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(ports.Organization)
	resp, err := s.client.Do(markOperation(req, "Organizations.Delete"), org)
	return org, resp, err
}

// DeleteNetworkInOrg deletes one network of ANY org the login supports (DELETE /api/orgs/{orgId}/networks/{networkId};
// NetworkController.deleteNetworkForForwardSupport, @RequiresForwardSupport). Forward deletes the network's snapshots first and then the
// network, the same clean path as Networks.Delete, and answers the network as it was. Unlike Networks.Delete it needs no permission in
// the network's own org, so no impersonation. The NQE tutorial network is refused (400). It cannot be undone. Preview: not in the published
// spec. Forward deletes the snapshots synchronously, so a large network can outlast a client or proxy timeout (a 504 says nothing about
// whether it finished; read the network again).
func (s *Organizations) DeleteNetworkInOrg(ctx context.Context, orgID, networkID string) (*ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	orgID, networkID = strings.TrimSpace(orgID), strings.TrimSpace(networkID)
	if orgID == "" || networkID == "" {
		return nil, nil, errors.New("forward: organization ID and network ID are required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/orgs/"+url.PathEscape(orgID)+"/networks/"+url.PathEscape(networkID), nil)
	if err != nil {
		return nil, nil, err
	}
	network := new(ports.Network)
	resp, err := s.client.Do(markOperation(req, "Organizations.DeleteNetworkInOrg"), network)
	return network, resp, err
}

// DeleteOrgNetworks deletes EVERY network of an org, snapshots first, the clean way (DELETE /api/orgs/{orgId}/networks;
// NetworkController.deleteOrgNetworks, internal EDIT_ORG). Forward refuses (400) unless the org is already disabled (SetEnabled false) and
// refuses the NQE tutorial org. It cannot be undone, and one failing network stops it part way with the earlier ones already gone;
// DeleteOrgCleanly reports each network instead. Preview: not in the published spec. Only the system ADMIN role holds EDIT_ORG.
func (s *Organizations) DeleteOrgNetworks(ctx context.Context, orgID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/orgs/"+url.PathEscape(orgID)+"/networks", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Organizations.DeleteOrgNetworks"), nil)
}
