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

type Users service

func (s *Users) Current(ctx context.Context) (*ports.User, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	var envelope struct {
		User ports.User `json:"user"`
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
func (s *Users) CurrentSession(ctx context.Context) (*ports.UserSession, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.CurrentSession")
	out := new(ports.UserSession)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *Users) ResetPassword(ctx context.Context, input ports.PasswordResetRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Users) ListTokens(ctx context.Context) ([]ports.UserToken, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.UserToken]{Keys: []string{"tokens"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/current/tokens", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.ListTokens")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *Users) DeleteToken(ctx context.Context, tokenName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

// CreateToken mints an API token for the signed-in user. POST /api/users/current/tokens (UserController) requires the
// user's password even on an authenticated session: confirmed live (v1.0.0-261005), where an impersonated cookie session
// sending no password was answered 400 "Required request parameter 'password' ... is not present" and created nothing.
// So Client.ImpersonatedClient cannot mint a token for a user whose password nobody holds; that needs the user's own
// stored password or a mint on the platform side.
func (s *Users) CreateToken(ctx context.Context, tokenName, password string) (*ports.UserTokenRegistration, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.UserTokenRegistration)
	response, err := s.client.doRequired(req, out)
	if err == nil && (strings.TrimSpace(out.Token.AccessKey) == "" || strings.TrimSpace(out.Secret) == "") {
		err = errors.New("forward: token registration response is missing access key or secret")
	}
	return out, response, err
}

func (s *Users) Roles(ctx context.Context, userID string) (*ports.UserRoles, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil, errors.New("forward: user ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/users/"+url.PathEscape(userID)+"/roles", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Users.Roles")
	out := new(ports.UserRoles)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}
