package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// NetworkRole is a role a user holds on one network. The values are Forward's
// NetworkRole enum; only NetworkRoleAdmin carries UPLOAD_SNAPSHOT, and
// NetworkRoleOperator carries MANAGE_CHANGE_SETS but not upload.
type NetworkRole string

const (
	NetworkRoleLimitedReadOnly   NetworkRole = "LIMITED_READ_ONLY"
	NetworkRoleReadOnly          NetworkRole = "READ_ONLY"
	NetworkRoleOperator          NetworkRole = "OPERATOR"
	NetworkRoleWorkspaceOperator NetworkRole = "WORKSPACE_OPERATOR"
	NetworkRoleAdmin             NetworkRole = "ADMIN"
)

func (r NetworkRole) valid() bool {
	switch r {
	case NetworkRoleLimitedReadOnly, NetworkRoleReadOnly, NetworkRoleOperator, NetworkRoleWorkspaceOperator, NetworkRoleAdmin:
		return true
	}
	return false
}

// UserCreateRequest is the body of POST /api/users. Username defaults to
// Email on the server when empty.
type UserCreateRequest struct {
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled,omitempty"`
	// IsSupport marks a support account (NewUser.isSupport).
	IsSupport *bool `json:"isSupport,omitempty"`
}

// Create is POST /api/users: an org admin (MANAGE_USER_ACCOUNTS) creating a
// LOCAL user in its own org. It returns the created user, including its ID.
// Admin.CreateOrganizationUser is a different route (/api/orgs/{orgId}/users)
// that Forward reserves for support admins and that returns no user.
func (s *UsersService) Create(ctx context.Context, input UserCreateRequest) (*User, *Response, error) {
	input.Email, input.Username = strings.TrimSpace(input.Email), strings.TrimSpace(input.Username)
	if input.Email == "" || input.Password == "" {
		return nil, nil, errors.New("forward: email and password are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/users", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Create")
	out := new(User)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: user creation returned no ID")
	}
	return out, response, err
}

// List is GET /api/users: every user in the caller's org (VIEW_USER_ACCOUNTS).
func (s *UsersService) List(ctx context.Context) ([]User, *Response, error) {
	result := listResponse[User]{Keys: []string{"users"}}
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
func (s *UsersService) SetNetworkRole(ctx context.Context, userID, networkID string, role NetworkRole) (*Response, error) {
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.valid() {
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
func (s *UsersService) AddNetworkRole(ctx context.Context, userID, networkID string, role NetworkRole) (*Response, error) {
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.valid() {
		return nil, fmt.Errorf("forward: invalid network role %q", role)
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/"+url.PathEscape(string(role)), nil)
	if err != nil {
		return nil, err
	}
	return s.expectNoContent(markOperation(req, "Users.AddNetworkRole"))
}

// RemoveNetworkRole is DELETE /api/users/{id}/roles/network/{networkId}/{role}.
func (s *UsersService) RemoveNetworkRole(ctx context.Context, userID, networkID string, role NetworkRole) (*Response, error) {
	path, err := userNetworkRolePath(userID, networkID)
	if err != nil {
		return nil, err
	}
	if !role.valid() {
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
func (s *UsersService) ClearNetworkRoles(ctx context.Context, userID, networkID string) (*Response, error) {
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
func (s *UsersService) ListTokensFor(ctx context.Context, userID string) ([]UserToken, *Response, error) {
	path, err := adminUserPath(userID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[UserToken]{Keys: []string{"tokens"}}
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
func (s *UsersService) DeleteTokenFor(ctx context.Context, userID, tokenName string) (*Response, error) {
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
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

func (s *UsersService) expectNoContent(req *http.Request) (*Response, error) {
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
func (s *UsersService) Get(ctx context.Context, userID string) (*User, *Response, error) {
	path, err := userPath(userID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Get")
	out := new(User)
	response, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, response, nil
	}
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// UserWithRoles is a user with its access summary (UserAccountWithRoles, GET /api/users?view=roles). AccessibleNetworks lists each
// network the user can reach and its role there; the API-token times are omitted when the user has no viable token.
type UserWithRoles struct {
	User
	OrgAdmin                bool                `json:"orgAdmin"`
	ForwardAdmin            bool                `json:"fnAdmin,omitempty"`
	SupportAdmin            bool                `json:"supportAdmin,omitempty"`
	AccessibleNetworks      []AccessibleNetwork `json:"accessibleNetworks,omitempty"`
	SupportedOrgIDs         []string            `json:"supportedOrgIds,omitempty"`
	GroupIDs                []string            `json:"groupIds,omitempty"`
	OldestAPITokenCreatedAt string              `json:"oldestApiTokenCreatedAt,omitempty"`
	APITokenLastUsedAt      string              `json:"apiTokenLastUsedAt,omitempty"`
}

// AccessibleNetwork is one network a user can reach, with the role it holds there.
type AccessibleNetwork struct {
	ID   Identifier `json:"id"`
	Name string     `json:"name"`
	Role string     `json:"role"`
}

// ListWithRoles returns the org's users with their roles, reachable networks, groups and API-token use. GET /api/users?view=roles
// (VIEW_USER_ACCOUNTS; UserController, on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
func (s *UsersService) ListWithRoles(ctx context.Context) ([]UserWithRoles, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users?view=roles", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.ListWithRoles")
	var out []UserWithRoles
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// TwoFactorStatus is one user's two-factor setup.
type TwoFactorStatus struct {
	SetUp             bool `json:"setUp"`
	AnyTrustedDevices bool `json:"anyTrustedDevices"`
}

// TwoFactorStatuses returns each of the org's users' two-factor setup, by user ID. GET /api/users?view=2fa (VIEW_USER_ACCOUNTS).
// Preview: not in the published spec.
func (s *UsersService) TwoFactorStatuses(ctx context.Context) (map[string]TwoFactorStatus, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users?view=2fa", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.TwoFactorStatuses")
	out := map[string]TwoFactorStatus{}
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// RolesDirect returns only the roles assigned to the user directly, without access-control-group grants. Roles returns the merged
// view. GET /api/users/{userId}/roles?type=directlyAssigned.
func (s *UsersService) RolesDirect(ctx context.Context, userID string) (*UserRoles, *Response, error) {
	path, err := userPath(userID, "/roles")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?type=directlyAssigned", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.RolesDirect")
	out := new(UserRoles)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// UserPatch changes some of a user; nil fields are left alone (UserPatch on the server).
type UserPatch struct {
	Email     *string `json:"email,omitempty"`
	Username  *string `json:"username,omitempty"`
	Password  *string `json:"password,omitempty"`
	IsSupport *bool   `json:"isSupport,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
}

// Patch changes a user of the caller's org and returns it. PATCH /api/users/{userId} (published updateUser; MANAGE_USER_ACCOUNTS).
// Forward refuses changing your own password this way, disabling your own account, a chatbot account, and a Forward admin unless you
// are one. Admin.PatchUser is the same route for a service principal.
func (s *UsersService) Patch(ctx context.Context, userID string, patch UserPatch) (*User, *Response, error) {
	path, err := userPath(userID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Patch")
	out := new(User)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// BulkSetEnabled enables or disables several users at once. PATCH /api/users {userIds, enabled} (MANAGE_USER_ACCOUNTS). Forward refuses
// the whole request if it includes your own account, a chatbot, or a Forward admin you may not change, and skips users already in the
// requested state. Disabling is how access is cut off, so treat it as destructive.
func (s *UsersService) BulkSetEnabled(ctx context.Context, userIDs []string, enabled bool) (*Response, error) {
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
func (s *UsersService) Reset2FA(ctx context.Context, userID string) (*Response, error) {
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
func (s *UsersService) GrantOrgAdmin(ctx context.Context, userID string) (*Response, error) {
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
	if isStatus(err, http.StatusConflict) {
		return response, nil
	}
	return response, err
}

// RevokeOrgAdmin takes org ADMIN away from a user. DELETE /api/users/{userId}/roles/org/ADMIN (MANAGE_USER_ACCOUNTS). Forward refuses
// revoking your own org admin (a LOCAL user) unless you are a Forward admin, and a chatbot account. Undo with GrantOrgAdmin.
func (s *UsersService) RevokeOrgAdmin(ctx context.Context, userID string) (*Response, error) {
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
func (s *UsersService) Delete(ctx context.Context, userID string) (*Response, error) {
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
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}
