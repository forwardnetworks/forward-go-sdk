package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// DeleteOrgCleanly deletes an organization without orphaning its network data: it disables the org, deletes each listed network through
// DeleteNetworkInOrg (snapshots first, inside Forward), sweeps any network left with DeleteOrgNetworks, and only if every network was
// deleted deletes the org itself. A network that is already gone counts as deleted (deleting a parent network also deletes its
// workspaces). A network that fails does not stop the others, but the org is then NOT deleted: it stays disabled and the failures are
// returned in the result (the error is non-nil too). Permissions: the system ADMIN role (internal EDIT_ORG) for disabling, sweeping and
// deleting the org; a support login can delete networks (DeleteNetworkInOrg) but not run this. It cannot be undone. Disabling the org
// also disables its users, and enabling the org again does NOT re-enable them, so after a failure re-enable the users too.
// Preview: not in the published spec.
func (s *Organizations) DeleteOrgCleanly(ctx context.Context, orgID string, opts domain.DeleteOrgOptions) (*domain.DeleteOrgResult, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	networks := opts.Networks
	if networks == nil {
		listed, _, err := s.admin.ListNetworks(ctx)
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
	result := &domain.DeleteOrgResult{Failed: map[string]error{}}
	if !opts.SkipUserCheck && s.session.AuthMode() == domain.AuthModeService {
		users, _, err := s.admin.ListUsers(ctx)
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
		if _, _, err := s.DeleteNetworkInOrg(ctx, orgID, networkID); err != nil && !ports.IsGone(err) {
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
