package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
	AuthMode ports.AuthMode
	// Cookies seed the same-origin cookie jar used by browser-session clients.
	// They are rejected for every other authentication mode.
	Cookies []*http.Cookie
	// NetworkID optionally binds the client to a default network. Services that
	// document scoped operation accept an empty network ID when it is bound.
	NetworkID string

	UserAgent          string
	HTTPClient         *http.Client
	InsecureSkipVerify bool
	// RootCAs, when set, replaces the system roots used to verify the server's
	// certificate, so a Forward behind a private or self-signed CA can be
	// trusted without turning verification off. Build it with LoadRootCAs, or
	// start from x509.SystemCertPool to trust the system roots as well. Like
	// InsecureSkipVerify it needs an *http.Transport (or none) in HTTPClient;
	// the transport is cloned, never modified.
	RootCAs *x509.CertPool

	// Hooks receive request lifecycle events. Query strings and bodies are
	// intentionally omitted so credentials and version-specific values are not
	// leaked into telemetry.
	Hooks []Hook

	// Capabilities declares what is known about the target appliance build.
	// Unlisted capabilities remain unknown and are attempted normally.
	Capabilities ports.CapabilityProfile

	// Retry rides out transient failures. The zero value retries nothing.
	Retry RetryPolicy
}

// Client is a Forward Networks REST API client.
type Client struct {
	httpClient     *http.Client
	baseURL        *url.URL
	username       string
	password       string
	authMode       ports.AuthMode
	userAgent      string
	hooks          []Hook
	networkID      string
	unavailableErr error
	capabilities   *capabilityRegistry
	retry          RetryPolicy
	// cooldown is shared by every clone (ForNetwork) of this client, which share one credential and so one rate limit.
	cooldown *cooldown
	csrf     *browserCSRFState

	Version                 *Version
	Networks                *Networks
	Devices                 *Devices
	ClassicDevices          *ClassicDevices
	ControllerManagedSetups *ControllerManagedSetups
	DeviceDiscovery         *DeviceDiscovery
	ConnectivityTests       *ConnectivityTests
	CustomCommandGroups     *CustomCommandGroups
	CloudManagedSetups      *CloudManagedSetups
	SaseSetups              *SaseSetups
	Decorators              *Decorators
	Trace                   *Trace
	SyntheticNodes          *SyntheticNodes
	Credentials             *Credentials
	CollectorTasks          *CollectorTasks
	CloudAccounts           *CloudAccounts
	Webhooks                *Webhooks
	Snapshots               *Snapshots
	NQE                     *NQE
	Predict                 *Predict
	AI                      *AI
	Diffs                   *Diffs
	AIAssist                *AIAssist
	Properties              *Properties
	Organizations           *Organizations
	Checks                  *Checks
	DeviceTags              *DeviceTags
	Performance             *Performance
	Capabilities            *Capabilities
	Raw                     *Raw
	Collectors              *Collectors
	Backups                 *Backups
	Browser                 *Browser
	Endpoints               *Endpoints
	Locations               *Locations
	Proxies                 *Proxies
	Users                   *Users
	Admin                   *Admin
	NQERepository           *NQERepository
	Banners                 *Banners
	Jobs                    *Jobs
	NetworkMaps             *NetworkMaps
	Routing                 *Routing
	ThreatExposure          *ThreatExposure
	EdgeAssets              *EdgeAssets
	Licensing               *Licensing
	SoftwareCentral         *SoftwareCentral
	CVEIndex                *CVEIndex
	FwdAppConnection        *FwdAppConnection
	ChangeReports           *ChangeReports
	AccessControl           *AccessControl
	SAML                    *SAML
	Configuration           *Configuration
	Integrations            *Integrations
	Topology                *Topology
	Collections             *Collections
	JumpServers             *JumpServers
	Compatibility           *Compatibility
	TrustedCertificates     *TrustedCertificates
	Vulnerabilities         *Vulnerabilities
	Aliases                 *Aliases
	WanCircuits             *WanCircuits
	DataFiles               *DataFiles
	DataConnectors          *DataConnectors
	AuditLogs               *AuditLogs
	Dashboards              *Dashboards
	Scorecards              *Scorecards
	CollectionSchedules     *CollectionSchedules
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
	if authMode != ports.AuthModeBrowser && len(copyCookies(cfg.Cookies)) != 0 {
		return nil, errors.New("forward: cookies require browser authentication mode")
	}

	httpClient, err := configureHTTPClient(cfg.HTTPClient, cfg.InsecureSkipVerify, cfg.RootCAs)
	if err != nil {
		return nil, err
	}
	secureRedirects(httpClient, baseURL)
	if authMode == ports.AuthModeBrowser {
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
		csrf:         &browserCSRFState{},
		hooks:        append([]Hook(nil), cfg.Hooks...),
		networkID:    strings.TrimSpace(cfg.NetworkID),
		capabilities: newCapabilityRegistry(cfg.Capabilities),
		retry:        cfg.Retry,
		cooldown:     &cooldown{},
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
		capabilities:   newCapabilityRegistry(ports.CapabilityProfile{}),
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

// BaseURL returns the origin this client was built against, as a string, for
// reports that name where a call went.
func (c *Client) BaseURL() string {
	if c == nil || c.baseURL == nil {
		return ""
	}
	return strings.TrimRight(c.baseURL.String(), "/")
}

// Network returns the default network ID bound to the client.
func (c *Client) Network() string {
	if c == nil {
		return ""
	}
	return c.networkID
}

func (c *Client) bindServices() {
	c.Version = (*Version)(&service{client: c})
	c.Networks = (*Networks)(&service{client: c})
	c.Devices = (*Devices)(&service{client: c})
	c.ClassicDevices = (*ClassicDevices)(&service{client: c})
	c.ControllerManagedSetups = (*ControllerManagedSetups)(&service{client: c})
	c.DeviceDiscovery = (*DeviceDiscovery)(&service{client: c})
	c.ConnectivityTests = (*ConnectivityTests)(&service{client: c})
	c.CustomCommandGroups = (*CustomCommandGroups)(&service{client: c})
	c.CloudManagedSetups = (*CloudManagedSetups)(&service{client: c})
	c.SaseSetups = (*SaseSetups)(&service{client: c})
	c.Decorators = (*Decorators)(&service{client: c})
	c.Trace = (*Trace)(&service{client: c})
	c.SyntheticNodes = (*SyntheticNodes)(&service{client: c})
	c.Credentials = (*Credentials)(&service{client: c})
	c.CollectorTasks = (*CollectorTasks)(&service{client: c})
	c.CloudAccounts = (*CloudAccounts)(&service{client: c})
	c.Webhooks = (*Webhooks)(&service{client: c})
	c.Snapshots = (*Snapshots)(&service{client: c})
	c.NQE = (*NQE)(&service{client: c})
	c.Predict = (*Predict)(&service{client: c})
	c.AI = (*AI)(&service{client: c})
	c.Diffs = (*Diffs)(&service{client: c})
	c.AIAssist = (*AIAssist)(&service{client: c})
	c.Properties = (*Properties)(&service{client: c})
	c.Organizations = (*Organizations)(&service{client: c})
	c.Checks = (*Checks)(&service{client: c})
	c.DeviceTags = (*DeviceTags)(&service{client: c})
	c.Performance = (*Performance)(&service{client: c})
	c.Capabilities = (*Capabilities)(&service{client: c})
	c.Raw = (*Raw)(&service{client: c})
	c.Collectors = (*Collectors)(&service{client: c})
	c.Backups = (*Backups)(&service{client: c})
	c.Browser = (*Browser)(&service{client: c})
	c.Endpoints = (*Endpoints)(&service{client: c})
	c.Locations = (*Locations)(&service{client: c})
	c.Proxies = (*Proxies)(&service{client: c})
	c.Users = (*Users)(&service{client: c})
	c.Admin = (*Admin)(&service{client: c})
	c.NQERepository = (*NQERepository)(&service{client: c})
	c.Banners = (*Banners)(&service{client: c})
	c.Jobs = (*Jobs)(&service{client: c})
	c.NetworkMaps = (*NetworkMaps)(&service{client: c})
	c.Routing = (*Routing)(&service{client: c})
	c.ThreatExposure = (*ThreatExposure)(&service{client: c})
	c.EdgeAssets = (*EdgeAssets)(&service{client: c})
	c.Licensing = (*Licensing)(&service{client: c})
	c.SoftwareCentral = (*SoftwareCentral)(&service{client: c})
	c.CVEIndex = (*CVEIndex)(&service{client: c})
	c.FwdAppConnection = (*FwdAppConnection)(&service{client: c})
	c.ChangeReports = (*ChangeReports)(&service{client: c})
	c.AccessControl = (*AccessControl)(&service{client: c})
	c.SAML = (*SAML)(&service{client: c})
	c.Configuration = (*Configuration)(&service{client: c})
	c.Integrations = (*Integrations)(&service{client: c})
	c.Topology = (*Topology)(&service{client: c})
	c.Collections = (*Collections)(&service{client: c})
	c.JumpServers = (*JumpServers)(&service{client: c})
	c.Compatibility = (*Compatibility)(&service{client: c})
	c.TrustedCertificates = (*TrustedCertificates)(&service{client: c})
	c.Vulnerabilities = (*Vulnerabilities)(&service{client: c})
	c.Aliases = (*Aliases)(&service{client: c})
	c.WanCircuits = (*WanCircuits)(&service{client: c})
	c.DataFiles = (*DataFiles)(&service{client: c})
	c.DataConnectors = (*DataConnectors)(&service{client: c})
	c.AuditLogs = (*AuditLogs)(&service{client: c})
	c.Dashboards = (*Dashboards)(&service{client: c})
	c.Scorecards = (*Scorecards)(&service{client: c})
	c.CollectionSchedules = (*CollectionSchedules)(&service{client: c})
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
		return nil, &ports.UnavailableError{NetworkID: c.networkID, Cause: c.unavailableErr}
	}

	target, err := c.resolve(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("forward: create request: %w", err)
	}
	if c.authMode != ports.AuthModeBrowser && c.authMode != ports.AuthModeNone {
		req.SetBasicAuth(c.username, c.password)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req = req.WithContext(withRequestMetadata(req.Context(), RequestMetadata{AuthMode: c.authMode}))

	return req, nil
}

func (c *Client) resolveNetworkID(networkID string) (string, error) {
	return ports.ResolveNetworkID(networkID, c.Network())
}

// Do sends req and decodes a successful response into dst. If dst implements
// io.Writer, the response body is copied to it instead of JSON-decoded. The
// response body is always closed before Do returns.
func (c *Client) Do(req *http.Request, dst any) (*ports.Response, error) {
	return c.do(req, dst, true, nil)
}

// doRequired is used by typed operations whose contract requires a JSON
// response body. Public Do keeps its historical empty-body-is-success policy.
func (c *Client) doRequired(req *http.Request, dst any) (*ports.Response, error) {
	return c.do(req, dst, false, nil)
}

func (c *Client) doAccepted(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool) (*ports.Response, error) {
	return c.do(req, dst, allowEmpty, accepted)
}

func (c *Client) doWithTimeout(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool, timeout time.Duration) (*ports.Response, error) {
	if timeout <= 0 || c == nil || c.httpClient == nil || c.httpClient.Timeout == timeout {
		return c.do(req, dst, allowEmpty, accepted)
	}
	clone := *c
	httpClient := *c.httpClient
	httpClient.Timeout = timeout
	clone.httpClient = &httpClient
	return clone.do(req, dst, allowEmpty, accepted)
}

func (c *Client) do(req *http.Request, dst any, allowEmpty bool, accepted func(int) bool) (*ports.Response, error) {
	if c == nil {
		return nil, errors.New("forward: client is nil")
	}
	if req == nil {
		return nil, errors.New("forward: request is nil")
	}

	started := time.Now()
	metadata := MetadataFromRequest(req)
	if browserRequestNeedsCSRF(req, metadata.AuthMode) {
		if err := c.attachBrowserCSRF(req); err != nil {
			return nil, fmt.Errorf("forward: browser CSRF token: %w", err)
		}
	}
	c.emit(req.Context(), Event{Type: EventRequest, Method: req.Method, Path: req.URL.Path, Operation: metadata.Operation, AuthMode: metadata.AuthMode})
	resp, err := c.send(req)
	if err != nil {
		err = fmt.Errorf("forward: send request: %w", err)
		c.emit(req.Context(), Event{Type: EventResponse, Method: req.Method, Path: req.URL.Path, Duration: time.Since(started), Err: errors.New("forward: transport error"), Operation: metadata.Operation, AuthMode: metadata.AuthMode})
		return nil, err
	}
	defer resp.Body.Close()

	response := &ports.Response{Response: resp}
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

// failClosed returns the *UnavailableError of a client from NewUnavailable, and nil for any other client, a nil
// service or a nil client. Every exported service method that returns an error calls it first (the v0.9 hexagonal split), so a fail-closed client names its cause before any argument, principal or empty-input check. It
// is nil-safe, so a nil service still reaches its own nil check and returns that error rather than panicking.
func (s *service) failClosed() error {
	if s == nil || s.client == nil || s.client.unavailableErr == nil {
		return nil
	}
	return &ports.UnavailableError{NetworkID: s.client.networkID, Cause: s.client.unavailableErr}
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
	mode     ports.AuthMode
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
		return nil, &ports.UnavailableError{NetworkID: c.networkID, Cause: c.unavailableErr}
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
	if mode != ports.AuthModeBrowser && mode != ports.AuthModeNone {
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
	// ResolveReference removes "." and ".." path segments (RFC 3986), so an ID or name of ".." would climb out of its route: a
	// device named ".." read as the network, "." sent to the whole-network start. No Forward route contains such a segment, so a
	// path that does is refused rather than silently resolved to another route. Go does not collapse a segment that is only
	// ENCODED dots ("%2e%2e"), but a server may normalise it, so that is refused too. Segments and the scope check use the
	// escaped path, which is what goes on the wire: an ID with an escaped separator ("a%2F..%2Fb", which PathEscape produces)
	// stays one harmless segment, and "/api%2F..%2Flogin" does not pass as an /api/ path.
	escaped := rel.EscapedPath()
	for _, segment := range strings.Split(escaped, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." {
			return nil, fmt.Errorf("forward: request path %q contains a %q segment, which would resolve to a different route", escaped, segment)
		}
	}
	allowed := strings.HasPrefix(escaped, "/api/") || escaped == "/api"
	switch scope {
	case pathScopeBackup:
		allowed = escaped == "/api/backup-settings" || escaped == "/api/backup-settings/storage" || escaped == "/api/backups"
	case pathScopeBrowser:
		allowed = allowed || escaped == "/login" || escaped == "/public/csrf" ||
			strings.HasPrefix(escaped, "/saml2/authenticate/") || strings.HasPrefix(escaped, "/login/saml2/sso/")
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

func configureHTTPClient(source *http.Client, insecure bool, roots *x509.CertPool) (*http.Client, error) {
	var client http.Client
	if source == nil {
		client = http.Client{Timeout: defaultTimeout}
	} else {
		client = *source
	}
	if !insecure && roots == nil {
		return &client, nil
	}

	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		return nil, errors.New("forward: InsecureSkipVerify and RootCAs require an *http.Transport")
	}
	clone := httpTransport.Clone()
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	}
	if roots != nil {
		clone.TLSClientConfig.RootCAs = roots
	}
	if insecure {
		clone.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // Explicit opt-in for development appliances.
	}
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
