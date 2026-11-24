package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
)

// Session is what a use case asks of the client it runs on: the network it is bound to and the principal it acts as
// (ports.Session), and whether it is a fail-closed client from NewUnavailable. The HTTP adapter's client implements it.
type Session interface {
	ports.Session
	Unavailable() error
}

// networkByID is the direct read of one network, GET /api/networks/{id}, which Networks.Get falls back from to the network
// listing on a build that does not serve it. It is declared here, where it is used, so the port names only what Client
// exposes and nothing inside the HTTP adapter calls its own List.
type networkByID interface {
	GetByID(ctx context.Context, networkID string) (r0 *domain.Network, r1 *ports.Response, err error)
}

// Version accesses Forward API version information.
type Version struct {
	ports.Version
	session Session
}

// Networks manages Forward networks.
type Networks struct {
	ports.Networks
	session Session
	byID    networkByID
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Networks reaches its own nil check.
func (s *Networks) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Devices inspects modeled devices and their collected files.
type Devices struct {
	ports.Devices
	session Session
}

// ClassicDevices manages device collection sources.
type ClassicDevices struct {
	ports.ClassicDevices
	session     Session
	credentials *Credentials
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil ClassicDevices reaches its own nil check.
func (s *ClassicDevices) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// ControllerManagedSetups manages SD-WAN controller-managed setups:
// the record that tells Forward a controller (vManage, vBond, vSmart) owns a
// set of devices, rather than each device being collected classically.
//
// Published in Forward's OpenAPI (controller-managed-setups.yaml) but tagged Experimental: the contract may change without
// deprecation.
type ControllerManagedSetups struct {
	ports.ControllerManagedSetups
	session Session
}

// DeviceDiscovery starts and reads Forward's device discovery: a collector scans subnets, follows seed devices'
// neighbours, or reads cloud setups, and Forward lists what it found so it can be added as classic devices. Discovery
// finds devices; it adds nothing, and Start scans a customer network from the collector, so call it deliberately.
//
// Routes are in DeviceDiscoveryController and ScheduledDiscoveryController (published OpenAPI is not consulted for them;
// treat them as preview).
type DeviceDiscovery struct {
	ports.DeviceDiscovery
	session Session
}

// ConnectivityTests runs and reads the connectivity tests of a network's classic devices: the test a collector runs
// to reach a device, log in, discover its type and query it (the UI's "Test" button on the Classic Devices page). Controller-
// managed setups have their own, ControllerManagedSetups.Test.
//
// A test is a probe: the network's collector connects to the device and logs in with its credentials, so every call here
// touches the customer's devices, and repeated logins with a bad credential can lock an account. Start and BulkStart queue the
// tests and return at once (204); the outcome appears later in List and Get, replacing the previous result for the device.
// Forward keeps the last result until then, so compare a result's SavedAt/EndTime with the time you queued the test (use the
// response's Date header rather than your own clock to avoid skew).
//
// Limits and requirements, from ClientController and ConnectivityService:
//   - Device limit: this SDK caps one BulkStart call at MaxConnectivityTestDevices (100) names and refuses more before sending
//     (BulkStop has no cap: stopping is the brake); Forward itself has no per-call limit. Names must match Forward's device name
//     rule. A name that is not a classic device of the network (an unknown one, or an endpoint) is a 400 "Devices not found in
//     this network" for the whole call, queueing nothing, not a 404.
//   - Rate limit: none specific to these routes. Forward queues one task per device on the collector, which runs them within its
//     own concurrency setting (default 128 devices at once, shared with every network on that collector and with collections),
//     one test per device at a time, and within a jump server's session limit, so a large call is mostly a burst of logins. Only
//     the per-user API rate limiter applies (off unless the deployment enables API_RATE_LIMITING).
//   - Collector id: none is passed. Each device is tested through the collector it is pinned to (ClassicDevice.CollectorID), else
//     the network's default collector. A 400 comes back if the network has no default collector (even when every device is
//     pinned) or a collector needs an update; a pinned collector that no longer exists falls back to the default; an offline
//     collector is not refused, the tests just wait.
//   - Permission: Start, BulkStart, Stop and BulkStop need the network operation TEST_COLLECTION_SOURCES; List and Get need
//     VIEW_NETWORK_AND_SNAPSHOTS. A refusal is the typed 403 (ErrPermissionDenied).
//
// The whole-network start and stop routes (POST ...connectivityTests/start and /stop, no device named) are deliberately not
// wrapped: they test every classic device in the network. The endpoint variants (?type=endpoint) are not wrapped either.
// Preview: ClientController, unpublished.
type ConnectivityTests struct {
	ports.ConnectivityTests
	session Session
}

// CustomCommandGroups manages a network's custom collection groups: extra CLI commands (or SNMP OIDs) the collector runs
// on devices of chosen types in addition to Forward's own collection (the UI's Custom Commands page). A command here is NOT one
// of Forward's built-in collection commands. The organization-wide disabledCommands setting (OrgCollectionSettingsPatch) is
// matched against every command sent through an INTERACTIVE CLI session on a device whose software version the collector could
// read, built-in or custom (CliDevice.setupDisabledCommands hands the entries for the device's connection type and version
// range to the agent; InteractiveCliHelper.runCommandAndGetOutcome checks each command, each part of a ":::" sequence, as a
// full-string regular expression match on the lowercased command, the pattern lowercased too). A custom command that matches is
// silently skipped: its "Command:" line is written to the output with nothing under it and the snapshot and API show no error
// (the collector only logs one). Non-interactive SSH agents and endpoint profiles ignore the setting, and SNMP groups are
// unaffected.
//
// Routes are in CustomCommandGroupController (ClientController for the type list); preview, unpublished. Permissions: List needs
// the network operation VIEW_CUSTOM_COLLECTION_GROUPS; Create, Update, Delete and Validate need EDIT_CUSTOM_COLLECTION_GROUPS. A
// refusal is the typed 403 (ErrPermissionDenied). A group's commands are NOT checked against the approved-CLI-commands list
// (Endpoints.ApprovedCLICommands, which governs endpoint profiles only); they are checked per device type against Forward's own
// allowed and banned command patterns (CustomCliConfig, CustomCommandGroupUtils), so Create and Update answer 400 for a command a
// type does not allow (among others, ";", "!" and line breaks are refused for most types); Validate asks the same question
// without writing.
type CustomCommandGroups struct {
	ports.CustomCommandGroups
	session Session
}

// CloudManagedSetups manages CLOUD-managed setups: the record that
// tells Forward a SaaS wireless controller owns a set of access points, which
// Forward then collects through the vendor's cloud API rather than device by
// device. That is Juniper Mist (type=MIST) and Meraki (type=MERAKI); VeloCloud
// (type=VELO_CLOUD) is not covered here.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/CloudManagedSetupController.java (add/discover/test/
// delete, all gated by org property MIST_ONBOARDING, default true) and
// web/.../json/mist/CreateMistSetup.java for the request shape.
type CloudManagedSetups struct {
	ports.CloudManagedSetups
	session Session
}

// SaseSetups manages SASE collection sources: today only Zscaler
// (type=ZSCALER), which collects the ZPA tenant through its API and, per App
// Connector, over SSH. A setup is the tenant plus its connectors; Forward
// names the connectors' devices after them.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/SaseSetupController.java (identical route set at both
// pinned builds), cv/sources/sase/ZscalerSetup.java, AppConnectorSetup.java,
// ZscalerSetupPatch.java, ZscalerTestResult.java and ZscalerConnectivityTestRequest.java.
// Unverified live: nothing here has been run against a tenant.
type SaseSetups struct {
	ports.SaseSetups
	session Session
}

// Decorators manages NQE entity decorators: a saved NQE query whose
// rows are attached to devices, interfaces, hosts or paths, so that the
// attributes it computes show up next to the entity. A decorator is an org
// object (not per network); Decorate reads what its query attaches to
// entities of one snapshot.
//
// Preview: these routes are not in the published OpenAPI set. Verified against
// web/.../controller/EntityDecoratorController.java (the same five mappings at
// both pinned builds), cv/decorator/{EntityDecorator,EntityDecorators,
// EntityDecoratorService,DecoratorDefinition,EntityDecoration,DecorationStatus}.java,
// web/.../json/decorator/*Patch.java and the search worker's EntityIndexComputer.
// Unverified live: nothing here has been run against a deployment.
type Decorators struct {
	ports.Decorators
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Decorators reaches its own nil check.
func (s *Decorators) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Trace is the Trace family's use cases. Its port has a method named Trace, which an embedded field of that name would
// hide, so it holds the port in a field and forwards every port method (the v0.9 hexagonal split).
type Trace struct {
	port    ports.Trace
	session Session
}

// Facets is the port's.
func (s *Trace) Facets(ctx context.Context, networkID string, snapshotID string, q ports.TraceQuery, opts ports.TraceFacetsOptions) (*ports.TraceFacets, *ports.Response, error) {
	return s.port.Facets(ctx, networkID, snapshotID, q, opts)
}

// FacetsQuery is the port's.
func (s *Trace) FacetsQuery(ctx context.Context, networkID string, snapshotID string, q ports.TracePathQuery, opts ports.TraceFacetsOptions) (*ports.TraceFacets, *ports.Response, error) {
	return s.port.FacetsQuery(ctx, networkID, snapshotID, q, opts)
}

// Trace is the port's.
func (s *Trace) Trace(ctx context.Context, networkID string, snapshotID string, q ports.TraceQuery, sel ports.TraceSelection) (*ports.TraceResult, *ports.Response, error) {
	return s.port.Trace(ctx, networkID, snapshotID, q, sel)
}

// TraceFor is the port's.
func (s *Trace) TraceFor(ctx context.Context, networkID string, snapshotID string, q ports.TracePathQuery, sel ports.TraceSelection) (*ports.TraceResult, *ports.Response, error) {
	return s.port.TraceFor(ctx, networkID, snapshotID, q, sel)
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Trace reaches its own nil check.
func (s *Trace) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// SyntheticNodes manages the synthetic devices a network can carry:
// the single internet node, intranet nodes, and L3 VPNs. They are not
// collected from real equipment -- they stand in for the parts of the world
// outside the modelled network, and Skyforge derives them from the topology.
//
// Preview: these routes are not in the published OpenAPI set.
type SyntheticNodes struct {
	ports.SyntheticNodes
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil SyntheticNodes reaches its own nil check.
func (s *SyntheticNodes) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Credentials manages collector-side device credentials. Forward
// substitutes opaque password identifiers in read responses.
type Credentials struct {
	ports.Credentials
	session Session
}

// CollectorTasks starts, observes, and stops collection work.
type CollectorTasks struct {
	ports.CollectorTasks
	session   Session
	snapshots *Snapshots
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil CollectorTasks reaches its own nil check.
func (s *CollectorTasks) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// CloudAccounts manages cloud collection sources.
//
// Preview: cloud account models and routes evolve independently across
// Forward versions and are not part of the published OpenAPI set.
type CloudAccounts struct {
	ports.CloudAccounts
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil CloudAccounts reaches its own nil check.
func (s *CloudAccounts) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Webhooks manages organization-scoped outbound callbacks.
//
// Preview: webhook schemas are unpublished and deployment capability-gated.
type Webhooks struct {
	ports.Webhooks
	session Session
}

// Snapshots lists, uploads, and downloads Forward snapshots.
type Snapshots struct {
	ports.Snapshots
	session        Session
	collectorTasks *CollectorTasks
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Snapshots reaches its own nil check.
func (s *Snapshots) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// NQE runs Network Query Engine queries and diffs.
type NQE struct {
	ports.NQE
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil NQE reaches its own nil check.
func (s *NQE) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Predict accesses Forward Predict and change-set APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// may change between Forward releases. The SDK will evolve them during v0.x.
type Predict struct {
	ports.Predict
	session        Session
	classicDevices *ClassicDevices
	properties     *Properties
	snapshots      *Snapshots
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Predict reaches its own nil check.
func (s *Predict) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// AI accesses Forward's snapshot-grounded AI chat APIs.
//
// Preview: these endpoints are not part of the published OpenAPI contract and
// are gated by the AI_ALLOWED and AI_CHATS organization properties.
type AI struct {
	ports.AI
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil AI reaches its own nil check.
func (s *AI) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Diffs wraps Forward's snapshot-to-snapshot diff family,
// GET /api/diffs/{snapshotAId}/{snapshotBId}/... (appserver DiffController).
// A is the OLDER snapshot, B the NEWER one. The cheap `?count` variants answer
// "did anything material change?" without pulling the diff bodies; the
// summary/detail variants follow the same paths without `count`.
type Diffs struct {
	ports.Diffs
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Diffs reaches its own nil check.
func (s *Diffs) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// AIAssist accesses Forward's NQE and Predict assistance APIs.
//
// Preview: these endpoints are unpublished and controlled by AI_ALLOWED and,
// for Predict assistance, PREDICT_AI_ASSIST and PREDICT_MODELING (and PREDICT_FAILURE_SUMMARY_ASSIST for the failure summary).
type AIAssist struct {
	ports.AIAssist
	session Session
}

// Properties accesses Forward organization and global properties.
//
// Preview: this configuration surface is unpublished, permission-sensitive,
// and expands as Forward features are developed.
type Properties struct {
	ports.Properties
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Properties reaches its own nil check.
func (s *Properties) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Organizations manages the current organization and the unpublished
// support/admin organization surface.
//
// Preview: /api/admin/orgs is permission-sensitive and version-dependent.
type Organizations struct {
	ports.Organizations
	session Session
	admin   *Admin
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Organizations reaches its own nil check.
func (s *Organizations) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Checks manages checks attached to snapshots. Checks are not created on
// a network resource: they attach to one real snapshot. When created with
// CreatePersistent, Forward carries them onto later real and predicted snapshots
// of that network.
type Checks struct {
	ports.Checks
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Checks reaches its own nil check.
func (s *Checks) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// DeviceTags manages network-scoped device tag definitions and
// assignments.
type DeviceTags struct {
	ports.DeviceTags
	session Session
}

// Performance uploads collector performance payloads. This endpoint
// uses collector credentials, supplied per call rather than replacing the API
// credentials on the shared client.
type Performance struct {
	ports.Performance
	session        Session
	classicDevices *ClassicDevices
	credentials    *Credentials
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Performance reaches its own nil check.
func (s *Performance) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Capabilities exposes the configured profile plus runtime evidence.
type Capabilities struct {
	ports.Capabilities
	session Session
}

// Raw provides a forward-compatible binding for every /api route,
// including endpoints and fields not yet represented by a typed service.
type Raw struct {
	ports.Raw
	session Session
}

// Collectors manages collector registration and network attachment.
type Collectors struct {
	ports.Collectors
	session Session
}

// Backups implements Forward CBR endpoints
// (ClusterBackupRestoreController). They are ordinary /api routes: the
// controller has no servlet of its own and Forward's DispatcherServlet is
// registered at /api/*. This service used to send them to the appliance root
// (/backup-settings, /backups), which Forward never served -- verified against
// ServletInitializer.getServletMappings at primary 15398425a69 and stable
// 67e89c87124.
//
// Every route here needs the ADMINISTER_SYSTEM system permission, not a particular kind of principal: a person's
// login that holds it works, and one that lacks it gets a typed 403. The controller exists only on on-prem
// Kubernetes deployments; elsewhere the routes answer ErrEndpointNotServed.
type Backups struct {
	ports.Backups
	session Session
}

// Browser implements Forward's cookie/CSRF session flow on the same
// Client transport and hook seam as the JSON API services.
type Browser struct {
	ports.Browser
	session Session
}

// Endpoints is the Endpoints family's use cases: its port, and the composites built on it.
type Endpoints struct {
	ports.Endpoints
	session Session
}

// Locations is the Locations family's use cases: its port, and the composites built on it.
type Locations struct {
	ports.Locations
	session Session
}

// Proxies is the Proxies family's use cases: its port, and the composites built on it.
type Proxies struct {
	ports.Proxies
	session Session
}

// Users is the Users family's use cases: its port, and the composites built on it.
type Users struct {
	ports.Users
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil Users reaches its own nil check.
func (s *Users) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Admin is the Admin family's use cases: its port, and the composites built on it.
type Admin struct {
	ports.Admin
	session Session
}

// NQERepository is the NQERepository family's use cases: its port, and the composites built on it.
type NQERepository struct {
	ports.NQERepository
	session Session
}

// Banners is the Banners family's use cases: its port, and the composites built on it.
type Banners struct {
	ports.Banners
	session Session
}

// Jobs reports and cancels top-level backend jobs (NQE executions, snapshot processing, device-processing
// batches and similar). This is an org-admin surface (ADMINISTER_SYSTEM, granted to OrgRole.ADMIN), not the
// customer-facing API: Forward's own GUI (Settings > System Overview > Jobs) and support engineers use it to find
// and clear a wedged execution instead of restarting a worker.
type Jobs struct {
	ports.Jobs
	session Session
}

// NetworkMaps reads a network's saved network maps: the named sets of devices, with positions and display settings, that the
// Network Maps page draws. READ-ONLY: this service does not create, change or delete a map or a directory. Preview: appserver
// NetworkMapController, not the published OpenAPI. Every read needs the network permission VIEW_NETWORK_MAPS; a caller without it gets
// the typed 403 (ErrPermissionDenied), and the directory listing is also switched by the organization property
// NETWORK_MAP_DIRECTORIES (default on; off answers 403 with ErrFeatureGated, checked before the permission). A licence that lacks the operation answers ErrUnlicensedOperation instead of ErrPermissionDenied.
//
// A network with no maps is not an error: the listing is an empty (non-nil) slice, and the directory listing still holds the root
// directory "/" with no map ids.
type NetworkMaps struct {
	ports.NetworkMaps
	session Session
}

// Routing reads the routing-protocol model Forward builds for a snapshot: where a prefix originates, which devices install,
// filter or lose it, the hop-by-hop propagation, the policies applied at each hop, and what each device has installed. It is the
// "routing app" behind Forward's BGP prefix search. Preview: appserver PredictController, not the published OpenAPI. Every route needs
// the NetworkOperation BGP_PREFIX_SEARCH (named RP_PREFIX_SEARCH in newer Forward), which only the network role ADMIN holds (an
// OPERATOR gets 403), the organization property PREDICT_MODELING, and a snapshot whose processing has reached the creation stage.
//
// The routes are the bgp-prefix-* spellings (bgp-prefix-search, -trace, -details, devices/{d}?view=bgp-prefix-info and
// bgp-installed-prefixes). Forward's stable line (26.11.2-01, 6bc6d4fbb06) still serves them, but its 1.0.0 primary line removed
// bgp-prefix-search, -trace and -details from 1.0.0-261118 (46993a8f8c9): there PrefixSearch, PrefixTrace and PrefixDetails answer
// 404 "No endpoint" (ErrEndpointNotServed), and RPPrefixSearch, RPPrefixTrace and RPPrefixDetails (routing_rp.go) are the
// replacements, which also trace OSPF, IS_IS, OMP and CONFIG nodes. The rp-* request bodies at both pinned builds are the ones
// those methods send; the older primary 5f7e4ec1841 read older bodies. Forward master's bgp-* routes take the NEW bodies too (a
// protocol on every router and origin, node keys), so the methods here fail on master with a router, origin or node: they are for
// the older builds only. Use the RP* methods against a 1.0.0 line.
type Routing struct {
	ports.Routing
	session Session
}

// ThreatExposure reads and manages threat-exposure analyses: which of a network's critical assets an attacker could reach through
// chosen exposure points (interface aliases such as an internet-facing edge), and how severely. A definition names the exposure points
// with a severity each and optionally narrows the critical assets by location, criticality and subnet; evaluating it against a snapshot
// starts analysis jobs, and the analysis views read their results. Preview and EXPERIMENTAL: the feature is behind the organization
// property THREAT_EXPOSURE (default off, not configurable by an org admin), the routes are appserver ThreatExposureController, and
// they are in the Forward source at both builds this SDK pins, but a deployment that predates them answers 404 (ErrEndpointNotServed). Reads need
// VIEW_THREAT_EXPOSURE (listing, getting, status and the views are not gated on the property); creating, changing and deleting
// definitions EDIT_THREAT_EXPOSURE; Evaluate EVALUATE_THREAT_EXPOSURE. These operations need the security licence facet.
type ThreatExposure struct {
	ports.ThreatExposure
	session Session
}

// EdgeAssets reads and manages a network's edge assets: hosts a user added that are not collected from, whose identifiers
// (addresses, MAC, cloud host id) are matched to the hosts of later snapshots, and the criticality that marks assets (edge assets and
// endpoints) as business critical. Threat-exposure analyses pick their critical assets from these. Preview and EXPERIMENTAL: appserver
// EdgeAssetController and DeviceController, not the published OpenAPI; a deployment without them answers 404 (ErrEndpointNotServed).
// Forward adds edge assets from a snapshot's inferred hosts (AddInferredHosts). The operations here need the security licence facet.
type EdgeAssets struct {
	ports.EdgeAssets
	session Session
}

// Licensing is the on-prem licensing surface.
//
// SCOPE, AND WHY IT IS SPLIT. Forward serves licensing from TWO controllers
// with different authority, and the split is the whole reason a caller has to
// think about credentials:
//
//   - OnPremLicenseController -- @Requires(org = MANAGE_LICENSING). An ORG
//     admin acting on THEIR OWN org: read the instance fingerprint, decode a
//     key to preview it, apply a key. The org id is taken from the session, NOT
//     from the path, so these can only ever touch the caller's own org.
//   - ForwardSupportLicenseController -- @RequiresForwardAdmin /
//     @RequiresForwardSupport. A PLATFORM operator acting on a NAMED org, id in
//     the path: list, remove, invalidate.
//
// So "apply" and "remove" are not symmetric. Applying is something an org can
// do to itself; removing is something only the platform can do to an org it
// names. A caller that has one credential does not automatically have the
// other, and the remove methods take an orgID precisely because the server
// does.
//
// The `/api` prefix is the servlet prefix every other service in this client
// uses; the controllers themselves map bare paths (verified against
// OnPremLicenseController.getFingerprint -> "/vm/instanceId" and
// CustomBannerController -> "/custom-banners", which this client reaches as
// "/api/custom-banners").
type Licensing struct {
	ports.Licensing
	session Session
}

// SoftwareCentral is the Forward Software Central surface: the catalog
// of installable and upgradable Forward software (appliance OVAs, upgrade
// bundles, air-gap packages) that Forward publishes from fwd.app.
//
// WHERE THIS LIVES, AND WHY IT IS NOT THE APPLIANCE. Every other service in
// this client talks to a Forward APPSERVER -- a customer's own instance.
// Software Central is served by DeploymentController under @Profile(SAAS):
// it only exists on Forward's hosted fwd.app, it authenticates a Software
// Central account with the same HTTP basic auth as everything else, and it
// refuses (403, "SOFTWARE_CENTRAL is not enabled for organization N") when
// the org holds no Software Central entitlement. Construct the client against
// https://fwd.app with the Software Central credential, not against an
// on-prem base URL; an on-prem appserver 404s these routes.
//
// TWO WAYS TO FETCH, ONE OF THEM CLOSED. The artifact route answers in two
// forms:
//
//   - GET /api/deployment-artifacts/{id}?versionId=V  -> 302 to the file.
//   - GET /api/deployment-artifacts/{id}?versionId=V&as=url -> {"url": "..."}.
//
// The 302 lands on a presigned object-store URL on ANOTHER HOST. This client's
// redirect policy (secureRedirects in client.go) deliberately refuses to
// follow cross-origin redirects so a Forward credential can never be replayed
// to a third party, which means the 302 form surfaces here as an API error
// carrying the 3xx. This service therefore only ever asks for `as=url` and
// fetches the returned URL in a separate, UNAUTHENTICATED request
// (DownloadDeploymentArtifact). Do not "fix" this by relaxing the redirect
// policy; the policy is the point.
//
// Unauthenticated calls come back as HTTP 401 with an EMPTY body (verified
// against fwd.app 2026-09-11). newErrorResponse classifies a 401 as
// ErrAuthentication regardless of body, so errors.Is(err, ErrAuthentication)
// holds even though there is no message to read.
type SoftwareCentral struct {
	ports.SoftwareCentral
	session Session
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil SoftwareCentral reaches its own nil check.
func (s *SoftwareCentral) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// CVEIndex is the appliance's CVE index -- the vulnerability catalogue
// device-vulnerability matching reads -- and the same route on fwd.app where
// Forward publishes it. One route, `/api/cve-index`, two sides:
//
//   - On fwd.app (a Software Central account, CveIndexCloudController under
//     @Profile(SAAS)): GET returns the gzipped index. Construct the client
//     against https://fwd.app with the Software Central credential, exactly
//     as SoftwareCentralService documents.
//   - On an on-prem appserver: GET ?view=metadata answers the digest and
//     upload provenance of the index it is serving; PUT ?sha=<sha256> installs
//     a gzipped index; DELETE reverts to the index bundled in the image. PUT
//     and DELETE require a Forward admin; metadata requires the
//     SOFTWARE_CENTRAL org property or a Forward admin.
//
// THE INDEX IS INSTANCE-WIDE. putCveIndex takes no orgId: every org on an
// appserver reads the same index, so an installer uploads once per instance,
// never per org. (Only Forward's OWN hourly updater fetches per org, because
// the entitlement it presents to fwd.app hangs off the org's license; that
// updater answers 403 "License authentication failed" on self-hosted
// deployments whose orgs hold no such license, which is why this surface
// exists.)
//
// THE DIGEST IS OF THE GZIP BYTES. `digest` in the metadata and `sha` on the
// PUT are sha256 over the compressed body as transferred -- verified against
// a real upload -- so a caller can compare what it fetched to what an
// instance serves without decompressing either.
type CVEIndex struct {
	ports.CVEIndex
	session Session
}

// FwdAppConnection tests and reads an ON-PREMISES deployment's connection to fwd.app (Software Central), which the CVE index
// updater and software downloads use. FwdAppConnectionController, ProfileCriteria ON_PREM: a SaaS deployment does not map these routes, so
// the answer there is the 404 ErrEndpointNotServed. Preview: not in the published OpenAPI; the shapes are from Forward's source and were not
// run live by this SDK.
type FwdAppConnection struct {
	ports.FwdAppConnection
	session Session
}

// ChangeReports builds a ChangeReport by reading Forward. The report's shape and assembly rules are shared with forward-python-sdk
// (testdata/change_report/RULES.md); this file is the Go side of the reads, which follow the "Reads" rules there: one existing read per section,
// sequential, each with its own time budget, and a section that cannot be read becomes an unavailable section with a typed reason, never a failed
// report and never a silent "no change". Preview: composed only from reads this SDK already has; nothing here was run against a live Forward.
type ChangeReports struct {
	ports.ChangeReports
	session   Session
	diffs     *Diffs
	nqe       *NQE
	predict   *Predict
	snapshots *Snapshots
	trace     *Trace
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil ChangeReports reaches its own nil check.
func (s *ChangeReports) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// AccessControl covers device access labels and access control groups.
//
// These routes post-date the Skyforge inventory this SDK was first cut from;
// Skyforge's own client grew them in September 2026 and the SDK did not, which
// left ACG reconciliation as the last hand-rolled Forward surface. The
// semantics below were carried over from that client because two of them are
// easy to get wrong and one of them was, in production.
type AccessControl struct {
	ports.AccessControl
	session Session
}

// SAML reads and writes the appliance's SAML settings.
//
// Forward keys SAML by a per-registration CustomName -- the registration id
// -- which is what lets SamlAuthManager resolve an org and do per-org ACG
// lookups. LDAP has no such key and is pinned platform-wide to one org,
// which is why Skyforge federates every org over SAML and never LDAP.
type SAML struct {
	ports.SAML
	session Session
}

// Configuration is the Configuration family's use cases: its port, and the composites built on it.
type Configuration struct {
	ports.Configuration
	session Session
}

// Integrations is the Integrations family's use cases: its port, and the composites built on it.
type Integrations struct {
	ports.Integrations
	session Session
}

// Topology is the Topology family's use cases: its port, and the composites built on it.
type Topology struct {
	ports.Topology
	session Session
}

// Collections reads per-device collection status.
//
// It also carried List (GET /networks/{id}/collections) and Progress
// (GET /networks/{id}/collectionProgress). Neither route exists on the
// primary 15398425a69 or stable 67e89c87124 builds; both 404'd for every
// network. Collection progress is CollectorTasks.List / CollectorTasks.Progress
// (GET /api/collector-tasks).
type Collections struct {
	ports.Collections
	session Session
}

// JumpServers is the JumpServers family's use cases: its port, and the composites built on it.
type JumpServers struct {
	ports.JumpServers
	session Session
}

// Compatibility exposes operation-specific typed passthroughs for
// facades whose public contract is the upstream status/headers/body.
// It is intentionally not an arbitrary Raw transport.
type Compatibility struct {
	ports.Compatibility
	session Session
}

// TrustedCertificates manages org-wide trusted certificates and
// dispatches the per-collector task that imports them into each collector's
// Java truststore.
//
// Real appserver routes (verified against forward/fwd @
// web/src/main/java/com/forwardnetworks/cv/web/controller/TrustedCertificateController.java):
//
//	GET    /api/trusted-certificates               list (getCertificates)
//	POST   /api/trusted-certificates               add one (addCertificate)
//	POST   /api/trusted-certificates?action=apply  dispatch ApplyCertificatesTask
//	                                                to every collector that
//	                                                supports it (applyCertificates)
//	DELETE /api/trusted-certificates/{name}        remove one (deleteCertificate)
//
// Apply is NOT a single org-wide operation: TrustedCertificateTaskService.
// applyCertificates (web/src/main/java/.../collector/ts/handler/cert/
// TrustedCertificateTaskService.java) enqueues one ApplyCertificatesTask per
// collector in the org that supports custom certificates and returns the list
// of enqueued CollectorTaskIds. It already fans out correctly; there is no
// separate per-collector endpoint to find. Skyforge previously believed this
// route could not be called safely -- the real defect was in the collector's
// keytool import (missing -storepass on the freshly copied cacerts
// truststore), which is fixed separately and out of scope here.
//
// Applying certificates makes every affected collector restart to pick up the
// new truststore. Do not call Apply on every reconcile pass: check List first
// and only Add+Apply when the named certificate is missing or its content
// changed.
type TrustedCertificates struct {
	ports.TrustedCertificates
	session        Session
	collectorTasks *CollectorTasks
}

// failClosed returns the session's *ports.UnavailableError on a client from NewUnavailable, so each use case fails closed
// before any argument check. It is nil-safe: a nil TrustedCertificates reaches its own nil check.
func (s *TrustedCertificates) failClosed() error {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Unavailable()
}

// Vulnerabilities reads Forward's vulnerability analysis: which CVEs
// may affect the network's devices, and Forward's per-device verdict. The
// three reads are the published Vulnerability Analysis API
// (fwd/api/apis/vulnerability-analysis.yaml, VulnerabilityAnalysisController),
// served on primary 15398425a69 and stable 67e89c87124.
//
// NQE's device.cveFindings carries only a boolean isVulnerable; the detection
// result here (VULNERABLE, OS_VULNERABLE, UNCONFIRMED, ...) says how sure
// Forward is, which is what an investigation has to report.
type Vulnerabilities struct {
	ports.Vulnerabilities
	session Session
}

// Aliases reads, creates and deactivates a network's aliases: named sets of hosts, devices,
// interfaces, traffic headers or logical networks that checks and path
// searches refer to by name.
type Aliases struct {
	ports.Aliases
	session Session
}

// WanCircuits manages a network's WAN circuits: synthetic devices that
// each join two customer-edge ports, as a carrier circuit would. The published
// WAN Circuits API (fwd/api/apis/wan-circuits.yaml, WanCircuitController),
// served on primary 15398425a69 and stable 67e89c87124. Unlike the other
// synthetic devices, a WAN circuit cannot take an NQE query.
type WanCircuits struct {
	ports.WanCircuits
	session Session
}

// DataFiles manages Data Files: datasets (CSV, JSON, XML, YAML, XLSX,
// text) an org uploads so NQE queries can join them with the network model.
// A file attached to a network is collected with its snapshots and appears in
// NQE as network.extensions.<NQEName>, a record {status: OK | MISSING |
// INVALID_DATA, value?: <the file's inferred type>}. That field exists only in
// an org that has data files, so it is absent from the standard schema; GET
// /api/nqe/schema returns the org's own schema, extensions included.
// DataFileController; every route is on primary 15398425a69 and stable
// 67e89c87124. Preview: not in the published spec.
type DataFiles struct {
	ports.DataFiles
	session Session
}

// DataConnectors manages a network's data connectors: HTTP services a
// Collector fetches from on every collection, each endpoint's response stored
// in the snapshot under its own name. Unlike a data file (DataFilesService),
// which an org uploads once, a connector is a per-network collection source.
// The published Data Connectors API (fwd/api/apis/data-connectors.yaml,
// DataConnectorController), served on primary 15398425a69 and stable
// 67e89c87124.
type DataConnectors struct {
	ports.DataConnectorsAPI
	session Session
}

// AuditLogs reads Forward's audit log: which signed-in user called
// which mutating route, from where, when, and with what response code. It
// records requests, not bodies, so it says who did something and when, never
// what the change was. AuditLogController, served on primary 15398425a69 and
// stable 67e89c87124. Preview: not in the published spec.
//
// What is recorded (RequestLoggingFilter.persistAuditLogRecord):
//   - POST, PUT, PATCH and DELETE requests. GETs are recorded only on the few
//     routes annotated @Audited, and a mutating route can opt out the same
//     way (e.g. schema inference and connector tests), so absence is not proof
//     nothing happened.
//   - Only authenticated users. Collector requests are not recorded, and
//     failed logins go to a separate record.
//   - The path after /api -- "/networks/123/classic-devices", not
//     "/api/networks/123/classic-devices" -- plus the query string, with
//     _csrf, password, token and secret values replaced by "<redacted>",
//     cut at 2048 characters.
type AuditLogs struct {
	ports.AuditLogsAPI
	session Session
}

// Dashboards reads a network's dashboards (DashboardController).
//
// UNPUBLISHED: none of the dashboard, scorecard and KPI routes is in Forward's
// OpenAPI set, so their shapes may change between builds. The identifying
// fields are typed; everything else stays in Raw so a change does not break a
// read. They are served, with the routes below, on primary 15398425a69 and
// stable 67e89c87124. Reads only; dashboard and scorecard writes are not wrapped.
type Dashboards struct {
	ports.Dashboards
	session Session
}

// Scorecards reads a network's scorecards and the KPI categories they
// are built from (ScorecardController). Forward computes them for the
// organization's license tier, so what an org sees can depend on its licence.
//
// UNPUBLISHED: not in Forward's OpenAPI set, so shapes may change between
// builds. Identifying fields are typed; the rest stays in Raw. Served on
// primary 15398425a69 and stable 67e89c87124. Reads only; definitions cannot
// be created or changed through this service.
type Scorecards struct {
	ports.Scorecards
	session Session
}

// CollectionSchedules reads and writes a network's collection schedules: when
// Forward starts collections on its own. The published Collection Schedules
// API (collection-schedules.yaml), served on primary 15398425a69 and stable
// 67e89c87124.
type CollectionSchedules struct {
	ports.CollectionSchedules
	session Session
}

// AddSupportedOrganization grants or renews ONE supported org (POST /api/users/{id}/supported-orgs?orgId=&expiresIn=, a support-admin
// route with the same rules as SetSupportedOrganizations). expiresDays is how long the grant stays valid after the user's last activity
// in that org (impersonating refreshes it). Forward's own default is 30 and its maximum 365, and a value of 0 expires the grant by the end
// of the current minute; this SDK, unlike Forward, sends 365 for any value of 0 or less, so it cannot send 0 (revoke with
// SetSupportedOrganizations instead).
//
// A nil Admin answers as a nil service always has.
func (s *Admin) AddSupportedOrganization(ctx context.Context, userID string, orgID string, expiresDays int) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.AddSupportedOrganization(ctx, userID, orgID, expiresDays)
}

func (s *Admin) CreateOrganizationUser(ctx context.Context, orgID string, input domain.AdminUserCreateRequest) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.CreateOrganizationUser(ctx, orgID, input)
}

func (s *Admin) DeleteUser(ctx context.Context, userID string) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.DeleteUser(ctx, userID)
}

func (s *Admin) GrantOrganizationAdmin(ctx context.Context, userID string) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.GrantOrganizationAdmin(ctx, userID)
}

func (s *Admin) ListNetworks(ctx context.Context) (r0 []domain.AdminNetwork, r1 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.ListNetworks(ctx)
}

func (s *Admin) ListUsers(ctx context.Context) (r0 []domain.AdminUser, r1 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.ListUsers(ctx)
}

func (s *Admin) LookupUser(ctx context.Context, idOrUsername string) (r0 *domain.AdminUser, r1 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.LookupUser(ctx, idOrUsername)
}

func (s *Admin) PatchUser(ctx context.Context, userID string, patch domain.AdminUserPatch) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.PatchUser(ctx, userID, patch)
}

// SetSupportedOrganizations REPLACES a Forward support user's whole set of supported orgs (PUT /api/users/{id}/supported-orgs, a
// support-admin route; the user must be in the Forward org, the org named "Forward Networks", and not a Forward admin, a chatbot or
// supporting its own org). Orgs not listed are removed at once (this is also the only way to remove one: there is no per-org delete).
// There is no expiry parameter: EVERY listed org, including ones already granted, is rewritten with last-active = now and 30 days, so
// a grant made with AddSupportedOrganization for longer drops back to 30 days here. A grant stays valid for expires-in-days after the
// user's last activity in that org (each impersonation refreshes it), not after the grant was made. Read the current set with
// Users.Roles (SupportedOrgIDs) first.
//
// A nil Admin answers as a nil service always has.
func (s *Admin) SetSupportedOrganizations(ctx context.Context, userID string, orgIDs []string) (r0 *ports.Response, err error) {
	if s == nil || s.Admin == nil {
		err = domain.NilServiceError("admin")
		return
	}
	return s.Admin.SetSupportedOrganizations(ctx, userID, orgIDs)
}

// GetS3Storage returns the stored S3 settings, or nil when none are configured. The secret key is write-only: Forward
// marks S3StorageSettings.secretKey @JsonIgnore, so no response carries it, and the SDK clears SecretKey on every
// decoded response anyway so a caller can never read one back.
//
// A nil Backups answers as a nil service always has.
func (s *Backups) GetS3Storage(ctx context.Context) (r0 *domain.S3StorageSettings, r1 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.GetS3Storage(ctx)
}

func (s *Backups) GetSettings(ctx context.Context, storageType domain.StorageType) (r0 *domain.BackupSettings, r1 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.GetSettings(ctx, storageType)
}

func (s *Backups) Last(ctx context.Context, storageType domain.StorageType, triggerType domain.BackupTriggerType) (r0 *domain.BackupResult, r1 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.Last(ctx, storageType, triggerType)
}

func (s *Backups) SetS3BucketOwnership(ctx context.Context, settings domain.S3StorageSettings) (r0 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.SetS3BucketOwnership(ctx, settings)
}

func (s *Backups) Trigger(ctx context.Context, input domain.BackupTriggerRequest) (r0 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.Trigger(ctx, input)
}

func (s *Backups) UpdateS3Storage(ctx context.Context, patch domain.S3StorageSettingsPatch) (r0 *domain.S3StorageSettings, r1 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.UpdateS3Storage(ctx, patch)
}

func (s *Backups) UpdateSettings(ctx context.Context, storageType domain.StorageType, patch domain.BackupSettingsPatch) (r0 *domain.BackupSettings, r1 *ports.Response, err error) {
	if s == nil || s.Backups == nil {
		err = domain.NilServiceError("backups")
		return
	}
	return s.Backups.UpdateSettings(ctx, storageType, patch)
}

func (s *Browser) Cookies() (r0 []*http.Cookie) {
	if s == nil || s.Browser == nil {
		return nil
	}
	return s.Browser.Cookies()
}

// CurrentSession reads the session's principal together with its resolved
// roles, so a caller can prove a sign-in carries the access it needs.
//
// A nil Browser answers as a nil service always has.
func (s *Browser) CurrentSession(ctx context.Context) (r0 *domain.BrowserSession, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.CurrentSession(ctx)
}

func (s *Browser) CurrentUser(ctx context.Context) (r0 *domain.BrowserUser, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.CurrentUser(ctx)
}

func (s *Browser) Impersonate(ctx context.Context, targetUserID string) (r0 *ports.BrowserLoginResult, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.Impersonate(ctx, targetUserID)
}

func (s *Browser) ImpersonateWithServiceCredential(ctx context.Context, targetUserID string) (r0 *ports.BrowserLoginResult, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.ImpersonateWithServiceCredential(ctx, targetUserID)
}

func (s *Browser) LoginLegacy(ctx context.Context, input domain.BrowserLoginRequest, csrf domain.BrowserCSRFToken) (r0 *ports.BrowserLoginResult, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.LoginLegacy(ctx, input, csrf)
}

func (s *Browser) LoginPageCSRF(ctx context.Context) (r0 *domain.BrowserCSRFToken, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.LoginPageCSRF(ctx)
}

func (s *Browser) PublicCSRFAPI(ctx context.Context) (r0 *domain.BrowserCSRFToken, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.PublicCSRFAPI(ctx)
}

// SAMLAssertionConsumer posts the IdP's SAML Response to Forward's assertion
// consumer service for registrationID (POST /login/saml2/sso/{registrationId})
// on THIS client's Forward origin -- the same origin the AuthnRequest came
// from, since Forward keeps the request in the session and checks
// InResponseTo against it. The IdP's form addresses Forward's public URL,
// which an in-cluster client may not share; checking that the IdP aimed at
// the expected consumer is the caller's job, before calling this.
//
// Forward JIT-creates the user on the first sign-in to an org and refreshes
// its external groups on every sign-in. Success is a redirect that does not
// land on /login.
//
// A nil Browser answers as a nil service always has.
func (s *Browser) SAMLAssertionConsumer(ctx context.Context, registrationID string, fields url.Values) (r0 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.SAMLAssertionConsumer(ctx, registrationID, fields)
}

// SAMLAuthenticationRequest starts SP-initiated SAML sign-in to the org that
// owns registrationID (GET /saml2/authenticate/{registrationId}, served by
// Forward's SAML filter chain). Forward answers with the POST-binding form
// addressed to the IdP. The request runs on this client's cookie jar, so the
// same client must carry the assertion back through SAMLAssertionConsumer.
//
// A nil Browser answers as a nil service always has.
func (s *Browser) SAMLAuthenticationRequest(ctx context.Context, registrationID string) (r0 *ports.BrowserSAMLForm, r1 *ports.Response, err error) {
	if s == nil || s.Browser == nil {
		err = domain.NilServiceError("browser")
		return
	}
	return s.Browser.SAMLAuthenticationRequest(ctx, registrationID)
}

// All returns known statuses in stable capability-name order.
//
// A nil Capabilities answers as a nil service always has.
func (s *Capabilities) All() (r0 []domain.CapabilityStatus) {
	if s == nil || s.Capabilities == nil {
		return nil
	}
	return s.Capabilities.All()
}

// Profile returns a copy of the selected deployment profile.
//
// A nil Capabilities answers as a nil service always has.
func (s *Capabilities) Profile() (r0 domain.CapabilityProfile) {
	if s == nil || s.Capabilities == nil {
		return domain.CapabilityProfile{}
	}
	return s.Capabilities.Profile()
}

// Support reports whether capability is supported, unsupported, or unknown.
//
// A nil Capabilities answers as a nil service always has.
func (s *Capabilities) Support(capability domain.Capability) (r0 domain.CapabilityStatus) {
	if s == nil || s.Capabilities == nil {
		return domain.CapabilityStatus{Capability: capability, Support: domain.CapabilityUnknown}
	}
	return s.Capabilities.Support(capability)
}

// Reachable reports whether an unauthenticated /api/version request received
// any HTTP response. Status, including 401 or 404, is deliberately ignored.
//
// A nil Version answers as a nil service always has.
func (s *Version) Reachable(ctx context.Context) (r0 bool, r1 *ports.Response, err error) {
	if s == nil || s.Version == nil {
		return false, nil, domain.NilServiceError("version")
	}
	return s.Version.Reachable(ctx)
}

// Ports is every port one client's use cases are built on, and the session they share.
type Ports struct {
	Session                 Session
	Version                 ports.Version
	Networks                ports.Networks
	Devices                 ports.Devices
	ClassicDevices          ports.ClassicDevices
	ControllerManagedSetups ports.ControllerManagedSetups
	DeviceDiscovery         ports.DeviceDiscovery
	ConnectivityTests       ports.ConnectivityTests
	CustomCommandGroups     ports.CustomCommandGroups
	CloudManagedSetups      ports.CloudManagedSetups
	SaseSetups              ports.SaseSetups
	Decorators              ports.Decorators
	Trace                   ports.Trace
	SyntheticNodes          ports.SyntheticNodes
	Credentials             ports.Credentials
	CollectorTasks          ports.CollectorTasks
	CloudAccounts           ports.CloudAccounts
	Webhooks                ports.Webhooks
	Snapshots               ports.Snapshots
	NQE                     ports.NQE
	Predict                 ports.Predict
	AI                      ports.AI
	Diffs                   ports.Diffs
	AIAssist                ports.AIAssist
	Properties              ports.Properties
	Organizations           ports.Organizations
	Checks                  ports.Checks
	DeviceTags              ports.DeviceTags
	Performance             ports.Performance
	Capabilities            ports.Capabilities
	Raw                     ports.Raw
	Collectors              ports.Collectors
	Backups                 ports.Backups
	Browser                 ports.Browser
	Endpoints               ports.Endpoints
	Locations               ports.Locations
	Proxies                 ports.Proxies
	Users                   ports.Users
	Admin                   ports.Admin
	NQERepository           ports.NQERepository
	Banners                 ports.Banners
	Jobs                    ports.Jobs
	NetworkMaps             ports.NetworkMaps
	Routing                 ports.Routing
	ThreatExposure          ports.ThreatExposure
	EdgeAssets              ports.EdgeAssets
	Licensing               ports.Licensing
	SoftwareCentral         ports.SoftwareCentral
	CVEIndex                ports.CVEIndex
	FwdAppConnection        ports.FwdAppConnection
	ChangeReports           ports.ChangeReports
	AccessControl           ports.AccessControl
	SAML                    ports.SAML
	Configuration           ports.Configuration
	Integrations            ports.Integrations
	Topology                ports.Topology
	Collections             ports.Collections
	JumpServers             ports.JumpServers
	Compatibility           ports.Compatibility
	TrustedCertificates     ports.TrustedCertificates
	Vulnerabilities         ports.Vulnerabilities
	Aliases                 ports.Aliases
	WanCircuits             ports.WanCircuits
	DataFiles               ports.DataFiles
	DataConnectors          ports.DataConnectorsAPI
	AuditLogs               ports.AuditLogsAPI
	Dashboards              ports.Dashboards
	Scorecards              ports.Scorecards
	CollectionSchedules     ports.CollectionSchedules
	NetworkByID             networkByID
}

// Families is every family's use cases over one client's ports. A use case that composes another family holds that
// family's use cases from the same Families, so a ForNetwork clone's composites reach the clone, never the parent.
type Families struct {
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

// Wire builds every family's use cases over p, and injects each family the others it composes.
func Wire(p Ports) Families {
	var f Families
	f.Version = &Version{Version: p.Version, session: p.Session}
	f.Networks = &Networks{Networks: p.Networks, session: p.Session}
	f.Devices = &Devices{Devices: p.Devices, session: p.Session}
	f.ClassicDevices = &ClassicDevices{ClassicDevices: p.ClassicDevices, session: p.Session}
	f.ControllerManagedSetups = &ControllerManagedSetups{ControllerManagedSetups: p.ControllerManagedSetups, session: p.Session}
	f.DeviceDiscovery = &DeviceDiscovery{DeviceDiscovery: p.DeviceDiscovery, session: p.Session}
	f.ConnectivityTests = &ConnectivityTests{ConnectivityTests: p.ConnectivityTests, session: p.Session}
	f.CustomCommandGroups = &CustomCommandGroups{CustomCommandGroups: p.CustomCommandGroups, session: p.Session}
	f.CloudManagedSetups = &CloudManagedSetups{CloudManagedSetups: p.CloudManagedSetups, session: p.Session}
	f.SaseSetups = &SaseSetups{SaseSetups: p.SaseSetups, session: p.Session}
	f.Decorators = &Decorators{Decorators: p.Decorators, session: p.Session}
	f.Trace = &Trace{port: p.Trace, session: p.Session}
	f.SyntheticNodes = &SyntheticNodes{SyntheticNodes: p.SyntheticNodes, session: p.Session}
	f.Credentials = &Credentials{Credentials: p.Credentials, session: p.Session}
	f.CollectorTasks = &CollectorTasks{CollectorTasks: p.CollectorTasks, session: p.Session}
	f.CloudAccounts = &CloudAccounts{CloudAccounts: p.CloudAccounts, session: p.Session}
	f.Webhooks = &Webhooks{Webhooks: p.Webhooks, session: p.Session}
	f.Snapshots = &Snapshots{Snapshots: p.Snapshots, session: p.Session}
	f.NQE = &NQE{NQE: p.NQE, session: p.Session}
	f.Predict = &Predict{Predict: p.Predict, session: p.Session}
	f.AI = &AI{AI: p.AI, session: p.Session}
	f.Diffs = &Diffs{Diffs: p.Diffs, session: p.Session}
	f.AIAssist = &AIAssist{AIAssist: p.AIAssist, session: p.Session}
	f.Properties = &Properties{Properties: p.Properties, session: p.Session}
	f.Organizations = &Organizations{Organizations: p.Organizations, session: p.Session}
	f.Checks = &Checks{Checks: p.Checks, session: p.Session}
	f.DeviceTags = &DeviceTags{DeviceTags: p.DeviceTags, session: p.Session}
	f.Performance = &Performance{Performance: p.Performance, session: p.Session}
	f.Capabilities = &Capabilities{Capabilities: p.Capabilities, session: p.Session}
	f.Raw = &Raw{Raw: p.Raw, session: p.Session}
	f.Collectors = &Collectors{Collectors: p.Collectors, session: p.Session}
	f.Backups = &Backups{Backups: p.Backups, session: p.Session}
	f.Browser = &Browser{Browser: p.Browser, session: p.Session}
	f.Endpoints = &Endpoints{Endpoints: p.Endpoints, session: p.Session}
	f.Locations = &Locations{Locations: p.Locations, session: p.Session}
	f.Proxies = &Proxies{Proxies: p.Proxies, session: p.Session}
	f.Users = &Users{Users: p.Users, session: p.Session}
	f.Admin = &Admin{Admin: p.Admin, session: p.Session}
	f.NQERepository = &NQERepository{NQERepository: p.NQERepository, session: p.Session}
	f.Banners = &Banners{Banners: p.Banners, session: p.Session}
	f.Jobs = &Jobs{Jobs: p.Jobs, session: p.Session}
	f.NetworkMaps = &NetworkMaps{NetworkMaps: p.NetworkMaps, session: p.Session}
	f.Routing = &Routing{Routing: p.Routing, session: p.Session}
	f.ThreatExposure = &ThreatExposure{ThreatExposure: p.ThreatExposure, session: p.Session}
	f.EdgeAssets = &EdgeAssets{EdgeAssets: p.EdgeAssets, session: p.Session}
	f.Licensing = &Licensing{Licensing: p.Licensing, session: p.Session}
	f.SoftwareCentral = &SoftwareCentral{SoftwareCentral: p.SoftwareCentral, session: p.Session}
	f.CVEIndex = &CVEIndex{CVEIndex: p.CVEIndex, session: p.Session}
	f.FwdAppConnection = &FwdAppConnection{FwdAppConnection: p.FwdAppConnection, session: p.Session}
	f.ChangeReports = &ChangeReports{ChangeReports: p.ChangeReports, session: p.Session}
	f.AccessControl = &AccessControl{AccessControl: p.AccessControl, session: p.Session}
	f.SAML = &SAML{SAML: p.SAML, session: p.Session}
	f.Configuration = &Configuration{Configuration: p.Configuration, session: p.Session}
	f.Integrations = &Integrations{Integrations: p.Integrations, session: p.Session}
	f.Topology = &Topology{Topology: p.Topology, session: p.Session}
	f.Collections = &Collections{Collections: p.Collections, session: p.Session}
	f.JumpServers = &JumpServers{JumpServers: p.JumpServers, session: p.Session}
	f.Compatibility = &Compatibility{Compatibility: p.Compatibility, session: p.Session}
	f.TrustedCertificates = &TrustedCertificates{TrustedCertificates: p.TrustedCertificates, session: p.Session}
	f.Vulnerabilities = &Vulnerabilities{Vulnerabilities: p.Vulnerabilities, session: p.Session}
	f.Aliases = &Aliases{Aliases: p.Aliases, session: p.Session}
	f.WanCircuits = &WanCircuits{WanCircuits: p.WanCircuits, session: p.Session}
	f.DataFiles = &DataFiles{DataFiles: p.DataFiles, session: p.Session}
	f.DataConnectors = &DataConnectors{DataConnectorsAPI: p.DataConnectors, session: p.Session}
	f.AuditLogs = &AuditLogs{AuditLogsAPI: p.AuditLogs, session: p.Session}
	f.Dashboards = &Dashboards{Dashboards: p.Dashboards, session: p.Session}
	f.Scorecards = &Scorecards{Scorecards: p.Scorecards, session: p.Session}
	f.CollectionSchedules = &CollectionSchedules{CollectionSchedules: p.CollectionSchedules, session: p.Session}
	f.ClassicDevices.credentials = f.Credentials
	f.CollectorTasks.snapshots = f.Snapshots
	f.Snapshots.collectorTasks = f.CollectorTasks
	f.Predict.classicDevices = f.ClassicDevices
	f.Predict.properties = f.Properties
	f.Predict.snapshots = f.Snapshots
	f.Organizations.admin = f.Admin
	f.Performance.classicDevices = f.ClassicDevices
	f.Performance.credentials = f.Credentials
	f.ChangeReports.diffs = f.Diffs
	f.ChangeReports.nqe = f.NQE
	f.ChangeReports.predict = f.Predict
	f.ChangeReports.snapshots = f.Snapshots
	f.ChangeReports.trace = f.Trace
	f.TrustedCertificates.collectorTasks = f.CollectorTasks
	f.Networks.byID = p.NetworkByID
	return f
}
