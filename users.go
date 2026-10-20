package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type UsersService service

// User is a Forward user account. Forward serializes id, username, email, enabled, lastActive, authSource (LOCAL, SAML, LDAP, TACACS,
// ...), isSupport (only when true) and externalGroups (for an external auth source using access control groups); OrgID and
// MustSetPassword are not sent by current builds and read as empty.
type User struct {
	ID              Identifier `json:"id"`
	OrgID           Identifier `json:"orgId"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	MustSetPassword bool       `json:"mustSetPassword"`
	Enabled         bool       `json:"enabled"`
	LastActive      string     `json:"lastActive,omitempty"`
	AuthSource      string     `json:"authSource,omitempty"`
	IsSupport       bool       `json:"isSupport,omitempty"`
	ExternalGroups  []string   `json:"externalGroups,omitempty"`
}

// UserSession is the caller's own session (GET /api/users/current): the user, the roles in effect for this session, the access control
// groups the user is in, who is impersonating (if anyone), and when the password was last set.
type UserSession struct {
	User          User         `json:"user"`
	Roles         SessionRoles `json:"roles"`
	GroupIDs      []string     `json:"groupIds,omitempty"`
	Impersonator  string       `json:"impersonator,omitempty"`
	PasswordSetAt string       `json:"passwordSetAt,omitempty"`
}

// SessionRoles are the roles in effect for a session: access-control-group grants merged in and workspace inheritance applied, so they can
// exceed what was assigned directly (Users.RolesDirect). Network maps a network ID to its one NetworkRole.
type SessionRoles struct {
	System          []string          `json:"system,omitempty"`
	Org             []string          `json:"org"`
	Network         map[string]string `json:"network"`
	SupportedOrgIDs []string          `json:"supportedOrgIds,omitempty"`
}

// UnmarshalJSON reads the roles tolerantly, as UserRoles does: system and org may be a list, a bare string (one role, as some builds
// and the NetBox plugin saw it) or null, and a network's role may be a string or a one-element list. A missing or empty roles object
// decodes to no roles, which means "Forward did not say", not "this account may do nothing": callers must not read an empty result as a
// refusal.
func (r *SessionRoles) UnmarshalJSON(data []byte) error {
	var raw struct {
		System          json.RawMessage            `json:"system"`
		Org             json.RawMessage            `json:"org"`
		Network         map[string]json.RawMessage `json:"network"`
		SupportedOrgIDs []string                   `json:"supportedOrgIds"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if r.System, err = roleList("system", raw.System); err != nil {
		return err
	}
	if r.Org, err = roleList("org", raw.Org); err != nil {
		return err
	}
	r.SupportedOrgIDs = raw.SupportedOrgIDs
	r.Network, err = singleNetworkRoles(raw.Network)
	return err
}

// singleNetworkRoles decodes a network-to-role map where each role is a string or a one-element list; a list of several roles is an
// error naming the network, not a silent pick.
func singleNetworkRoles(raw map[string]json.RawMessage) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for networkID, value := range raw {
		roles, err := roleList("network "+networkID, value)
		if err != nil {
			return nil, err
		}
		switch len(roles) {
		case 0:
			out[networkID] = ""
		case 1:
			out[networkID] = roles[0]
		default:
			return nil, fmt.Errorf("forward: network %s has %d roles %v where one was expected", networkID, len(roles), roles)
		}
	}
	return out, nil
}

// roleList decodes a roles field that Forward writes as a list but that other builds and plugins have sent as one bare string or as
// null: a list is returned as is, a string as a one-element list (not split into characters), and null or an empty string as nil.
func roleList(field string, raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '"' {
		var role string
		if err := json.Unmarshal(raw, &role); err != nil {
			return nil, fmt.Errorf("forward: %s role: %w", field, err)
		}
		if role = strings.TrimSpace(role); role == "" {
			return nil, nil
		}
		return []string{role}, nil
	}
	var roles []string
	if err := json.Unmarshal(raw, &roles); err != nil {
		return nil, fmt.Errorf("forward: %s roles are neither a list nor a string: %w", field, err)
	}
	for i := range roles {
		roles[i] = strings.TrimSpace(roles[i])
	}
	return roles, nil
}

// HasOrgAdmin reports whether the session holds org ADMIN.
func (r SessionRoles) HasOrgAdmin() bool {
	for _, role := range r.Org {
		if strings.EqualFold(role, "ADMIN") {
			return true
		}
	}
	return false
}

type UserToken struct {
	Name      string `json:"name"`
	AccessKey string `json:"accessKey"`
}

type UserTokenRegistration struct {
	Token  UserToken `json:"token"`
	Secret string    `json:"secret"`
}

type PasswordResetRequest struct {
	CurrentPassword string
	NewPassword     string
}

func (s *UsersService) Current(ctx context.Context) (*User, *Response, error) {
	var envelope struct {
		User User `json:"user"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Current")
	response, err := s.client.doRequired(req, &envelope)
	if err == nil && envelope.User.ID == "" && strings.TrimSpace(envelope.User.Username) == "" {
		err = errors.New("forward: current user response is missing user identity")
	}
	return &envelope.User, response, err
}

// CurrentSession returns the caller's own session, roles included -- the way a token client learns what it may do. Current returns only
// the user. GET /api/users/current (published getCurrentUser).
func (s *UsersService) CurrentSession(ctx context.Context) (*UserSession, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.CurrentSession")
	out := new(UserSession)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *UsersService) ResetPassword(ctx context.Context, input PasswordResetRequest) (*Response, error) {
	input.CurrentPassword = strings.TrimSpace(input.CurrentPassword)
	input.NewPassword = strings.TrimSpace(input.NewPassword)
	if input.CurrentPassword == "" || input.NewPassword == "" {
		return nil, errors.New("forward: current and new password are required")
	}
	form := url.Values{"password": []string{input.CurrentPassword}, "newPassword": []string{input.NewPassword}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/users/current/password", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = markOperation(req, "Users.ResetPassword")
	response, err := s.client.Do(req, nil)
	if err == nil && response.StatusCode != http.StatusNoContent {
		err = fmt.Errorf("forward: password reset returned HTTP %d, want 204", response.StatusCode)
	}
	return response, err
}

func (s *UsersService) ListTokens(ctx context.Context) ([]UserToken, *Response, error) {
	result := listResponse[UserToken]{Keys: []string{"tokens"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current/tokens", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.ListTokens")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *UsersService) DeleteToken(ctx context.Context, tokenName string) (*Response, error) {
	tokenName = strings.TrimSpace(tokenName)
	if tokenName == "" {
		return nil, errors.New("forward: token name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/users/current/tokens/"+url.PathEscape(tokenName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Users.DeleteToken")
	response, err := s.client.Do(req, nil)
	if isGone(err) {
		return response, nil
	}
	return response, err
}

// CreateToken mints an API token for the signed-in user. POST /api/users/current/tokens (UserController) requires the
// user's password even on an authenticated session: confirmed live (v1.0.0-261005), where an impersonated cookie session
// sending no password was answered 400 "Required request parameter 'password' ... is not present" and created nothing.
// So Client.ImpersonatedClient cannot mint a token for a user whose password nobody holds; that needs the user's own
// stored password or a mint on the platform side.
func (s *UsersService) CreateToken(ctx context.Context, tokenName, password string) (*UserTokenRegistration, *Response, error) {
	tokenName, password = strings.TrimSpace(tokenName), strings.TrimSpace(password)
	if tokenName == "" || password == "" {
		return nil, nil, errors.New("forward: token name and password are required")
	}
	form := url.Values{"password": []string{password}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/users/current/tokens?name="+url.QueryEscape(tokenName), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = markOperation(req, "Users.CreateToken")
	out := new(UserTokenRegistration)
	response, err := s.client.doRequired(req, out)
	if err == nil && (strings.TrimSpace(out.Token.AccessKey) == "" || strings.TrimSpace(out.Secret) == "") {
		err = errors.New("forward: token registration response is missing access key or secret")
	}
	return out, response, err
}

// UserRoles is GET /api/users/{id}/roles: org-level roles and per-network
// roles. It exists so a reconcile can ask before it writes -- granting org
// admin is not idempotent on the wire and re-granting flaps a user's
// network-scoped permissions.
type UserRoles struct {
	// System are Forward-internal roles (ADMIN, ARTIFACT_UPLOADER, SUPPORT_ADMIN, FREE_TRIAL_PROVISIONER); empty for an ordinary user.
	// There is no SUPPORT system role: a user supports an org through SupportedOrgIDs.
	System  []string            `json:"system,omitempty"`
	Org     []string            `json:"org"`
	Network map[string][]string `json:"network"`
	// SupportedOrgIDs are the orgs a Forward support user can currently impersonate into: grants (Admin.SetSupportedOrganizations,
	// Admin.AddSupportedOrganization) that have not expired. A grant is valid for its expires-in-days after the user's last activity in that
	// org, not after it was made, and an expired grant is not listed. It is always empty for a Forward ADMIN, who can reach every org
	// without grants, so an empty list does not mean no support access there.
	SupportedOrgIDs []string `json:"supportedOrgIds,omitempty"`
}

// UnmarshalJSON accepts both network-role shapes. Forward's UserRoles maps
// each network to a single NetworkRole ("ADMIN"); a list form is kept for
// builds that return one. Decoding the single form into []string used to fail
// the whole response as soon as a user held any network role.
func (r *UserRoles) UnmarshalJSON(data []byte) error {
	var raw struct {
		System          json.RawMessage            `json:"system"`
		Org             json.RawMessage            `json:"org"`
		Network         map[string]json.RawMessage `json:"network"`
		SupportedOrgIDs []string                   `json:"supportedOrgIds"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if r.System, err = roleList("system", raw.System); err != nil {
		return err
	}
	if r.Org, err = roleList("org", raw.Org); err != nil {
		return err
	}
	r.SupportedOrgIDs = raw.SupportedOrgIDs
	r.Network = nil
	if raw.Network == nil {
		return nil
	}
	r.Network = make(map[string][]string, len(raw.Network))
	for networkID, value := range raw.Network {
		value = bytes.TrimSpace(value)
		if len(value) == 0 || bytes.Equal(value, []byte("null")) {
			r.Network[networkID] = nil
			continue
		}
		if value[0] == '[' {
			var roles []string
			if err := json.Unmarshal(value, &roles); err != nil {
				return fmt.Errorf("forward: network %s roles: %w", networkID, err)
			}
			r.Network[networkID] = roles
			continue
		}
		var role string
		if err := json.Unmarshal(value, &role); err != nil {
			return fmt.Errorf("forward: network %s role: %w", networkID, err)
		}
		r.Network[networkID] = []string{role}
	}
	return nil
}

// NetworkRoles returns the roles held on networkID (usually one), or nil.
func (r UserRoles) NetworkRoles(networkID string) []string {
	return r.Network[strings.TrimSpace(networkID)]
}

// HasOrgAdmin reports whether Org carries ADMIN (case-insensitive).
func (r UserRoles) HasOrgAdmin() bool {
	for _, role := range r.Org {
		if strings.EqualFold(strings.TrimSpace(role), "ADMIN") {
			return true
		}
	}
	return false
}

func (s *UsersService) Roles(ctx context.Context, userID string) (*UserRoles, *Response, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil, errors.New("forward: user ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/"+url.PathEscape(userID)+"/roles", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Roles")
	out := new(UserRoles)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}
