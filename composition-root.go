// Package forward is a Go client for the Forward Networks REST API.
//
// It is built in hexagonal layers. This package is the composition root: it holds Client, Config and the constructors, and wires
// the layers together. The other packages are:
//   - domain: the API's value types and the rules about them, with no I/O;
//   - ports: one interface per API family (what a Client field sends), and the values that carry a transport type;
//   - usecases: one struct per API family, embedding its port, plus the composites that combine calls (Diffs.MaterialSummary,
//     Snapshots.Collect), the generic Poller and the resumable operations;
//   - internal/adapters/secondary/rest and accessfile: the HTTP client and the access-table loaders that implement the ports.
//
// A consumer usually needs only this package, domain for the value types, and ports for errors and the transport-carrying
// values. To fake one API family in a test, implement its port, embedding the interface.
package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"crypto/x509"
	"github.com/forwardnetworks/forward-go-sdk/internal/adapters/secondary/accessfile"
	"github.com/forwardnetworks/forward-go-sdk/internal/adapters/secondary/rest"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"github.com/forwardnetworks/forward-go-sdk/usecases"
	"io"
	"net/http"
)

// Client is a Forward API client. Each field is one API family's use cases; they share one HTTP adapter client, which holds
// the transport, the credentials, the rate-limit cooldown and the capability registry. ForNetwork returns a cheap clone bound
// to another network; NewUnavailable returns one that fails closed.
//
// This file is the composition root (the v0.9 hexagonal split): the only file that imports the HTTP adapter.
type Client struct {
	rest *rest.Client

	Version                 *usecases.Version
	Networks                *usecases.Networks
	Devices                 *usecases.Devices
	ClassicDevices          *usecases.ClassicDevices
	ControllerManagedSetups *usecases.ControllerManagedSetups
	DeviceDiscovery         *usecases.DeviceDiscovery
	ConnectivityTests       *usecases.ConnectivityTests
	CustomCommandGroups     *usecases.CustomCommandGroups
	CloudManagedSetups      *usecases.CloudManagedSetups
	SaseSetups              *usecases.SaseSetups
	Decorators              *usecases.Decorators
	Trace                   *usecases.Trace
	SyntheticNodes          *usecases.SyntheticNodes
	Credentials             *usecases.Credentials
	CollectorTasks          *usecases.CollectorTasks
	CloudAccounts           *usecases.CloudAccounts
	Webhooks                *usecases.Webhooks
	Snapshots               *usecases.Snapshots
	NQE                     *usecases.NQE
	Predict                 *usecases.Predict
	AI                      *usecases.AI
	Diffs                   *usecases.Diffs
	AIAssist                *usecases.AIAssist
	Properties              *usecases.Properties
	Organizations           *usecases.Organizations
	Checks                  *usecases.Checks
	DeviceTags              *usecases.DeviceTags
	Performance             *usecases.Performance
	Capabilities            *usecases.Capabilities
	Raw                     *usecases.Raw
	Collectors              *usecases.Collectors
	Backups                 *usecases.Backups
	Browser                 *usecases.Browser
	Endpoints               *usecases.Endpoints
	Locations               *usecases.Locations
	Proxies                 *usecases.Proxies
	Users                   *usecases.Users
	Admin                   *usecases.Admin
	NQERepository           *usecases.NQERepository
	Banners                 *usecases.Banners
	Jobs                    *usecases.Jobs
	NetworkMaps             *usecases.NetworkMaps
	Routing                 *usecases.Routing
	ThreatExposure          *usecases.ThreatExposure
	EdgeAssets              *usecases.EdgeAssets
	Licensing               *usecases.Licensing
	SoftwareCentral         *usecases.SoftwareCentral
	CVEIndex                *usecases.CVEIndex
	FwdAppConnection        *usecases.FwdAppConnection
	ChangeReports           *usecases.ChangeReports
	AccessControl           *usecases.AccessControl
	SAML                    *usecases.SAML
	Configuration           *usecases.Configuration
	Integrations            *usecases.Integrations
	Topology                *usecases.Topology
	Collections             *usecases.Collections
	JumpServers             *usecases.JumpServers
	Compatibility           *usecases.Compatibility
	TrustedCertificates     *usecases.TrustedCertificates
	Vulnerabilities         *usecases.Vulnerabilities
	Aliases                 *usecases.Aliases
	WanCircuits             *usecases.WanCircuits
	DataFiles               *usecases.DataFiles
	DataConnectors          *usecases.DataConnectors
	AuditLogs               *usecases.AuditLogs
	Dashboards              *usecases.Dashboards
	Scorecards              *usecases.Scorecards
	CollectionSchedules     *usecases.CollectionSchedules
}

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
	AuthMode domain.AuthMode
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
	Capabilities domain.CapabilityProfile

	// Retry rides out transient failures. The zero value retries nothing.
	Retry RetryPolicy
}

// restConfig is cfg as the HTTP adapter takes it.
func (cfg Config) restConfig() rest.Config {
	return rest.Config{
		BaseURL:            cfg.BaseURL,
		Username:           cfg.Username,
		Password:           cfg.Password,
		APIToken:           cfg.APIToken,
		AuthMode:           cfg.AuthMode,
		Cookies:            cfg.Cookies,
		NetworkID:          cfg.NetworkID,
		UserAgent:          cfg.UserAgent,
		HTTPClient:         cfg.HTTPClient,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		RootCAs:            cfg.RootCAs,
		Hooks:              cfg.Hooks,
		Capabilities:       cfg.Capabilities,
		Retry:              cfg.Retry,
	}
}

// NewClient validates cfg and returns a configured Forward API client.
func NewClient(cfg Config) (*Client, error) {
	rc, err := rest.NewClient(cfg.restConfig())
	if err != nil {
		return nil, err
	}
	return wire(rc), nil
}

// NewUnavailable returns a fail-closed client that preserves a credential or
// configuration error until the first attempted operation. It exists for
// service seams that must always carry a client value even when per-tenant
// credential resolution failed.
func NewUnavailable(networkID string, cause error) *Client {
	return wire(rest.NewUnavailable(networkID, cause))
}

// ForNetwork returns a cheap client clone bound to networkID. The clone shares
// the HTTP transport, credentials, hooks, and capability registry; it does not
// authenticate again. Service values are rebound to the clone so an empty
// network argument on a scoped service resolves against the clone rather than
// the parent.
func (c *Client) ForNetwork(networkID string) *Client {
	rc := c.restClient()
	clone := rc.ForNetwork(networkID)
	if c != nil && clone == rc {
		return c
	}
	return wire(clone)
}

// ImpersonatedClient returns a new client acting as targetUserID, for platforms that own an organization's automation
// user and never hold a secret for it. c must be a browser-mode client (Config{AuthMode: AuthModeBrowser, Username,
// Password}) whose user may impersonate (Forward's admin impersonation route). It logs in on a private cookie jar,
// impersonates, and returns a client carrying only the impersonated session's cookies: c is not changed, and the
// returned client holds no password, so it cannot sign back in as the administrator. The session ends when Forward
// expires it; build a new client then. It does not give a token: Forward requires the user's own password to mint one,
// even on an impersonated session (see UsersService.CreateToken).
//
//	admin, _ := forward.NewClient(forward.Config{BaseURL: u, Username: "admin", Password: pw, AuthMode: forward.AuthModeBrowser})
//	as, err := admin.ImpersonatedClient(ctx, "2342")
func (c *Client) ImpersonatedClient(ctx context.Context, targetUserID string) (*Client, error) {
	rc, err := c.restClient().ImpersonatedClient(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	return wire(rc), nil
}

// BaseURL returns the origin this client was built against, as a string, for
// reports that name where a call went.
func (c *Client) BaseURL() string {
	return c.restClient().BaseURL()
}

// Do sends req and decodes a successful response into dst. If dst implements
// io.Writer, the response body is copied to it instead of JSON-decoded. The
// response body is always closed before Do returns.
func (c *Client) Do(req *http.Request, dst any) (*ports.Response, error) {
	return c.restClient().Do(req, dst)
}

// Network returns the default network ID bound to the client.
func (c *Client) Network() string {
	return c.restClient().Network()
}

// NewRequest creates an authenticated request relative to the appliance URL.
// API paths should use the same form as the REST documentation, such as
// /api/networks. Absolute URLs are rejected so credentials cannot be sent to
// another host accidentally.
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	return c.restClient().NewRequest(ctx, method, path, body)
}

// restClient is the adapter client, or nil for a nil Client, whose methods answer as a nil client always has.
func (c *Client) restClient() *rest.Client {
	if c == nil {
		return nil
	}
	return c.rest
}

// wire builds the public Client over one rest client: every family's port is the rest client's, and the use cases are built
// over them together, so a clone's composites reach the clone.
func wire(rc *rest.Client) *Client {
	f := usecases.Wire(usecases.Ports{
		Session:                 rc,
		Version:                 rc.Version,
		Networks:                rc.Networks,
		Devices:                 rc.Devices,
		ClassicDevices:          rc.ClassicDevices,
		ControllerManagedSetups: rc.ControllerManagedSetups,
		DeviceDiscovery:         rc.DeviceDiscovery,
		ConnectivityTests:       rc.ConnectivityTests,
		CustomCommandGroups:     rc.CustomCommandGroups,
		CloudManagedSetups:      rc.CloudManagedSetups,
		SaseSetups:              rc.SaseSetups,
		Decorators:              rc.Decorators,
		Trace:                   rc.Trace,
		SyntheticNodes:          rc.SyntheticNodes,
		Credentials:             rc.Credentials,
		CollectorTasks:          rc.CollectorTasks,
		CloudAccounts:           rc.CloudAccounts,
		Webhooks:                rc.Webhooks,
		Snapshots:               rc.Snapshots,
		NQE:                     rc.NQE,
		Predict:                 rc.Predict,
		AI:                      rc.AI,
		Diffs:                   rc.Diffs,
		AIAssist:                rc.AIAssist,
		Properties:              rc.Properties,
		Organizations:           rc.Organizations,
		Checks:                  rc.Checks,
		DeviceTags:              rc.DeviceTags,
		Performance:             rc.Performance,
		Capabilities:            rc.Capabilities,
		Raw:                     rc.Raw,
		Collectors:              rc.Collectors,
		Backups:                 rc.Backups,
		Browser:                 rc.Browser,
		Endpoints:               rc.Endpoints,
		Locations:               rc.Locations,
		Proxies:                 rc.Proxies,
		Users:                   rc.Users,
		Admin:                   rc.Admin,
		NQERepository:           rc.NQERepository,
		Banners:                 rc.Banners,
		Jobs:                    rc.Jobs,
		NetworkMaps:             rc.NetworkMaps,
		Routing:                 rc.Routing,
		ThreatExposure:          rc.ThreatExposure,
		EdgeAssets:              rc.EdgeAssets,
		Licensing:               rc.Licensing,
		SoftwareCentral:         rc.SoftwareCentral,
		CVEIndex:                rc.CVEIndex,
		FwdAppConnection:        rc.FwdAppConnection,
		ChangeReports:           rc.ChangeReports,
		AccessControl:           rc.AccessControl,
		SAML:                    rc.SAML,
		Configuration:           rc.Configuration,
		Integrations:            rc.Integrations,
		Topology:                rc.Topology,
		Collections:             rc.Collections,
		JumpServers:             rc.JumpServers,
		Compatibility:           rc.Compatibility,
		TrustedCertificates:     rc.TrustedCertificates,
		Vulnerabilities:         rc.Vulnerabilities,
		Aliases:                 rc.Aliases,
		WanCircuits:             rc.WanCircuits,
		DataFiles:               rc.DataFiles,
		DataConnectors:          rc.DataConnectors,
		AuditLogs:               rc.AuditLogs,
		Dashboards:              rc.Dashboards,
		Scorecards:              rc.Scorecards,
		CollectionSchedules:     rc.CollectionSchedules,
		NetworkByID:             rc.Networks,
	})
	return &Client{
		rest:                    rc,
		Version:                 f.Version,
		Networks:                f.Networks,
		Devices:                 f.Devices,
		ClassicDevices:          f.ClassicDevices,
		ControllerManagedSetups: f.ControllerManagedSetups,
		DeviceDiscovery:         f.DeviceDiscovery,
		ConnectivityTests:       f.ConnectivityTests,
		CustomCommandGroups:     f.CustomCommandGroups,
		CloudManagedSetups:      f.CloudManagedSetups,
		SaseSetups:              f.SaseSetups,
		Decorators:              f.Decorators,
		Trace:                   f.Trace,
		SyntheticNodes:          f.SyntheticNodes,
		Credentials:             f.Credentials,
		CollectorTasks:          f.CollectorTasks,
		CloudAccounts:           f.CloudAccounts,
		Webhooks:                f.Webhooks,
		Snapshots:               f.Snapshots,
		NQE:                     f.NQE,
		Predict:                 f.Predict,
		AI:                      f.AI,
		Diffs:                   f.Diffs,
		AIAssist:                f.AIAssist,
		Properties:              f.Properties,
		Organizations:           f.Organizations,
		Checks:                  f.Checks,
		DeviceTags:              f.DeviceTags,
		Performance:             f.Performance,
		Capabilities:            f.Capabilities,
		Raw:                     f.Raw,
		Collectors:              f.Collectors,
		Backups:                 f.Backups,
		Browser:                 f.Browser,
		Endpoints:               f.Endpoints,
		Locations:               f.Locations,
		Proxies:                 f.Proxies,
		Users:                   f.Users,
		Admin:                   f.Admin,
		NQERepository:           f.NQERepository,
		Banners:                 f.Banners,
		Jobs:                    f.Jobs,
		NetworkMaps:             f.NetworkMaps,
		Routing:                 f.Routing,
		ThreatExposure:          f.ThreatExposure,
		EdgeAssets:              f.EdgeAssets,
		Licensing:               f.Licensing,
		SoftwareCentral:         f.SoftwareCentral,
		CVEIndex:                f.CVEIndex,
		FwdAppConnection:        f.FwdAppConnection,
		ChangeReports:           f.ChangeReports,
		AccessControl:           f.AccessControl,
		SAML:                    f.SAML,
		Configuration:           f.Configuration,
		Integrations:            f.Integrations,
		Topology:                f.Topology,
		Collections:             f.Collections,
		JumpServers:             f.JumpServers,
		Compatibility:           f.Compatibility,
		TrustedCertificates:     f.TrustedCertificates,
		Vulnerabilities:         f.Vulnerabilities,
		Aliases:                 f.Aliases,
		WanCircuits:             f.WanCircuits,
		DataFiles:               f.DataFiles,
		DataConnectors:          f.DataConnectors,
		AuditLogs:               f.AuditLogs,
		Dashboards:              f.Dashboards,
		Scorecards:              f.Scorecards,
		CollectionSchedules:     f.CollectionSchedules,
	}
}

// The root's names for what moved to the HTTP adapter and the use cases, so a consumer's calls read as before.

// CollectionOperation retains the current state of a collection task and the
// snapshot it creates. It may be inspected concurrently while Wait is running.
type CollectionOperation = usecases.CollectionOperation

// DefaultRetryPolicy is a sensible opt-in policy for a script or service: five
// attempts, exponential backoff from one second (doubling, capped at two
// minutes) with half jitter so clients refused together do not retry together,
// and a server's Retry-After honored exactly. It retries 429 and 503 for any
// method, a failure to obtain a connection for any method, and other transport
// errors and 5xx only for idempotent methods (see RetryPolicy). A rate-limit
// lockout longer than MaxDelay is returned to the caller (RateLimitLockout)
// instead of slept through.
func DefaultRetryPolicy() RetryPolicy { return rest.DefaultRetryPolicy() }

// Event is safe request metadata for logging, tracing, metrics, and callbacks.
// It never includes authentication, query parameters, or request/response
// bodies.
type Event = rest.Event

const EventRequest = rest.EventRequest

const EventResponse = rest.EventResponse

// EventType identifies a client lifecycle event.
type EventType = rest.EventType

// Hook observes requests made by the client. Hooks run synchronously and
// should return quickly; a hook that needs blocking work should enqueue it.
type Hook = rest.Hook

// LoadRootCAs reads a PEM file of one or more CA certificates into a pool for
// Config.RootCAs. The pool holds only those certificates, so a client built
// with it trusts nothing else: that is what a Forward behind a private CA
// wants, and it is stricter than the system roots. A file with no certificate
// in it is an error rather than an empty pool that would fail every request
// with a certificate error that does not name the cause.
func LoadRootCAs(path string) (*x509.CertPool, error) { return rest.LoadRootCAs(path) }

// MetadataFromRequest returns bounded operation metadata attached by the SDK.
// It never includes resource IDs, query values, credentials, or bodies.
func MetadataFromRequest(req *http.Request) RequestMetadata { return rest.MetadataFromRequest(req) }

// NewPoller constructs an operation handle from an initial server state, a
// refresh function, and a completion predicate.
func NewPoller[T any](initial *T, poll func(context.Context) (*T, *ports.Response, error), done func(*T) (bool, error)) (*Poller[T], error) {
	return usecases.NewPoller[T](initial, poll, done)
}

// Poller retains the latest state of an asynchronous Forward operation. It is
// safe to inspect from another goroutine while Wait is polling.
type Poller[T any] = usecases.Poller[T]

// RequestMetadata is safe for both SDK hooks and an injected RoundTripper.
// A transport can read it on every attempt while SDK hooks continue to report
// the logical operation once.
type RequestMetadata = rest.RequestMetadata

// RetryPolicy makes a client ride out a transient failure instead of surfacing
// it to the caller. The zero value retries nothing, so a client that says
// nothing about retries behaves exactly as it always has.
type RetryPolicy = rest.RetryPolicy

// SnapshotOperation retains the latest state of a known snapshot.
type SnapshotOperation = usecases.SnapshotOperation

// WithIdempotent marks every request made with the returned context as safe to
// repeat, whatever its method: the caller asserts that applying it twice has
// the effect of applying it once (a PATCH that sets a field to a value, a
// PatchMany with the same patch). The retry policy then treats it like a GET or
// PUT, so a transport error after the request was written, a 502, a 504 or
// another 5xx is retried too, where it otherwise would not be because the
// outcome is unknown. It changes nothing else: it does not turn retries on
// (Config.Retry still decides), and 501 is never retried. Only claim it for a
// call that really is idempotent: a repeated POST that creates something is
// not. The mark covers every request made with the context, so do not put it
// on a composite call that also creates something (a read followed by a create).
func WithIdempotent(ctx context.Context) context.Context { return rest.WithIdempotent(ctx) }

// WithoutRetry opts the requests made with the returned context out of the
// client's retry policy: each is sent once and its first answer, 429 or 503
// included, is returned. For a write whose duplicate is worse than its failure
// (a network create, a device batch put), on a client whose policy is set for
// the reads. The policy treats a 503 as "turned away unprocessed", but that is
// usual, not proven: Spring answers 503 when an asynchronous handler's timeout
// fires (AsyncRequestTimeoutException; WebMvcConfig sets a 10s default), which
// can be after the work started (cloud discovery and the CAP connectivity test
// are asynchronous writes), and a proxy in front can answer one too. It wins
// over WithIdempotent if a context carries both. The mark covers every request
// made with the context, so do not put it on a composite call whose follow-up
// reads (polling an import) should keep their retries. The client's shared
// rate-limit cooldown still applies, so a call does not send into a lockout
// another request already met.
func WithoutRetry(ctx context.Context) context.Context { return rest.WithoutRetry(ctx) }

// Config's own vocabulary stays nameable from the root, so a Config reads as before: the principal modes and the capability
// profile it takes. They are domain's (and ports re-exports them for the adapters).

// AuthMode is the principal a client acts as; the zero value is the per-user credential.
type AuthMode = domain.AuthMode

// The principal modes Config.AuthMode takes.
const (
	AuthModeUser      = domain.AuthModeUser
	AuthModeService   = domain.AuthModeService
	AuthModeCollector = domain.AuthModeCollector
	AuthModeBrowser   = domain.AuthModeBrowser
	AuthModeNone      = domain.AuthModeNone
)

// CapabilityProfile is what Config.Capabilities declares about the target appliance build.
type CapabilityProfile = domain.CapabilityProfile

// The access-table loaders read files and the environment, so they live in the file-system adapter.

// LoadAccessTable parses the table from a Forward source checkout: root is the checkout, or the access package directory itself.
// An error means the path is not a Forward checkout of a layout this parser knows, never an empty table.
func LoadAccessTable(root string) (*domain.AccessTable, error) {
	return accessfile.LoadAccessTable(root)
}

// LoadAccessTableJSON reads a copy made by WriteJSON.
func LoadAccessTableJSON(path string) (*domain.AccessTable, error) {
	return accessfile.LoadAccessTableJSON(path)
}

// AccessTableFromEnv loads the table from FORWARD_RBAC_TABLE (a JSON copy) if set, else from FORWARD_SOURCE (a Forward checkout) if
// set. It returns (nil, nil) when neither is set, which the checker treats as "every answer unknown"; a variable that is set to a
// wrong path is an error, never a silent fallback to no table.
func AccessTableFromEnv() (*domain.AccessTable, error) { return accessfile.AccessTableFromEnv() }

// Each rest family implements its port, and the rest client is the use cases' session; a renamed or removed method fails
// the build here.
var (
	_ usecases.Session              = (*rest.Client)(nil)
	_ ports.Version                 = (*rest.Version)(nil)
	_ ports.Networks                = (*rest.Networks)(nil)
	_ ports.Devices                 = (*rest.Devices)(nil)
	_ ports.ClassicDevices          = (*rest.ClassicDevices)(nil)
	_ ports.ControllerManagedSetups = (*rest.ControllerManagedSetups)(nil)
	_ ports.DeviceDiscovery         = (*rest.DeviceDiscovery)(nil)
	_ ports.ConnectivityTests       = (*rest.ConnectivityTests)(nil)
	_ ports.CustomCommandGroups     = (*rest.CustomCommandGroups)(nil)
	_ ports.CloudManagedSetups      = (*rest.CloudManagedSetups)(nil)
	_ ports.SaseSetups              = (*rest.SaseSetups)(nil)
	_ ports.Decorators              = (*rest.Decorators)(nil)
	_ ports.Trace                   = (*rest.Trace)(nil)
	_ ports.SyntheticNodes          = (*rest.SyntheticNodes)(nil)
	_ ports.Credentials             = (*rest.Credentials)(nil)
	_ ports.CollectorTasks          = (*rest.CollectorTasks)(nil)
	_ ports.CloudAccounts           = (*rest.CloudAccounts)(nil)
	_ ports.Webhooks                = (*rest.Webhooks)(nil)
	_ ports.Snapshots               = (*rest.Snapshots)(nil)
	_ ports.NQE                     = (*rest.NQE)(nil)
	_ ports.Predict                 = (*rest.Predict)(nil)
	_ ports.AI                      = (*rest.AI)(nil)
	_ ports.Diffs                   = (*rest.Diffs)(nil)
	_ ports.AIAssist                = (*rest.AIAssist)(nil)
	_ ports.Properties              = (*rest.Properties)(nil)
	_ ports.Organizations           = (*rest.Organizations)(nil)
	_ ports.Checks                  = (*rest.Checks)(nil)
	_ ports.DeviceTags              = (*rest.DeviceTags)(nil)
	_ ports.Performance             = (*rest.Performance)(nil)
	_ ports.Capabilities            = (*rest.Capabilities)(nil)
	_ ports.Raw                     = (*rest.Raw)(nil)
	_ ports.Collectors              = (*rest.Collectors)(nil)
	_ ports.Backups                 = (*rest.Backups)(nil)
	_ ports.Browser                 = (*rest.Browser)(nil)
	_ ports.Endpoints               = (*rest.Endpoints)(nil)
	_ ports.Locations               = (*rest.Locations)(nil)
	_ ports.Proxies                 = (*rest.Proxies)(nil)
	_ ports.Users                   = (*rest.Users)(nil)
	_ ports.Admin                   = (*rest.Admin)(nil)
	_ ports.NQERepository           = (*rest.NQERepository)(nil)
	_ ports.Banners                 = (*rest.Banners)(nil)
	_ ports.Jobs                    = (*rest.Jobs)(nil)
	_ ports.NetworkMaps             = (*rest.NetworkMaps)(nil)
	_ ports.Routing                 = (*rest.Routing)(nil)
	_ ports.ThreatExposure          = (*rest.ThreatExposure)(nil)
	_ ports.EdgeAssets              = (*rest.EdgeAssets)(nil)
	_ ports.Licensing               = (*rest.Licensing)(nil)
	_ ports.SoftwareCentral         = (*rest.SoftwareCentral)(nil)
	_ ports.CVEIndex                = (*rest.CVEIndex)(nil)
	_ ports.FwdAppConnection        = (*rest.FwdAppConnection)(nil)
	_ ports.ChangeReports           = (*rest.ChangeReports)(nil)
	_ ports.AccessControl           = (*rest.AccessControl)(nil)
	_ ports.SAML                    = (*rest.SAML)(nil)
	_ ports.Configuration           = (*rest.Configuration)(nil)
	_ ports.Integrations            = (*rest.Integrations)(nil)
	_ ports.Topology                = (*rest.Topology)(nil)
	_ ports.Collections             = (*rest.Collections)(nil)
	_ ports.JumpServers             = (*rest.JumpServers)(nil)
	_ ports.Compatibility           = (*rest.Compatibility)(nil)
	_ ports.TrustedCertificates     = (*rest.TrustedCertificates)(nil)
	_ ports.Vulnerabilities         = (*rest.Vulnerabilities)(nil)
	_ ports.Aliases                 = (*rest.Aliases)(nil)
	_ ports.WanCircuits             = (*rest.WanCircuits)(nil)
	_ ports.DataFiles               = (*rest.DataFiles)(nil)
	_ ports.DataConnectorsAPI       = (*rest.DataConnectors)(nil)
	_ ports.AuditLogsAPI            = (*rest.AuditLogs)(nil)
	_ ports.Dashboards              = (*rest.Dashboards)(nil)
	_ ports.Scorecards              = (*rest.Scorecards)(nil)
	_ ports.CollectionSchedules     = (*rest.CollectionSchedules)(nil)
)
