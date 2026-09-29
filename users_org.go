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
