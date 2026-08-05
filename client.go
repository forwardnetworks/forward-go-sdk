package forward

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout   = 60 * time.Second
	defaultUserAgent = "forward-go-sdk"
)

// Config contains the settings used to create a Client.
//
// BaseURL is the Forward appliance URL, for example https://fwd.example.com.
// Both username/password credentials and API tokens use HTTP Basic
// authentication. APIToken must have the form accessKey:secret.
type Config struct {
	BaseURL  string
	Username string
	Password string
	APIToken string
	// AuthMode distinguishes user, service, collector, browser-session, and
	// deliberately unauthenticated clients. The zero value preserves the
	// historical per-user behavior.
	AuthMode AuthMode
	// Cookies seed the same-origin cookie jar used by browser-session clients.
	// They are rejected for every other authentication mode.
	Cookies []*http.Cookie
	// NetworkID optionally binds the client to a default network. Services that
	// document scoped operation accept an empty network ID when it is bound.
	NetworkID string

	UserAgent          string
	HTTPClient         *http.Client
	InsecureSkipVerify bool

	// Hooks receive request lifecycle events. Query strings and bodies are
	// intentionally omitted so credentials and version-specific values are not
	// leaked into telemetry.
	Hooks []Hook

	// Capabilities declares what is known about the target appliance build.
	// Unlisted capabilities remain unknown and are attempted normally.
	Capabilities CapabilityProfile
}

// Client is a Forward Networks REST API client.
type Client struct {
	httpClient     *http.Client
	baseURL        *url.URL
	username       string
	password       string
	authMode       AuthMode
	userAgent      string
	hooks          []Hook
	networkID      string
	unavailableErr error
	capabilities   *capabilityRegistry

	Version        *VersionService
	Networks       *NetworksService
	Devices        *DevicesService
	ClassicDevices *ClassicDevicesService
	Credentials    *CredentialsService
	CollectorTasks *CollectorTasksService
	CloudAccounts  *CloudAccountsService
	Webhooks       *WebhooksService
	Snapshots      *SnapshotsService
	NQE            *NQEService
	Predict        *PredictService
	AI             *AIService
	AIAssist       *AIAssistService
	Properties     *PropertiesService
	Organizations  *OrganizationsService
	Checks         *ChecksService
	DeviceTags     *DeviceTagsService
	Performance    *PerformanceService
	Capabilities   *CapabilitiesService
	Raw            *RawService
	Collectors     *CollectorsService
	Backups        *BackupsService
	Browser        *BrowserService
	Endpoints      *EndpointsService
	Locations      *LocationsService
	Proxies        *ProxiesService
	Users          *UsersService
	Admin          *AdminService
	NQERepository  *NQERepositoryService
	Banners        *BannersService
	Configuration  *ConfigurationService
	Integrations   *IntegrationsService
	Topology       *TopologyService
	Collections    *CollectionsService
	JumpServers    *JumpServersService
	Compatibility  *CompatibilityService
}

// Response wraps an HTTP response returned by the Forward API.
type Response struct {
	*http.Response
}

// NewClient validates cfg and returns a configured Forward API client.
func NewClient(cfg Config) (*Client, error) {
	baseURL, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}

	authMode, err := normalizeAuthMode(cfg.AuthMode)
	if err != nil {
		return nil, err
	}
	username, password, err := authCredentials(cfg, authMode)
	if err != nil {
		return nil, err
	}
	if authMode != AuthModeBrowser && len(copyCookies(cfg.Cookies)) != 0 {
		return nil, errors.New("forward: cookies require browser authentication mode")
	}

	httpClient, err := configureHTTPClient(cfg.HTTPClient, cfg.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}
	secureRedirects(httpClient, baseURL)
	if authMode == AuthModeBrowser {
		if httpClient.Jar == nil {
			httpClient.Jar, err = cookiejar.New(nil)
			if err != nil {
				return nil, fmt.Errorf("forward: create cookie jar: %w", err)
			}
		}
		if cookies := copyCookies(cfg.Cookies); len(cookies) != 0 {
			httpClient.Jar.SetCookies(baseURL, cookies)
		}
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	c := &Client{
		httpClient:   httpClient,
		baseURL:      baseURL,
		username:     username,
		password:     password,
		authMode:     authMode,
		userAgent:    userAgent,
		hooks:        append([]Hook(nil), cfg.Hooks...),
		networkID:    strings.TrimSpace(cfg.NetworkID),
		capabilities: newCapabilityRegistry(cfg.Capabilities),
	}
	c.bindServices()

	return c, nil
}

// NewUnavailable returns a fail-closed client that preserves a credential or
// configuration error until the first attempted operation. It exists for
// service seams that must always carry a client value even when per-tenant
// credential resolution failed.
func NewUnavailable(networkID string, cause error) *Client {
	if cause == nil {
		cause = errors.New("Forward access is unavailable")
	}
	c := &Client{
		httpClient:     &http.Client{Timeout: defaultTimeout},
		networkID:      strings.TrimSpace(networkID),
		unavailableErr: cause,
		capabilities:   newCapabilityRegistry(CapabilityProfile{}),
	}
	c.bindServices()
	return c
}

// ForNetwork returns a cheap client clone bound to networkID. The clone shares
// the HTTP transport, credentials, hooks, and capability registry; it does not
// authenticate again. Service values are rebound to the clone so an empty
// network argument on a scoped service resolves against the clone rather than
// the parent.
func (c *Client) ForNetwork(networkID string) *Client {
	if c == nil {
		return NewUnavailable(networkID, errors.New("forward: client is nil"))
	}
	networkID = strings.TrimSpace(networkID)
	if networkID == "" || networkID == c.networkID {
		return c
	}
	clone := *c
	clone.networkID = networkID
	clone.bindServices()
	return &clone
}

// Network returns the default network ID bound to the client.
func (c *Client) Network() string {
	if c == nil {
		return ""
	}
	return c.networkID
}

func (c *Client) bindServices() {
	c.Version = (*VersionService)(&service{client: c})
	c.Networks = (*NetworksService)(&service{client: c})
	c.Devices = (*DevicesService)(&service{client: c})
	c.ClassicDevices = (*ClassicDevicesService)(&service{client: c})
	c.Credentials = (*CredentialsService)(&service{client: c})
	c.CollectorTasks = (*CollectorTasksService)(&service{client: c})
	c.CloudAccounts = (*CloudAccountsService)(&service{client: c})
	c.Webhooks = (*WebhooksService)(&service{client: c})
	c.Snapshots = (*SnapshotsService)(&service{client: c})
	c.NQE = (*NQEService)(&service{client: c})
	c.Predict = (*PredictService)(&service{client: c})
	c.AI = (*AIService)(&service{client: c})
	c.AIAssist = (*AIAssistService)(&service{client: c})
	c.Properties = (*PropertiesService)(&service{client: c})
	c.Organizations = (*OrganizationsService)(&service{client: c})
	c.Checks = (*ChecksService)(&service{client: c})
	c.DeviceTags = (*DeviceTagsService)(&service{client: c})
	c.Performance = (*PerformanceService)(&service{client: c})
	c.Capabilities = (*CapabilitiesService)(&service{client: c})
	c.Raw = (*RawService)(&service{client: c})
	c.Collectors = (*CollectorsService)(&service{client: c})
	c.Backups = (*BackupsService)(&service{client: c})
	c.Browser = (*BrowserService)(&service{client: c})
	c.Endpoints = (*EndpointsService)(&service{client: c})
	c.Locations = (*LocationsService)(&service{client: c})
	c.Proxies = (*ProxiesService)(&service{client: c})
	c.Users = (*UsersService)(&service{client: c})
	c.Admin = (*AdminService)(&service{client: c})
	c.NQERepository = (*NQERepositoryService)(&service{client: c})
	c.Banners = (*BannersService)(&service{client: c})
	c.Configuration = (*ConfigurationService)(&service{client: c})
	c.Integrations = (*IntegrationsService)(&service{client: c})
	c.Topology = (*TopologyService)(&service{client: c})
	c.Collections = (*CollectionsService)(&service{client: c})
	c.JumpServers = (*JumpServersService)(&service{client: c})
	c.Compatibility = (*CompatibilityService)(&service{client: c})
}

// NewRequest creates an authenticated request relative to the appliance URL.
// API paths should use the same form as the REST documentation, such as
// /api/networks. Absolute URLs are rejected so credentials cannot be sent to
// another host accidentally.
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if c == nil {
		return nil, errors.New("forward: client is nil")
	}
	if ctx == nil {
		return nil, errors.New("forward: context is nil")
	}
	if c.unavailableErr != nil {
		return nil, &UnavailableError{NetworkID: c.networkID, Cause: c.unavailableErr}
	}

	target, err := c.resolve(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("forward: create request: %w", err)
	}
	if c.authMode != AuthModeBrowser && c.authMode != AuthModeNone {
		req.SetBasicAuth(c.username, c.password)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req = req.WithContext(withRequestMetadata(req.Context(), RequestMetadata{AuthMode: c.authMode}))

	return req, nil
}

func (c *Client) resolveNetworkID(networkID string) (string, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		networkID = c.Network()
	}
	if networkID == "" {
		return "", errors.New("forward: network ID is required (bind one with ForNetwork or pass it explicitly)")
	}
	return networkID, nil
}

// Do sends req and decodes a successful response into dst. If dst implements
// io.Writer, the response body is copied to it instead of JSON-decoded. The
// response body is always closed before Do returns.
func (c *Client) Do(req *http.Request, dst any) (*Response, error) {
	return c.do(req, dst, true, nil)
}

// doRequired is used by typed operations whose contract requires a JSON
// response body. Public Do keeps its historical empty-body-is-success policy.
func (c *Client) doRequired(req *http.Request, dst any) (*Response, error) {
	return c.do(req, dst, false, nil)
}

func (c *Client) doAccepted(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool) (*Response, error) {
	return c.do(req, dst, allowEmpty, accepted)
}

func (c *Client) doWithTimeout(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool, timeout time.Duration) (*Response, error) {
	if timeout <= 0 || c == nil || c.httpClient == nil || c.httpClient.Timeout == timeout {
		return c.do(req, dst, allowEmpty, accepted)
	}
	clone := *c
	httpClient := *c.httpClient
	httpClient.Timeout = timeout
	clone.httpClient = &httpClient
	return clone.do(req, dst, allowEmpty, accepted)
}

func (c *Client) do(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool) (*Response, error) {
	if c == nil {
		return nil, errors.New("forward: client is nil")
	}
	if req == nil {
		return nil, errors.New("forward: request is nil")
	}

	started := time.Now()
	metadata := MetadataFromRequest(req)
	c.emit(req.Context(), Event{Type: EventRequest, Method: req.Method, Path: req.URL.Path, Operation: metadata.Operation, AuthMode: metadata.AuthMode})
	resp, err := c.httpClient.Do(req)
	if err != nil {
		err = fmt.Errorf("forward: send request: %w", err)
		c.emit(req.Context(), Event{Type: EventResponse, Method: req.Method, Path: req.URL.Path, Duration: time.Since(started), Err: errors.New("forward: transport error"), Operation: metadata.Operation, AuthMode: metadata.AuthMode})
		return nil, err
	}
	defer resp.Body.Close()

	response := &Response{Response: resp}
	if accepted == nil {
		accepted = func(status int) bool { return status >= http.StatusOK && status < http.StatusMultipleChoices }
	}
	if !accepted(resp.StatusCode) {
		err = newErrorResponse(resp)
		c.emit(req.Context(), Event{Type: EventResponse, Method: req.Method, Path: req.URL.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Err: fmt.Errorf("forward: HTTP status %d", resp.StatusCode), Operation: metadata.Operation, AuthMode: metadata.AuthMode})
		return response, err
	}

	if dst == nil {
		_, err = io.Copy(io.Discard, resp.Body)
	} else if writer, ok := dst.(io.Writer); ok {
		_, err = io.Copy(writer, resp.Body)
	} else {
		err = json.NewDecoder(resp.Body).Decode(dst)
		if errors.Is(err, io.EOF) && allowEmpty {
			err = nil
		}
	}
	if err != nil {
		err = fmt.Errorf("forward: decode response: %w", err)
		c.emit(req.Context(), Event{Type: EventResponse, Method: req.Method, Path: req.URL.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Err: errors.New("forward: response decode error"), Operation: metadata.Operation, AuthMode: metadata.AuthMode})
		return response, err
	}

	c.emit(req.Context(), Event{Type: EventResponse, Method: req.Method, Path: req.URL.Path, StatusCode: resp.StatusCode, Duration: time.Since(started), Operation: metadata.Operation, AuthMode: metadata.AuthMode})
	return response, nil
}

type service struct {
	client *Client
}

func (c *Client) newJSONRequest(ctx context.Context, method, path string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("forward: encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := c.NewRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

type pathScope uint8

const (
	pathScopeAPI pathScope = iota
	pathScopeBackup
	pathScopeBrowser
)

type requestAuth struct {
	mode     AuthMode
	username string
	password string
}

func (c *Client) newScopedRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
	scope pathScope,
	auth *requestAuth,
) (*http.Request, error) {
	if c == nil {
		return nil, errors.New("forward: client is nil")
	}
	if ctx == nil {
		return nil, errors.New("forward: context is nil")
	}
	if c.unavailableErr != nil {
		return nil, &UnavailableError{NetworkID: c.networkID, Cause: c.unavailableErr}
	}
	target, err := c.resolveScoped(path, scope)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("forward: create request: %w", err)
	}
	mode := c.authMode
	username, password := c.username, c.password
	if auth != nil {
		mode, username, password = auth.mode, auth.username, auth.password
	}
	if mode != AuthModeBrowser && mode != AuthModeNone {
		if strings.TrimSpace(username) == "" || password == "" {
			return nil, errors.New("forward: operation credentials are required")
		}
		req.SetBasicAuth(username, password)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req = req.WithContext(withRequestMetadata(req.Context(), RequestMetadata{AuthMode: mode}))
	return req, nil
}

func (c *Client) newScopedJSONRequest(
	ctx context.Context,
	method string,
	path string,
	payload any,
	scope pathScope,
	auth *requestAuth,
) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("forward: encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := c.newScopedRequest(ctx, method, path, body, scope, auth)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) resolve(path string) (*url.URL, error) {
	return c.resolveScoped(path, pathScopeAPI)
}

func (c *Client) resolveScoped(path string, scope pathScope) (*url.URL, error) {
	value := strings.TrimSpace(path)
	if value == "" {
		return nil, errors.New("forward: request path is required")
	}

	rel, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("forward: parse request path: %w", err)
	}
	if rel.IsAbs() || rel.Host != "" || rel.User != nil {
		return nil, errors.New("forward: request path must not be an absolute URL")
	}
	allowed := strings.HasPrefix(rel.Path, "/api/") || rel.Path == "/api"
	switch scope {
	case pathScopeBackup:
		allowed = rel.Path == "/backup-settings" || rel.Path == "/backup-settings/storage" || rel.Path == "/backups"
	case pathScopeBrowser:
		allowed = allowed || rel.Path == "/login" || rel.Path == "/public/csrf"
	}
	if !allowed {
		return nil, errors.New("forward: request path is outside the typed service scope")
	}

	return c.baseURL.ResolveReference(rel), nil
}

func normalizeBaseURL(raw string) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, errors.New("forward: base URL is required")
	}

	u, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("forward: parse base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("forward: base URL must use http or https")
	}
	if u.Host == "" {
		return nil, errors.New("forward: base URL must include a host")
	}
	if u.User != nil {
		return nil, errors.New("forward: base URL must not include credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("forward: base URL must not include a query or fragment")
	}

	switch strings.TrimRight(u.Path, "/") {
	case "", "/api":
		u.Path = "/"
		u.RawPath = ""
	default:
		return nil, errors.New("forward: base URL path must be empty or /api")
	}

	return u, nil
}

func configureHTTPClient(source *http.Client, insecure bool) (*http.Client, error) {
	var client http.Client
	if source == nil {
		client = http.Client{Timeout: defaultTimeout}
	} else {
		client = *source
	}
	if !insecure {
		return &client, nil
	}

	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		return nil, errors.New("forward: InsecureSkipVerify requires an *http.Transport")
	}
	clone := httpTransport.Clone()
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	}
	clone.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // Explicit opt-in for development appliances.
	client.Transport = clone

	return &client, nil
}

func secureRedirects(client *http.Client, origin *url.URL) {
	callerPolicy := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !sameOrigin(req.URL, origin) {
			// Returning ErrUseLastResponse prevents the redirected request and lets
			// Do surface the original 3xx response as an API error.
			return http.ErrUseLastResponse
		}
		if callerPolicy != nil {
			return callerPolicy(req, via)
		}
		if len(via) >= 10 {
			return errors.New("forward: stopped after 10 redirects")
		}
		return nil
	}
}

func sameOrigin(candidate, origin *url.URL) bool {
	return candidate != nil && origin != nil &&
		strings.EqualFold(candidate.Scheme, origin.Scheme) &&
		strings.EqualFold(candidate.Host, origin.Host)
}
