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

// Create is POST /api/users: an org admin (MANAGE_USER_ACCOUNTS) creating a
// LOCAL user in its own org. It returns the created user, including its ID.
// Admin.CreateOrganizationUser is a different route (/api/orgs/{orgId}/users)
// that Forward reserves for support admins and that returns no user.
func (s *Users) Create(ctx context.Context, input ports.UserCreateRequest) (*ports.User, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	input.Email, input.Username = strings.TrimSpace(input.Email), strings.TrimSpace(input.Username)
	if input.Email == "" || input.Password == "" {
		return nil, nil, errors.New("forward: email and password are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/users", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Create")
	out := new(ports.User)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: user creation returned no ID")
	}
	return out, response, err
}

// List is GET /api/users: every user in the caller's org (VIEW_USER_ACCOUNTS).
func (s *Users) List(ctx context.Context) ([]ports.User, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.User]{Keys: []string{"users"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// SetNetworkRole is POST /api/users/{id}/roles/network/{networkId}?role=: the
// user ends up holding exactly role on the network (Forward removes the pair's
// existing roles first). Like every role grant it is not a no-op on the wire,
// so a reconcile should read Roles first and only call this on a difference.
func (s *Users) SetNetworkRole(ctx context.Context, userID, networkID string, role ports.NetworkRole) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, fmt.Errorf("forward: invalid network role %q", role)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?"+url.Values{"role": []string{string(role)}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	return s.expectNoContent(markOperation(req, "Users.SetNetworkRole"))
}

// AddNetworkRole is POST /api/users/{id}/roles/network/{networkId}/{role}; it
// keeps any role the user already holds on the network.
func (s *Users) AddNetworkRole(ctx context.Context, userID, networkID string, role ports.NetworkRole) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, fmt.Errorf("forward: invalid network role %q", role)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/"+url.PathEscape(string(role)), nil)
	if err != nil {
		return nil, err
	}
	return s.expectNoContent(markOperation(req, "Users.AddNetworkRole"))
}

// RemoveNetworkRole is DELETE /api/users/{id}/roles/network/{networkId}/{role}.
func (s *Users) RemoveNetworkRole(ctx context.Context, userID, networkID string, role ports.NetworkRole) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, fmt.Errorf("forward: invalid network role %q", role)
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(string(role)), nil)
	if err != nil {
		return nil, err
	}
	return s.expectNoContent(markOperation(req, "Users.RemoveNetworkRole"))
}

// ClearNetworkRoles is DELETE /api/users/{id}/roles/network/{networkId}: the
// user keeps no role on the network.
func (s *Users) ClearNetworkRoles(ctx context.Context, userID, networkID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.expectNoContent(markOperation(req, "Users.ClearNetworkRoles"))
}

// ListTokensFor is GET /api/users/{id}/tokens: another user's API tokens, as an
// org admin sees them (names and access keys; secrets are never returned).
func (s *Users) ListTokensFor(ctx context.Context, userID string) ([]ports.UserToken, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.UserToken]{Keys: []string{"tokens"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/tokens", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.ListTokensFor")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// DeleteTokenFor is DELETE /api/users/{id}/tokens/{tokenName}. A token that is
// already gone counts as deleted.
func (s *Users) DeleteTokenFor(ctx context.Context, userID, tokenName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, err
	}
	tokenName = strings.TrimSpace(tokenName)
	if tokenName == "" {
		return nil, errors.New("forward: token name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/tokens/"+url.PathEscape(tokenName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.DeleteTokenFor")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

func (s *Users) expectNoContent(req *http.Request) (*ports.Response, error) {
	response, err := s.client.Do(req, nil)
	if err == nil && response.StatusCode != http.StatusNoContent {
		err = fmt.Errorf("forward: %s returned HTTP %d, want 204", MetadataFromRequest(req).Operation, response.StatusCode)
	}
	return response, err
}

func userNetworkRolePath(userID, networkID string) (string, error) {
	path, err := adminUserPath(userID)
	if err != nil {
		return "", err
	}
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return "", errors.New("forward: network ID is required")
	}
	return path + "/roles/network/" + url.PathEscape(networkID), nil
}

// Get returns one user of the caller's org, or (nil, nil) when it does not exist. GET /api/users/{userId} (published getUser;
// VIEW_USER_ACCOUNTS). Admin.LookupUser is the Forward-admin route.
func (s *Users) Get(ctx context.Context, userID string) (*ports.User, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := userPath(userID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Get")
	out := new(ports.User)
	response, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// ListWithRoles returns the org's users with their roles, reachable networks, groups and API-token use. GET /api/users?view=roles
// (VIEW_USER_ACCOUNTS; UserController, on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
func (s *Users) ListWithRoles(ctx context.Context) ([]ports.UserWithRoles, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users?view=roles", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.ListWithRoles")
	var out []ports.UserWithRoles
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// TwoFactorStatuses returns each of the org's users' two-factor setup, by user ID. GET /api/users?view=2fa (VIEW_USER_ACCOUNTS).
// Preview: not in the published spec.
func (s *Users) TwoFactorStatuses(ctx context.Context) (map[string]ports.TwoFactorStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users?view=2fa", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.TwoFactorStatuses")
	out := map[string]ports.TwoFactorStatus{}
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// RolesDirect returns only the roles assigned to the user directly, without access-control-group grants. Roles returns the merged
// view. GET /api/users/{userId}/roles?type=directlyAssigned.
func (s *Users) RolesDirect(ctx context.Context, userID string) (*ports.UserRoles, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := userPath(userID, "/roles")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?type=directlyAssigned", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.RolesDirect")
	out := new(ports.UserRoles)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Patch changes a user of the caller's org and returns it. PATCH /api/users/{userId} (published updateUser; MANAGE_USER_ACCOUNTS).
// Forward refuses changing your own password this way, disabling your own account, a chatbot account, and a Forward admin unless you
// are one. Admin.PatchUser is the same route for a service principal.
func (s *Users) Patch(ctx context.Context, userID string, patch ports.UserPatch) (*ports.User, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := userPath(userID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Patch")
	out := new(ports.User)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// BulkSetEnabled enables or disables several users at once. PATCH /api/users {userIds, enabled} (MANAGE_USER_ACCOUNTS). Forward refuses
// the whole request if it includes your own account, a chatbot, or a Forward admin you may not change, and skips users already in the
// requested state. Disabling is how access is cut off, so treat it as destructive.
func (s *Users) BulkSetEnabled(ctx context.Context, userIDs []string, enabled bool) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	userIDs = nonEmptyStrings(userIDs)
	if len(userIDs) == 0 {
		return nil, errors.New("forward: at least one user ID is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/users", map[string]any{"userIds": userIDs, "enabled": enabled})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.BulkSetEnabled")
	return s.client.Do(req, nil)
}

// Reset2FA clears a user's two-factor setup, so the next sign-in enrolls again. DELETE /api/users/{userId}/2fa (MANAGE_USER_ACCOUNTS;
// refused for a chatbot account).
func (s *Users) Reset2FA(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userPath(userID, "/2fa")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.Reset2FA")
	return s.client.Do(req, nil)
}

// GrantOrgAdmin gives a user org ADMIN; already holding it is success. POST /api/users/{userId}/roles/org/ADMIN (MANAGE_USER_ACCOUNTS).
// The undo of RevokeOrgAdmin. Admin.GrantOrganizationAdmin is the same route for a service principal.
func (s *Users) GrantOrgAdmin(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userPath(userID, "/roles/org/ADMIN")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.GrantOrgAdmin")
	response, err := s.client.Do(req, nil)
	if ports.IsStatus(err, http.StatusConflict) {
		return response, nil
	}
	return response, err
}

// RevokeOrgAdmin takes org ADMIN away from a user. DELETE /api/users/{userId}/roles/org/ADMIN (MANAGE_USER_ACCOUNTS). Forward refuses
// revoking your own org admin (a LOCAL user) unless you are a Forward admin, and a chatbot account. Undo with GrantOrgAdmin.
func (s *Users) RevokeOrgAdmin(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userPath(userID, "/roles/org/ADMIN")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.RevokeOrgAdmin")
	return s.client.Do(req, nil)
}

func userPath(userID, tail string) (string, error) {
	if userID = strings.TrimSpace(userID); userID == "" {
		return "", errors.New("forward: user ID is required")
	}
	return "/api/users/" + url.PathEscape(userID) + tail, nil
}

// Delete deletes a user of the caller's org. A 404 is success (already gone). DELETE /api/users/{userId} (published deleteUser;
// MANAGE_USER_ACCOUNTS). Forward refuses deleting yourself (400 "Can't delete yourself."), a chatbot account, and a Forward admin
// unless you are one (403); those come back as the ErrorResponse with Forward's message. Admin.DeleteUser is the same route for a
// service principal.
func (s *Users) Delete(ctx context.Context, userID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := userPath(userID, "")
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.Delete")
	response, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}
