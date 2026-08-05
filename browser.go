package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// BrowserService implements Forward's cookie/CSRF session flow on the same
// Client transport and hook seam as the JSON API services.
type BrowserService service

type BrowserUser struct {
	ID              Identifier `json:"id"`
	OrgID           Identifier `json:"orgId"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	MustSetPassword bool       `json:"mustSetPassword"`
}

type BrowserCSRFToken struct {
	HeaderName    string `json:"headerName"`
	ParameterName string `json:"parameterName"`
	Token         string `json:"token"`
}

type BrowserLoginResult struct {
	Location        string         `json:"location"`
	Token           string         `json:"token,omitempty"`
	PasswordExpired bool           `json:"passwordExpired,omitempty"`
	Cookies         []*http.Cookie `json:"-"`
}

type BrowserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var (
	browserParameterNameRE = regexp.MustCompile(`"parameterName"\s*:\s*"([^"]+)"`)
	browserTokenRE         = regexp.MustCompile(`"token"\s*:\s*"([^"]+)"`)
	browserHiddenInputRE   = regexp.MustCompile(`(?is)<input[^>]+name=["'](_csrf)["'][^>]+value=["']([^"']+)["']`)
)

func (s *BrowserService) CurrentUser(ctx context.Context) (*BrowserUser, *Response, error) {
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/users/current", nil, pathScopeBrowser, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Browser.CurrentUser")
	var envelope struct {
		User BrowserUser `json:"user"`
	}
	response, err := s.client.doRequired(req, &envelope)
	if err == nil && envelope.User.ID == "" && strings.TrimSpace(envelope.User.Username) == "" {
		err = errors.New("forward: browser session response is missing user identity")
	}
	return &envelope.User, response, err
}

func (s *BrowserService) PublicCSRFAPI(ctx context.Context) (*BrowserCSRFToken, *Response, error) {
	return s.publicCSRF(ctx, "/api/public/csrf", "Browser.PublicCSRFAPI")
}

func (s *BrowserService) PublicCSRFLegacy(ctx context.Context) (*BrowserCSRFToken, *Response, error) {
	return s.publicCSRF(ctx, "/public/csrf", "Browser.PublicCSRFLegacy")
}

func (s *BrowserService) publicCSRF(ctx context.Context, path, operation string) (*BrowserCSRFToken, *Response, error) {
	if err := s.requireBrowserOrNone(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, path, nil, pathScopeBrowser, &requestAuth{mode: AuthModeNone})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(BrowserCSRFToken)
	response, err := s.client.doRequired(req, out)
	if err == nil {
		out.HeaderName = strings.TrimSpace(out.HeaderName)
		out.ParameterName = strings.TrimSpace(out.ParameterName)
		out.Token = strings.TrimSpace(out.Token)
		if out.ParameterName == "" || out.Token == "" {
			err = errors.New("forward: CSRF response is missing token fields")
		}
	}
	return out, response, err
}

func (s *BrowserService) LoginPageCSRF(ctx context.Context) (*BrowserCSRFToken, *Response, error) {
	if err := s.requireBrowserOrNone(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/login", nil, pathScopeBrowser, &requestAuth{mode: AuthModeNone})
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req = markOperation(req, "Browser.LoginPageCSRF")
	var body bytes.Buffer
	response, err := s.client.Do(req, &body)
	if err != nil {
		return nil, response, err
	}
	token, err := parseBrowserCSRF(body.String())
	return token, response, err
}

func (s *BrowserService) LoginAPI(ctx context.Context, input BrowserLoginRequest, csrf *BrowserCSRFToken) (*BrowserLoginResult, *Response, error) {
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	input, err := s.loginInput(input)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedJSONRequest(ctx, http.MethodPost, "/api/auth/login", input, pathScopeBrowser, &requestAuth{mode: AuthModeBrowser})
	if err != nil {
		return nil, nil, err
	}
	if csrf != nil && strings.TrimSpace(csrf.Token) != "" {
		header := strings.TrimSpace(csrf.HeaderName)
		if header == "" {
			header = "X-CSRF-TOKEN"
		}
		req.Header.Set(header, strings.TrimSpace(csrf.Token))
	}
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	req = markOperation(req, "Browser.LoginAPI")
	return s.finishLogin(req)
}

func (s *BrowserService) LoginLegacy(ctx context.Context, input BrowserLoginRequest, csrf BrowserCSRFToken) (*BrowserLoginResult, *Response, error) {
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	input, err := s.loginInput(input)
	if err != nil {
		return nil, nil, err
	}
	parameter := strings.TrimSpace(csrf.ParameterName)
	if parameter == "" || strings.TrimSpace(csrf.Token) == "" {
		return nil, nil, errors.New("forward: legacy login requires a CSRF parameter and token")
	}
	form := url.Values{"username": []string{input.Username}, "password": []string{input.Password}, parameter: []string{strings.TrimSpace(csrf.Token)}}
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, "/login", strings.NewReader(form.Encode()), pathScopeBrowser, &requestAuth{mode: AuthModeBrowser})
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	req = markOperation(req, "Browser.LoginLegacy")
	return s.finishLogin(req)
}

// Login performs the measured Skyforge fallback order: public API CSRF,
// legacy public CSRF, login-page CSRF, JSON login, then legacy form login only
// when the JSON route is unsupported or unauthorized.
func (s *BrowserService) Login(ctx context.Context) (*BrowserLoginResult, error) {
	input, err := s.loginInput(BrowserLoginRequest{})
	if err != nil {
		return nil, err
	}
	csrf, _, csrfErr := s.PublicCSRFAPI(ctx)
	if csrfErr != nil {
		csrf, _, csrfErr = s.PublicCSRFLegacy(ctx)
	}
	if csrfErr != nil {
		csrf, _, csrfErr = s.LoginPageCSRF(ctx)
	}
	result, response, err := s.LoginAPI(ctx, input, csrf)
	if err == nil {
		return result, nil
	}
	if response == nil || (response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden) {
		return nil, err
	}
	if csrf == nil {
		return nil, fmt.Errorf("forward: legacy browser login has no CSRF token: %w", csrfErr)
	}
	result, _, err = s.LoginLegacy(ctx, input, *csrf)
	return result, err
}

func (s *BrowserService) Impersonate(ctx context.Context, targetUserID string) (*BrowserLoginResult, *Response, error) {
	return s.impersonate(ctx, targetUserID, false)
}

func (s *BrowserService) ImpersonateWithServiceCredential(ctx context.Context, targetUserID string) (*BrowserLoginResult, *Response, error) {
	return s.impersonate(ctx, targetUserID, true)
}

func (s *BrowserService) impersonate(ctx context.Context, targetUserID string, basic bool) (*BrowserLoginResult, *Response, error) {
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	targetUserID = strings.TrimSpace(targetUserID)
	if targetUserID == "" {
		return nil, nil, errors.New("forward: impersonation user ID is required")
	}
	if !strings.HasSuffix(strings.ToLower(targetUserID), ".full") {
		targetUserID += ".full"
	}
	auth := &requestAuth{mode: AuthModeBrowser}
	operation := "Browser.Impersonate"
	if basic {
		auth = &requestAuth{mode: AuthModeService, username: s.client.username, password: s.client.password}
		operation = "Browser.ImpersonateWithServiceCredential"
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/admin/impersonate?user="+url.QueryEscape(targetUserID), nil, pathScopeBrowser, auth)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
	req = markOperation(req, operation)
	var body bytes.Buffer
	response, err := s.client.doAccepted(req, &body, true, func(status int) bool { return status >= 200 && status < 400 })
	if err != nil {
		return nil, response, err
	}
	if response.Request != nil && response.Request.URL != nil && strings.EqualFold(strings.Trim(response.Request.URL.Path, "/"), "login") {
		return nil, response, errors.New("forward: impersonation redirected to login")
	}
	cookies := s.Cookies()
	if len(cookies) == 0 {
		return nil, response, errors.New("forward: impersonation returned no session cookies")
	}
	return &BrowserLoginResult{Cookies: cookies}, response, nil
}

func (s *BrowserService) Cookies() []*http.Cookie {
	if s == nil || s.client == nil || s.client.httpClient == nil || s.client.httpClient.Jar == nil || s.client.baseURL == nil {
		return nil
	}
	return copyCookies(s.client.httpClient.Jar.Cookies(s.client.baseURL))
}

func (s *BrowserService) finishLogin(req *http.Request) (*BrowserLoginResult, *Response, error) {
	var body bytes.Buffer
	response, err := s.client.Do(req, &body)
	if err != nil {
		return nil, response, err
	}
	out := new(BrowserLoginResult)
	decodeErr := json.Unmarshal(body.Bytes(), out)
	if decodeErr == nil && out.PasswordExpired {
		return nil, response, errors.New("forward: login requires password settlement")
	}
	out.Cookies = s.Cookies()
	finalIsLogin := response.Request != nil && response.Request.URL != nil && strings.EqualFold(strings.Trim(response.Request.URL.Path, "/"), "login")
	if decodeErr != nil && (len(out.Cookies) == 0 || finalIsLogin) {
		return nil, response, fmt.Errorf("forward: decode login response: %w", decodeErr)
	}
	if len(out.Cookies) == 0 {
		return nil, response, errors.New("forward: login returned no session cookies")
	}
	return out, response, nil
}

func (s *BrowserService) loginInput(input BrowserLoginRequest) (BrowserLoginRequest, error) {
	if strings.TrimSpace(input.Username) == "" {
		input.Username = s.client.username
	}
	if input.Password == "" {
		input.Password = s.client.password
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || input.Password == "" {
		return input, errors.New("forward: browser login credentials are required")
	}
	return input, nil
}

func (s *BrowserService) requireBrowser() error {
	if s == nil || s.client == nil {
		return errors.New("forward: browser service is nil")
	}
	if s.client.authMode != AuthModeBrowser {
		return errors.New("forward: browser operation requires browser authentication mode")
	}
	return nil
}

func (s *BrowserService) requireBrowserOrNone() error {
	if s == nil || s.client == nil {
		return errors.New("forward: browser service is nil")
	}
	if s.client.authMode != AuthModeBrowser && s.client.authMode != AuthModeNone {
		return errors.New("forward: public browser operation requires browser or unauthenticated mode")
	}
	return nil
}

func parseBrowserCSRF(body string) (*BrowserCSRFToken, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("forward: empty login page")
	}
	parameter := browserParameterNameRE.FindStringSubmatch(body)
	token := browserTokenRE.FindStringSubmatch(body)
	if len(parameter) > 1 && len(token) > 1 {
		return &BrowserCSRFToken{ParameterName: strings.TrimSpace(parameter[1]), Token: strings.TrimSpace(token[1])}, nil
	}
	hidden := browserHiddenInputRE.FindStringSubmatch(body)
	if len(hidden) > 2 {
		return &BrowserCSRFToken{ParameterName: strings.TrimSpace(hidden[1]), Token: strings.TrimSpace(hidden[2])}, nil
	}
	return nil, errors.New("forward: CSRF token not found in login page")
}

// Reachable reports whether an unauthenticated /api/version request received
// any HTTP response. Status, including 401 or 404, is deliberately ignored.
func (s *VersionService) Reachable(ctx context.Context) (bool, *Response, error) {
	if s == nil || s.client == nil {
		return false, nil, errors.New("forward: version service is nil")
	}
	if s.client.authMode != AuthModeNone {
		return false, nil, errors.New("forward: reachability requires an unauthenticated client")
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/version", nil, pathScopeAPI, &requestAuth{mode: AuthModeNone})
	if err != nil {
		return false, nil, err
	}
	req = markOperation(req, "Version.Reachable")
	response, err := s.client.doAccepted(req, nil, true, func(int) bool { return true })
	return err == nil, response, err
}
