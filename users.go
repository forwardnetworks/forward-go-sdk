package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type UsersService service

type User struct {
	ID              Identifier `json:"id"`
	OrgID           Identifier `json:"orgId"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	MustSetPassword bool       `json:"mustSetPassword"`
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
	if isStatus(err, http.StatusNotFound) {
		return response, nil
	}
	return response, err
}

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
	Org     []string            `json:"org"`
	Network map[string][]string `json:"network"`
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
