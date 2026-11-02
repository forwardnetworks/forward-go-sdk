package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// OrganizationsService manages the current organization and the unpublished
// support/admin organization surface.
//
// Preview: /api/admin/orgs is permission-sensitive and version-dependent.
type OrganizationsService service

// Organization is a Forward tenant. Raw retains version-specific response
// fields returned by administrative builds.
type Organization struct {
	ID            Identifier `json:"id"`
	Name          string     `json:"name"`
	Disabled      bool       `json:"disabled,omitempty"`
	SFDCAccountID string     `json:"sfdcAccountId,omitempty"`
	SFDCContactID string     `json:"sfdcContactId,omitempty"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
}

func (o *Organization) UnmarshalJSON(data []byte) error {
	type plain Organization
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*o = Organization(value)
	o.Raw = raw
	return nil
}

// OrganizationCreateRequest contains known administrative query fields.
// ExtraQuery carries fields introduced by a particular Forward version.
type OrganizationCreateRequest struct {
	Name          string
	Type          string
	OnPrem        *bool
	SFDCAccountID string
	SFDCContactID string
	ExtraQuery    url.Values
}

func (s *OrganizationsService) Current(ctx context.Context) (*Organization, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/orgs/current", nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// List returns organizations visible to a Forward support principal.
func (s *OrganizationsService) List(ctx context.Context) ([]Organization, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/orgs", nil)
	if err != nil {
		return nil, nil, err
	}
	var orgs []Organization
	resp, err := s.client.Do(req, &orgs)
	return orgs, resp, err
}

// Create creates an organization using the support-admin API.
func (s *OrganizationsService) Create(ctx context.Context, input OrganizationCreateRequest) (*Organization, *Response, error) {
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
	org := new(Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// Update renames an organization. Current updates the authenticated tenant;
// an explicit ID uses the Forward-admin route.
func (s *OrganizationsService) Update(ctx context.Context, orgID, name string) (*Response, error) {
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
func (s *OrganizationsService) SetEnabled(ctx context.Context, orgID string, enabled bool) (*Response, error) {
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
func (s *OrganizationsService) Delete(ctx context.Context, orgID string) (*Organization, *Response, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	path := "/api/admin/orgs/" + url.PathEscape(orgID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(Organization)
	resp, err := s.client.Do(markOperation(req, "Organizations.Delete"), org)
	return org, resp, err
}

// DeleteNetworkInOrg deletes one network of ANY org the login supports (DELETE /api/orgs/{orgId}/networks/{networkId};
// NetworkController.deleteNetworkForForwardSupport, @RequiresForwardSupport). Forward deletes the network's snapshots first and then the
// network, the same clean path as Networks.Delete, and answers the network as it was. Unlike Networks.Delete it needs no permission in
// the network's own org, so no impersonation. The NQE tutorial network is refused (400). It cannot be undone. Preview: not in the published
// spec. Forward deletes the snapshots synchronously, so a large network can outlast a client or proxy timeout (a 504 says nothing about
// whether it finished; read the network again).
func (s *OrganizationsService) DeleteNetworkInOrg(ctx context.Context, orgID, networkID string) (*Network, *Response, error) {
	orgID, networkID = strings.TrimSpace(orgID), strings.TrimSpace(networkID)
	if orgID == "" || networkID == "" {
		return nil, nil, errors.New("forward: organization ID and network ID are required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/orgs/"+url.PathEscape(orgID)+"/networks/"+url.PathEscape(networkID), nil)
	if err != nil {
		return nil, nil, err
	}
	network := new(Network)
	resp, err := s.client.Do(markOperation(req, "Organizations.DeleteNetworkInOrg"), network)
	return network, resp, err
}

// DeleteOrgNetworks deletes EVERY network of an org, snapshots first, the clean way (DELETE /api/orgs/{orgId}/networks;
// NetworkController.deleteOrgNetworks, internal EDIT_ORG). Forward refuses (400) unless the org is already disabled (SetEnabled false) and
// refuses the NQE tutorial org. It cannot be undone, and one failing network stops it part way with the earlier ones already gone;
// DeleteOrgCleanly reports each network instead. Preview: not in the published spec. Only the system ADMIN role holds EDIT_ORG.
func (s *OrganizationsService) DeleteOrgNetworks(ctx context.Context, orgID string) (*Response, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/orgs/"+url.PathEscape(orgID)+"/networks", nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Organizations.DeleteOrgNetworks"), nil)
}

// DeleteOrgOptions tunes DeleteOrgCleanly.
type DeleteOrgOptions struct {
	// Networks are the ids of the org's networks to delete first, one by one, so each failure is reported by id. When nil they are listed
	// with Admin.ListNetworks (GET /api/admin/networks), which needs a client built with AuthModeService. Whatever this misses is swept by
	// DeleteOrgNetworks before the org is deleted, so a short list cannot orphan a network.
	Networks []string
	// SkipUserCheck skips the check that the org has no users. With an AuthModeService client DeleteOrgCleanly lists users first and refuses
	// before disabling anything, because Forward will not delete an org that still has users. Other clients cannot list users, so the
	// check is skipped for them: delete the users first, or the call ends with the org disabled and its networks gone.
	SkipUserCheck bool
	// KeepOrg leaves the org disabled and not deleted, after its networks are deleted.
	KeepOrg bool
}

// DeleteOrgResult says what DeleteOrgCleanly did. Failed holds one error per network that could not be deleted; when it is not empty
// the org was NOT deleted.
type DeleteOrgResult struct {
	Disabled bool
	Deleted  []string
	Failed   map[string]error
	Org      *Organization
}

// DeleteOrgCleanly deletes an organization without orphaning its network data: it disables the org, deletes each listed network through
// DeleteNetworkInOrg (snapshots first, inside Forward), sweeps any network left with DeleteOrgNetworks, and only if every network was
// deleted deletes the org itself. A network that is already gone counts as deleted (deleting a parent network also deletes its
// workspaces). A network that fails does not stop the others, but the org is then NOT deleted: it stays disabled and the failures are
// returned in the result (the error is non-nil too). Permissions: the system ADMIN role (internal EDIT_ORG) for disabling, sweeping and
// deleting the org; a support login can delete networks (DeleteNetworkInOrg) but not run this. It cannot be undone. Disabling the org
// also disables its users, and enabling the org again does NOT re-enable them, so after a failure re-enable the users too.
// Preview: not in the published spec.
func (s *OrganizationsService) DeleteOrgCleanly(ctx context.Context, orgID string, opts DeleteOrgOptions) (*DeleteOrgResult, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	networks := opts.Networks
	if networks == nil {
		listed, _, err := s.client.Admin.ListNetworks(ctx)
		if err != nil {
			return nil, fmt.Errorf("forward: list the org's networks (or pass DeleteOrgOptions.Networks): %w", err)
		}
		networks = []string{}
		for _, network := range listed {
			if network.OrgID.String() == orgID {
				networks = append(networks, network.ID.String())
			}
		}
	}
	result := &DeleteOrgResult{Failed: map[string]error{}}
	if !opts.SkipUserCheck && s.client.authMode == AuthModeService {
		users, _, err := s.client.Admin.ListUsers(ctx)
		if err != nil {
			return result, fmt.Errorf("forward: check that organization %s has no users (or set SkipUserCheck): %w", orgID, err)
		}
		for _, user := range users {
			if user.OrgID.String() == orgID {
				return result, fmt.Errorf("forward: organization %s still has users, which Forward will not delete the org with: delete them first (nothing was changed)", orgID)
			}
		}
	}
	if _, err := s.SetEnabled(ctx, orgID, false); err != nil {
		return result, fmt.Errorf("forward: disable organization %s: %w", orgID, err)
	}
	result.Disabled = true
	for _, networkID := range networks {
		if _, _, err := s.DeleteNetworkInOrg(ctx, orgID, networkID); err != nil && !isGone(err) {
			result.Failed[networkID] = err
			continue
		}
		result.Deleted = append(result.Deleted, networkID)
	}
	if len(result.Failed) > 0 {
		return result, fmt.Errorf("forward: %d network(s) of organization %s could not be deleted, so the organization was not deleted", len(result.Failed), orgID)
	}
	if opts.KeepOrg {
		return result, nil
	}
	// Forward deletes every network the org still has, by the same clean path, so one missing from the list cannot be orphaned.
	if _, err := s.DeleteOrgNetworks(ctx, orgID); err != nil {
		return result, fmt.Errorf("forward: sweep the remaining networks of organization %s: %w", orgID, err)
	}
	org, _, err := s.Delete(ctx, orgID)
	if err != nil {
		return result, fmt.Errorf("forward: delete organization %s: %w", orgID, err)
	}
	result.Org = org
	return result, nil
}
