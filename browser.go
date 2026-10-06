package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
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
	AuthSource      string     `json:"authSource,omitempty"`
	ExternalGroups  []string   `json:"externalGroups,omitempty"`
}

// BrowserSessionRoles are the roles Forward resolved for the session's
// principal: org roles (e.g. ADMIN) and network roles keyed by network ID.
type BrowserSessionRoles struct {
	Org     []string          `json:"org"`
	Network map[string]string `json:"network"`
}

// BrowserSession is GET /api/users/current in full: who the session is and
// what Forward lets it do. For an impersonated session it is the target.
type BrowserSession struct {
	User  BrowserUser         `json:"user"`
	Roles BrowserSessionRoles `json:"roles"`
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

// CurrentSession reads the session's principal together with its resolved
// roles, so a caller can prove a sign-in carries the access it needs.
func (s *BrowserService) CurrentSession(ctx context.Context) (*BrowserSession, *Response, error) {
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/api/users/current", nil, pathScopeBrowser, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Browser.CurrentSession")
	out := new(BrowserSession)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.User.ID == "" && strings.TrimSpace(out.User.Username) == "" {
		err = errors.New("forward: browser session response is missing user identity")
	}
	return out, response, err
}

func (s *BrowserService) PublicCSRFAPI(ctx context.Context) (*BrowserCSRFToken, *Response, error) {
	return s.publicCSRF(ctx, "/api/public/csrf", "Browser.PublicCSRFAPI")
}

func (s *BrowserService) publicCSRF(ctx context.Context, path, operation string) (*BrowserCSRFToken, *Response, error) {
	if err := s.requireBrowserOrNone(); err != nil {
		return nil, nil, err
	}
	return s.client.fetchPublicCSRF(ctx, path, operation)
}

// fetchPublicCSRF reads the session's CSRF token. It is a client helper, not a
// service method, because every browser-mode write reaches it (see
// browser_csrf.go); the route stays listed under Browser.PublicCSRFAPI.
func (c *Client) fetchPublicCSRF(ctx context.Context, path, operation string) (*BrowserCSRFToken, *Response, error) {
	req, err := c.newScopedRequest(ctx, http.MethodGet, path, nil, pathScopeBrowser, &requestAuth{mode: AuthModeNone})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(BrowserCSRFToken)
	response, err := c.doRequired(req, out)
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
	// The form already carries this session's token as a parameter.
	req = markOperation(skipBrowserCSRF(req), "Browser.LoginLegacy")
	return s.finishLogin(req)
}

// Login performs Forward's browser login: a CSRF token from the public API
// endpoint (GET /api/public/csrf), falling back to the token embedded in the
// login page, then the Spring Security form login (POST /login).
//
// It used to try a JSON POST /api/auth/login and a root GET /public/csrf
// first. Neither route exists on any Forward build this SDK targets (checked
// at primary 15398425a69 and stable 67e89c87124): every login spent two
// requests on 404s before reaching the form login that actually works.
func (s *BrowserService) Login(ctx context.Context) (*BrowserLoginResult, error) {
	input, err := s.loginInput(BrowserLoginRequest{})
	if err != nil {
		return nil, err
	}
	csrf, _, csrfErr := s.PublicCSRFAPI(ctx)
	if csrfErr != nil {
		csrf, _, csrfErr = s.LoginPageCSRF(ctx)
	}
	if csrfErr != nil || csrf == nil {
		return nil, fmt.Errorf("forward: browser login has no CSRF token: %w", csrfErr)
	}
	result, _, err := s.LoginLegacy(ctx, input, *csrf)
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
	s.client.resetBrowserCSRF()
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
	s.client.resetBrowserCSRF()
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

// ImpersonatedClient returns a new client acting as targetUserID, for platforms that own an organization's automation
// user and never hold a secret for it. c must be a browser-mode client (Config{AuthMode: AuthModeBrowser, Username,
// Password}) whose user may impersonate (Forward's admin impersonation route). It logs in on a private cookie jar,
// impersonates, and returns a client carrying only the impersonated session's cookies: c is not changed, and the
// returned client holds no password, so it cannot sign back in as the administrator. The session ends when Forward
// expires it; build a new client then.
//
//	admin, _ := forward.NewClient(forward.Config{BaseURL: u, Username: "admin", Password: pw, AuthMode: forward.AuthModeBrowser})
//	as, err := admin.ImpersonatedClient(ctx, "2342")
func (c *Client) ImpersonatedClient(ctx context.Context, targetUserID string) (*Client, error) {
	if c == nil || c.httpClient == nil {
		return nil, errors.New("forward: client is nil")
	}
	if c.authMode != AuthModeBrowser {
		return nil, errors.New("forward: impersonation requires browser authentication mode")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("forward: create cookie jar: %w", err)
	}
	httpClient := *c.httpClient
	httpClient.Jar = jar
	admin := *c
	admin.httpClient = &httpClient
	admin.csrf = &browserCSRFState{}
	admin.bindServices()
	if _, err := admin.Browser.Login(ctx); err != nil {
		return nil, fmt.Errorf("forward: sign in to impersonate: %w", err)
	}
	if _, _, err := admin.Browser.Impersonate(ctx, targetUserID); err != nil {
		return nil, err
	}
	as := admin
	as.username, as.password = "", ""
	as.csrf = &browserCSRFState{}
	as.bindServices()
	return &as, nil
}
