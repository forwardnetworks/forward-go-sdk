package ports

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Session is what a use case may know about the client it runs on: the network it is bound to (ForNetwork) and the principal
// it acts as. It carries no URL and no credential.
type Session interface {
	Network() string
	AuthMode() AuthMode
}

// Version is the Version API family: what Client.Version sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Version interface {
	// Get returns the appliance's current API version.
	Get(ctx context.Context) (*APIVersion, *Response, error)

	// Reachable reports whether an unauthenticated /api/version request received
	// any HTTP response. Status, including 401 or 404, is deliberately ignored.
	Reachable(ctx context.Context) (bool, *Response, error)
}

// Networks is the Networks API family: what Client.Networks sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Networks interface {
	// CheckAccess preserves status-only consumers that deliberately do not require
	// a JSON list body.
	CheckAccess(ctx context.Context) (*Response, error)

	// CloudObject returns one cloud object by the provider's own ID (rtb-..., sg-...).
	// GET /api/networks/{networkId}/cloud-objects/{objectId}
	// (VIEW_NETWORK_AND_SNAPSHOTS). An empty snapshotID uses the latest processed
	// snapshot. Preview.
	CloudObject(ctx context.Context, networkID string, objectID string, snapshotID string) (*CloudObjectInfo, *Response, error)

	// CloudObjectFiles lists a cloud object's stored files. For an object that
	// exists but has none, Forward returns a single dummy entry for a file that does
	// not exist (DeviceState PROPERTIES), so do not treat a non-empty list as proof
	// of content. GET /api/networks/{networkId}/cloud-objects/{objectId}/files
	// (VIEW_COLLECTED_FILES; the snapshot must have reached the STORED_FILES
	// stage). Preview.
	CloudObjectFiles(ctx context.Context, networkID string, objectID string, options CloudObjectFilesOptions) ([]CloudObjectFile, *Response, error)

	// CloudObjects lists the cloud objects collected in a snapshot, in Forward's
	// order. GET /api/networks/{networkId}/cloud-objects (CloudObjectController;
	// VIEW_NETWORK_AND_SNAPSHOTS). Preview: not in the published spec.
	CloudObjects(ctx context.Context, networkID string, options CloudObjectListOptions) (*CloudObjectPage, *Response, error)

	// Create creates a network with name.
	Create(ctx context.Context, name string) (*Network, *Response, error)

	// CreateWorkspace creates a workspace network beneath parentNetworkID.
	CreateWorkspace(ctx context.Context, parentNetworkID string, request WorkspaceNetworkRequest) (*Network, *Response, error)

	// Delete deletes a network and returns its final representation.
	Delete(ctx context.Context, networkID string) (*Network, *Response, error)

	// GetSnapshotRetentionPolicy returns the network's snapshot thinning policy:
	// the saved one, or the deployment's default. GET
	// /api/networks/{networkId}/snapshotRetentionPolicy
	// (SnapshotRetentionController; VIEW_COLLECTION_SETTINGS). Preview: not in the
	// published spec.
	GetSnapshotRetentionPolicy(ctx context.Context, networkID string) (*SnapshotRetentionPolicy, *Response, error)

	// InterestingPaths runs Forward's interesting-path search on a snapshot (GET /api/networks/{id}/interesting-path-queries?snapshotId=&type=
	// &maxSeconds=, InterestingPathsController; the UI's "View interesting paths"). Read-only as far as network data goes (it caches its
	// result and records a usage event), but SYNCHRONOUS and expensive: Forward samples hosts and searches flows on the backend, and the call
	// blocks until it is done or maxSeconds passes. maxSeconds is a soft deadline: Forward returns what it has found, and a run can overshoot
	// it by a search or two. maxSeconds 0 sends nothing, so Forward's own default of 300 seconds applies (the SDK never sends 0: Forward fails
	// on it); the HTTP timeout is maxSeconds plus 90 seconds, and at most 3600 seconds is accepted here. The job is reusable and cached per
	// snapshot, type and maxSeconds, and the sampling is deterministic, so calling again after a client timeout returns the stored result
	// cheaply. Needs the network operation VIEW_PATHS (licence tier B gets 403 unlicensed-operation) and a snapshot processed through its
	// advanced-reachability stage: otherwise Forward answers 400 "Advanced reachability <STATE> for snapshot ...", typed here as
	// ErrSnapshotNotProcessed (or ErrSnapshotProcessingFailed for a canceled, timed-out or failed run); start it with
	// Snapshots.ComputeAdvancedReachability. Forward would pick the latest snapshot if none were named; this SDK requires one.
	InterestingPaths(ctx context.Context, networkID string, snapshotID string, kind InterestingPathsKind, maxSeconds int) (*InterestingPaths, *Response, error)

	// L7Applications returns the application names path search recognizes. GET
	// /api/l7-applications (getL7Applications, published; PathSearchController on
	// primary 15398425a69 and stable 67e89c87124). The list is the appserver's,
	// not a network's, so no network is named.
	L7Applications(ctx context.Context) ([]L7Application, *Response, error)

	// List returns all networks visible to the authenticated principal.
	List(ctx context.Context) ([]Network, *Response, error)

	// Paths executes a path analysis query.
	//
	// Either From or SrcIP is required, and DstIP always is: a search with no
	// destination has no question in it.
	Paths(ctx context.Context, networkID string, input PathSearchRequest) (*PathSearchResponse, *Response, error)

	// PathsBulk runs many path searches against one snapshot in a single request (POST /api/networks/{networkId}/paths-bulk, published,
	// VIEW_PATHS; the snapshot must be processed to the reachability stage) and returns one result per query, in the order of queries.
	//
	// Between 1 and MaxPathBulkQueries (20000) queries are accepted; Forward recommends about 1000 per request and answers 400 beyond the
	// limit, which this SDK refuses first. A malformed query (a bad address, ports with a protocol other than 6 or 17, an ICMP type with a
	// port) fails the WHOLE request with a 400 naming it; only a problem found while searching (a device not in the snapshot, a compute
	// error) becomes a per-query failure. Each result is a path search response like Networks.Paths returns, with ReturnPathInfo filled in
	// when MaxReturnPathResults is above zero (always empty for a multicast query), or a failure for that query alone (see PathBulkResult).
	// A matrix of A to B and B to A searches is two queries per pair. The whole body is read before PathsBulk returns; Forward's
	// streaming variant (paths-bulk-seq) is not wrapped.
	//
	// This is a read sent as a POST, so a retry policy resends it only after a 429 or 503 or a failure to connect; wrap the context in
	// WithIdempotent to also resend it after a gateway error or a lost connection, bearing in mind that Forward is probably still
	// computing the first attempt.
	PathsBulk(ctx context.Context, networkID string, queries []PathQuery, options PathBulkOptions) ([]PathBulkResult, *Response, error)

	// PreviewSnapshotRetention lists the snapshots the network's retention policy
	// would delete if the cleanup ran now, and deletes NOTHING: the request always
	// carries dryRun=true. POST /api/networks/{networkId}/snapshotRetentionPolicy/trigger
	// ?dryRun=true (SnapshotRetentionController; DELETE_SNAPSHOT, which even the
	// preview needs). The same route without dryRun deletes synchronously, so the
	// SDK does not expose that.
	//
	// Forward always keeps, whatever the policy: the 10 newest processed
	// snapshots of the network (not forks, drafts or restored ones), every
	// favorited snapshot, forks and predictions with their ancestors and bases,
	// and restored snapshots newer than the RESTORED_SNAPSHOT_RETENTION_IN_DAYS
	// org property. The preview applies all of those.
	//
	// What it does not check: the SNAPSHOT_AUTO_CLEANUP org property (default
	// true; org admins cannot change it) that switches the daily 05:00 UTC
	// cleanup off for every network in the org. With it false the real job
	// deletes nothing although this preview may list snapshots; read it with
	// Properties before warning about deletions. A disabled policy
	// (Enabled false) previews as empty. Preview.
	PreviewSnapshotRetention(ctx context.Context, networkID string) (*SnapshotRetentionPreview, *Response, error)

	// SecurityZones returns the security zones each device defines, as device name to zone names, for a snapshot (the
	// latest processed one when snapshotID is empty, which sends no parameter so the appserver applies its own default).
	// GET /api/networks/{networkId}/security-zones (SecurityAnalysisController.getSecurityZones; VIEW_SECURITY_ANALYSIS;
	// needs the snapshot to have reached the CREATION stage; on primary 15398425a69 and stable 67e89c87124). Preview: not
	// in the published spec. The zone-to-zone security matrix routes return compressed protobuf and are not modelled.
	SecurityZones(ctx context.Context, networkID string, snapshotID string) (map[string][]string, *Response, error)

	// SetSnapshotRetentionPolicy replaces the network's snapshot thinning policy.
	// PUT /api/networks/{networkId}/snapshotRetentionPolicy (SnapshotRetentionController;
	// EDIT_COLLECTION_SETTINGS and DELETE_SNAPSHOT). Preview.
	//
	// It changes which snapshots the daily cleanup deletes, so a stricter policy
	// is a deletion later; PreviewSnapshotRetention shows what it would take. To
	// stop thinning a network, GetSnapshotRetentionPolicy, set Enabled false and
	// save it back: that keeps the other fields valid, which Forward requires even
	// for a disabled policy.
	//
	// Only ON-PREM deployments accept this. On SaaS the policy is fixed and Forward
	// answers 404 ("policy modification not allowed on cloud"), which is also what
	// an unknown network gets; the error says so and still satisfies
	// IsStatus(err, 404). Thinning is also switched off for a whole organization by
	// the SNAPSHOT_AUTO_CLEANUP org property (see PreviewSnapshotRetention).
	SetSnapshotRetentionPolicy(ctx context.Context, networkID string, policy SnapshotRetentionPolicy) (*Response, error)

	// Update changes fields on an existing network.
	Update(ctx context.Context, networkID string, update NetworkUpdate) (*Network, *Response, error)
}

// Devices is the Devices API family: what Client.Devices sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Devices interface {
	// BGPAdvertisements returns the BGP advertisements a device received for one prefix in one VRF, as the snapshot recorded them: the
	// eBGP ones first, then the iBGP ones. GET /api/networks/{networkId}/devices/{deviceName}/bgp-advertisements?vrf=&prefix=&snapshotId=
	// (DeviceController.getDeviceBgpAdvertisements, VIEW_NETWORK_AND_SNAPSHOTS; served on primary and stable). Preview: the route is not in
	// the published OpenAPI; the shape is BgpAdvertisement, the same record Predict stages, so a received route and a staged one compare
	// field by field. The Device field is not part of the body and stays empty.
	//
	// Forward requires both vrf and prefix: there is no "list everything" form, so a caller that wants a withdraw pre-check asks for the
	// key's own VRF and prefix and looks for a row with the same ExternalPeer. The prefix is an IP subnet, "10.10.10.0/24"; an empty
	// snapshotID lets Forward use the latest processed snapshot. A VRF or prefix with no advertisements is an empty list, not an error.
	// A received row can omit nextHop and origin: Forward builds it from the parsed table with a builder that skips the checks a staged
	// advertisement goes through (BgpAdvertisementUtils.getBgpAdvertisement), so empty values there are Forward's, not a decode loss.
	// Seen live (26.11.0-03, cEOS): a host route comes back with a BARE prefix ("10.0.0.2", no /32), the query accepts the bare or the /32 form,
	// unset localPref and med are absent and communities is null, and a snapshot with no BGP routes answers 200 with an empty list. Compare
	// returned prefixes after normalising a bare address to its host route. // It reads the snapshot's parsed received-route tables (EBGP_RECV_ROUTES, IBGP_RECV_ROUTES) via Predict's BgpAdvertisementUtils; whether a
	// predicted snapshot shows staged advertisements here is NOT confirmed, so read predicted routes with Routing.RPDevicePrefixInfo and
	// Routing.RPPrefixDetails.
	BGPAdvertisements(ctx context.Context, networkID string, deviceName string, snapshotID string, vrf string, prefix string) ([]BGPAdvertisement, *Response, error)

	// Diff reports each device added, removed or modified between the before and after snapshots of a network; unchanged
	// devices are omitted and devices match by name ignoring case. GET /api/networks/{id}/device-diffs/{before}/{after}
	// (DeviceController, published in 26.10 as experimental). Answers 409 (ErrSnapshotNotProcessed) while Forward is processing either snapshot.
	Diff(ctx context.Context, networkID string, beforeSnapshotID string, afterSnapshotID string, options DeviceDiffOptions) (*DeviceDiffs, *Response, error)

	// DownloadFile streams a raw collected device file.
	DownloadFile(ctx context.Context, networkID string, deviceName string, fileName string, snapshotID string, dst io.Writer) (*Response, error)

	// DownloadFileHead writes at most maxBytes of a collected device file to dst
	// and stops, reporting whether the file was longer. Forward does not honor
	// Range on this route (its handler returns an InputStreamResource, which
	// Spring MVC serves whole), so the cap is applied here: the response body is
	// closed once maxBytes have arrived, which stops the transfer rather than
	// discarding the rest. A truncated read is not an error.
	DownloadFileHead(ctx context.Context, networkID string, deviceName string, fileName string, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error)

	// Get returns one modeled device.
	Get(ctx context.Context, networkID string, deviceName string, snapshotID string, with ...string) (*Device, *Response, error)

	// Interface returns one of a device's interfaces by name or alias (case-sensitive; a "/" in it is percent-encoded).
	// GET /api/networks/{id}/devices/{device}/interfaces/{interface}; experimental, see Interfaces.
	Interface(ctx context.Context, networkID string, deviceName string, interfaceName string, options DeviceInterfaceOptions) (*DeviceInterface, *Response, error)

	// Interfaces lists a device's interfaces, sorted by name. GET /api/networks/{id}/devices/{device}/interfaces
	// (DeviceController, published in 26.10 as experimental; 409 while Forward processes the snapshot, see ErrSnapshotNotProcessed; the same path served an unpublished route with a different
	// shape: type was a display name, VLANs were strings, neighbors were "device port" strings and came without a request).
	Interfaces(ctx context.Context, networkID string, deviceName string, options DeviceInterfaceOptions) ([]DeviceInterface, *Response, error)

	// List returns modeled devices for a network snapshot.
	List(ctx context.Context, networkID string, options DeviceListOptions) ([]Device, *Response, error)

	// ListFiles lists raw collected files for a device.
	ListFiles(ctx context.Context, networkID string, deviceName string, snapshotID string) ([]DeviceFile, *Response, error)

	// Missing returns the devices a snapshot's neighbors see but Forward does not
	// model -- usually ones that were never added or failed collection. GET
	// /api/networks/{networkId}/missing-devices?snapshotId= (DeviceController.
	// getMissingDevices, published; served on primary 15398425a69 and stable
	// 67e89c87124). An empty snapshotID lets Forward pick the latest processed
	// snapshot.
	Missing(ctx context.Context, networkID string, snapshotID string) ([]MissingDevice, *Response, error)
}

// ClassicDevices is the ClassicDevices API family: what Client.ClassicDevices sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type ClassicDevices interface {
	// ConnTypes lists the device connection types that can be classic devices, including INTERNAL ones the UI needs the names of
	// (GET /api/deviceConnTypes?for=ui, ClientController.getDeviceTypes; any signed-in user; without for=ui only the exposed types come back). Sorted by ID. Preview:
	// unpublished. The UI's "devices need to be migrated" count joins a device's type (or its test result's DiscoveredType)
	// against this list.
	ConnTypes(ctx context.Context) ([]ConnTypeInfo, *Response, error)

	// ConnTypesExposed lists only the exposed types (GET /api/deviceConnTypes with no for=, ClientController.getDeviceTypes). This is
	// the NARROWER list: ConnTypes (for=ui) already includes every type that can be a classic device, INTERNAL and UNDER_DEVELOPMENT
	// ones too, so use ConnTypes to learn about a hidden type and this to see what the product offers. Sorted by ID. Preview.
	ConnTypesExposed(ctx context.Context) ([]ConnTypeInfo, *Response, error)

	Create(ctx context.Context, networkID string, input ClassicDeviceRequest) (*ClassicDevice, *Response, error)

	Delete(ctx context.Context, networkID string, deviceName string) (*Response, error)

	Get(ctx context.Context, networkID string, deviceName string, with ...string) (*ClassicDevice, *Response, error)

	// GetMany reads the named classic devices in one request, in place of one Get
	// per device or a List of the whole network: POST
	// /api/networks/{networkId}/classic-devices?action=getBatch
	// (ClassicDeviceController.getSpecificClassicDevices, published;
	// VIEW_COLLECTION_SOURCES) with {"names": [...]}. with is the same list as in
	// List ("tags", "testResult" and so on). A name that matches no device is left
	// out of the result rather than failing the call, so check the count when
	// every device must exist.
	GetMany(ctx context.Context, networkID string, names []string, with ...string) ([]ClassicDevice, *Response, error)

	// List reads every classic device of the network in one request: GET
	// /api/networks/{networkId}/classic-devices takes no offset or limit
	// (ClassicDeviceController.getClassicDevices), so there is no paging to be
	// unstable. For a subset by name use GetMany.
	List(ctx context.Context, networkID string, with ...string) ([]ClassicDevice, *Response, error)

	ListTestStatuses(ctx context.Context, networkID string) ([]SourceTestStatus, *Response, error)

	// Patch updates only the fields the patch states.
	Patch(ctx context.Context, networkID string, deviceName string, patch ClassicDevicePatch) (*ClassicDevice, *Response, error)

	// PatchMany applies ONE patch to many classic devices in a single request:
	// PATCH /api/networks/{networkId}/classic-devices with {"names": [...],
	// "update": <patch>} (ClassicDeviceController.patchClassicDevices, published as
	// ClassicDevicesPatch; MANAGE_COLLECTION_SOURCES). It is the way to change
	// thousands of devices without thousands of PATCH calls: Forward applies it in
	// one transaction, so it either changes every named device or none (a bad
	// credential or jump server id, a collector that does not exist, or a
	// conflict fails the whole batch). Names are matched case-insensitively, and a
	// name that matches no device is skipped silently, so compare with List when
	// that matters. The same patch goes to every device: per-device values need
	// per-device Patch calls (or PutBatch). patch.Clear and the pointer fields work
	// exactly as in Patch, except Name, which PatchMany refuses. There is no response body.
	PatchMany(ctx context.Context, networkID string, names []string, patch ClassicDevicePatch) (*Response, error)

	Put(ctx context.Context, networkID string, deviceName string, input ClassicDeviceRequest) (*ClassicDevice, *Response, error)

	PutBatch(ctx context.Context, networkID string, devices []ClassicDeviceBatchItem) (*Response, error)

	// ResolveGlobs says which of the network's classic devices of the given connection types each glob matches (POST
	// ...classic-devices?action=resolveGlobs, ClassicDeviceController.resolveGlobs; VIEW_COLLECTION_SOURCES; a read, nothing changes).
	// A custom command group's Devices are such globs, so this answers "which devices would this group's commands run on". The
	// result maps each glob, as sent, to the matching device names; a glob that matches nothing may be missing from the map (Forward
	// sends {} when no classic device of those types exists), so read it with a lookup. Globs match ignoring case. Globs and connTypes must be non-empty,
	// and no glob blank.
	ResolveGlobs(ctx context.Context, networkID string, globs []string, connTypes []string) (map[string][]string, *Response, error)
}

// ControllerManagedSetups is the ControllerManagedSetups API family: what Client.ControllerManagedSetups sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type ControllerManagedSetups interface {
	// Create adds a controller-managed setup.
	//
	// Names are unique across every collection source in the network, so a controller or guest that a classic device
	// already holds under the same name is refused with a 400; CreateMigratingClassic is the one atomic way to take such
	// devices over.
	Create(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error)

	// CreateMigratingClassic adds a controller-managed setup and, in the same transaction, DELETES every classic device
	// whose name matches one of the setup's controllers or managed devices (POST ...controller-managed-setups?action=migrate,
	// addControllerManagedSetupWithMigration, published as Experimental). It is how the UI moves devices that were added
	// as classic devices into a setup: if the setup is refused, nothing is deleted; if it is accepted, the classic devices are
	// gone, with their per-device settings, their connectivity test results and their device tags. Nothing is copied onto the new
	// entry, so state in the request every credential (HTTPCredentialID for an API-collected controller such as a Versa Director),
	// jump server, port and collection option (note, BGP, SNMP, pagination, prompts, thresholds: all fields of ControllerDevice and
	// ManagedDevice) you want kept, or Forward falls back to auto-association and its defaults. Test results and tags cannot be carried.
	// Only the setup's controllers and managed devices are matched, not the setup's own name (a classic device called that is
	// still a 400). A name held by a
	// non-classic source (another setup, an endpoint) is still a 400 and rolls the whole thing back. Names that match no
	// classic device are simply added. Read the classic devices you are about to replace first.
	CreateMigratingClassic(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error)

	// Delete removes a setup by name. A 404 is success: the desired state, that
	// the setup is absent, already holds.
	Delete(ctx context.Context, networkID string, setupName string) (*Response, error)

	// DeleteController removes one controller from a setup without restating the others (DELETE
	// /api/networks/{id}/controller-managed-setups/{setup}/controllers/{name}, ControllerManagedSetupController.deleteControllerDevice,
	// DELETE_COLLECTION_SOURCES, 204; mapped at both pinned builds). Removing the setup's LAST controller is a 400 (the setup would be
	// left with none; ControllerManagedSetupService.deleteControllerDevice maps IllegalArgumentException to BadRequestException), so
	// delete the whole setup with Delete instead. A 404 is success (the member, or its setup, is already absent), so a typo in the setup
	// name also reads as success. Preview.
	DeleteController(ctx context.Context, networkID string, setupName string, controllerName string) (*Response, error)

	// DeleteManagedDevice removes one guest device from a setup without restating the others (DELETE
	// .../controller-managed-setups/{setup}/managed-devices/{name}, deleteManagedDevice, DELETE_COLLECTION_SOURCES, 204). Removing the
	// last managed device is a 400, as for DeleteController; a 404 is success. Preview.
	DeleteManagedDevice(ctx context.Context, networkID string, setupName string, deviceName string) (*Response, error)

	// List returns the network's controller-managed setups.
	//
	// Forward wraps them as {"setups": [...]}. A bare array and the other list
	// envelopes are accepted too, because a reader that knows one shape does not
	// fail on another -- it returns EMPTY, and an empty list here means "no setup
	// exists", which sends the caller straight into a create that then collides
	// with the setup that was sitting there ("Controller-managed setup named
	// 'sdwan' already exists in network", measured on cs-lab network 3150).
	//
	// A body that is NEITHER an array nor an object with a known key is an
	// ERROR, not an empty list -- an HTML login page or {"error":...} must not
	// read as "this network has no setups".
	List(ctx context.Context, networkID string) ([]ControllerManagedSetup, *Response, error)

	// ListWithTestResults is List with ?with=testResult: each controller and guest carries its last connectivity test
	// (DeviceTestResult), which is where the outcome of Test shows up. The same read is available per device on classic devices
	// through ClassicDevices.ListTestStatuses.
	ListWithTestResults(ctx context.Context, networkID string) ([]ControllerManagedSetup, *Response, error)

	// Patch applies a partial update to an existing setup and returns it as stored.
	//
	// This is the idempotent way to declare a setup's guests. Forward compares the
	// patched setup against the stored one and writes only if they differ
	// (ControllerManagedSetupController.patchSetup: `if (!updated.equals(existing))`),
	// so re-sending the same guest list is a no-op on the server -- it does not
	// touch the controllers and does not churn the connectivity test result the way
	// a delete-and-recreate would.
	//
	// NAME COLLISIONS ARE THE CALLER'S PROBLEM. Forward validates a device name as
	// unique across ALL of a network's collection sources, so patching in a guest
	// whose name is still held by a classic device fails with a 400. The classic
	// record has to go first. Forward's own ?action=migrate create does exactly
	// that (addSetupMigratingClassicDevices deletes the overlapping classic devices
	// inside the same transaction), but there is no migrating form of PATCH.
	//
	// ManagedDevices and Controllers replace the stored list, so send whole entries (a list read back and re-sent round-trips
	// every property the entries model, which is all of them except the guest's hidden concurrency and attribution). To change one
	// controller or guest without restating the rest, use PatchController or PatchManagedDevice. A setup rename is not modelled.
	Patch(ctx context.Context, networkID string, setupName string, patch ControllerManagedSetupPatch) (*ControllerManagedSetup, *Response, error)

	// PatchController changes one controller of a setup in place and returns it as stored. The patch is the same
	// property set as ClassicDevicePatch (name, type, host, port, the CLI, HTTP, SNMP and jump-server ids, collection options,
	// thresholds, Clear for explicit nulls), except that CollectorID is refused: a setup's collector is set on the setup,
	// with Patch. A rename is allowed, done in place (the test result moves with it), follows Forward's naming rules, and is not
	// idempotent: the old name answers 404 afterwards, so do not retry it with WithIdempotent. PATCH
	// /api/networks/{id}/controller-managed-setups/{setup}/controllers/{name} (ControllerManagedSetupController.
	// patchControllerDevice, ControllerDevicePatch; MANAGE_COLLECTION_SOURCES). ndoName is not in the patch; set it through
	// Patch with Controllers.
	PatchController(ctx context.Context, networkID string, setupName string, controllerName string, patch ClassicDevicePatch) (*ControllerDevice, *Response, error)

	// PatchManagedDevice changes one guest device of a setup in place and returns it as stored; the patch is the same as for
	// PatchController. Unlike Patch with ManagedDevices, it touches only the properties the patch states, so nothing the SDK
	// does not model is dropped. PATCH /api/networks/{id}/controller-managed-setups/{setup}/managed-devices/{name}
	// (ControllerManagedSetupController.patchManagedDevice, ManagedDevicePatch; MANAGE_COLLECTION_SOURCES).
	PatchManagedDevice(ctx context.Context, networkID string, setupName string, deviceName string, patch ClassicDevicePatch) (*ManagedDevice, *Response, error)

	// Test queues a connectivity test of the selected controllers and managed devices (POST
	// ...controller-managed-setups?action=test, testControllerManagedSetups, published as Experimental; the UI's "Test" button). Forward answers
	// 204 as soon as the tests are queued, not when they finish: read each device's testResult afterwards (ListWithTestResults, or
	// ClassicDevices.ListTestStatuses for a device still held as a classic one). SetupNames are matched case-sensitively against the stored (lowercase) names, so Test folds
	// them to lowercase; a name that matches no setup tests nothing and still answers 204. collectorID, when not empty, runs them through that collector instead of the setup's own.
	Test(ctx context.Context, networkID string, collectorID string, selection ControllerAndManagedDeviceSelection) (*Response, error)
}

// DeviceDiscovery is the DeviceDiscovery API family: what Client.DeviceDiscovery sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type DeviceDiscovery interface {
	// CreateScheduled adds a scheduled discovery (POST ...scheduled-discoveries; EDIT_COLLECTION_SETTINGS) and returns it with
	// its id. Forward refuses (400) a name that matches an existing one ignoring case, or a schedule identical to an existing one
	// (a second one with no schedule included). The scheduled scan runs from the network's default collector; Forward's scheduled-discovery params carry no
	// collector. An enabled schedule scans the customer network on its own, so create it disabled unless that is intended.
	CreateScheduled(ctx context.Context, networkID string, definition ScheduledDiscoveryDefinition) (*ScheduledDiscovery, *Response, error)

	// DeleteScheduled removes a scheduled discovery by id (DELETE ...scheduled-discoveries/{id}; 204). Forward answers 204 for an
	// id that does not exist too, so a 204 does not prove it was there; a 404 is a missing network (or an unserved route) and is
	// returned as the error, never as success, so a wrong network id cannot look like a completed delete. Discovered devices it
	// already found stay listed.
	DeleteScheduled(ctx context.Context, networkID string, id string) (*Response, error)

	// GetScheduled returns one scheduled discovery by id (a 404 when there is none).
	GetScheduled(ctx context.Context, networkID string, id string) (*ScheduledDiscovery, *Response, error)

	// ListDiscovered returns the discovery jobs and the devices they found (GET ...discovered-devices; VIEW_NETWORK_AND_SNAPSHOTS).
	// A non-zero since limits the result to what changed at or after that time (Forward's `since` instant).
	ListDiscovered(ctx context.Context, networkID string, since time.Time) (*DiscoveredDevices, *Response, error)

	// ListScheduled returns the network's scheduled discoveries (GET ...scheduled-discoveries; VIEW_COLLECTION_SETTINGS).
	ListScheduled(ctx context.Context, networkID string) ([]ScheduledDiscovery, *Response, error)

	// Start begins a discovery from collectorID's collector (empty: the network's default) and returns the collector task id
	// (POST ...deviceDiscovery?op=start, MANAGE_COLLECTION_SOURCES). It returns as soon as the job is queued; read the outcome with
	// ListDiscovered. The task id carries a prefix ("P8781") and is not a DiscoveryJob.ID ("8781"): match the job by its StartedAt
	// and Type, or take the newest. The network is scanned by that collector until the job ends or Stop is called. Forward answers
	// 409 when a discovery is already running, and 400 for a type that logs in to devices when the organization's
	// EXPLORATORY_LOGIN setting is off.
	Start(ctx context.Context, networkID string, collectorID string, params DiscoveryParams) (Identifier, *Response, error)

	// Stop ends the network's running discovery (POST ...deviceDiscovery?op=stop; 204). What it already found stays listed.
	Stop(ctx context.Context, networkID string) (*Response, error)
}

// ConnectivityTests is the ConnectivityTests API family: what Client.ConnectivityTests sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type ConnectivityTests interface {
	// BulkStart queues connectivity tests of the named classic devices (POST ...connectivityTests/bulkStart {"devices": [...]};
	// 204). At most MaxConnectivityTestDevices names per call, duplicates folded. Forward refuses the whole call, queueing nothing,
	// when any name is not a classic device of the network ("Devices not found in this network").
	BulkStart(ctx context.Context, networkID string, deviceNames []string) (*Response, error)

	// BulkStop cancels queued or running connectivity tests of the named devices (POST ...connectivityTests/bulkStop; 204), with no
	// cap. Names that are unknown or have nothing queued are ignored, and it also cancels connectivity tests a running collection
	// queued for those devices.
	BulkStop(ctx context.Context, networkID string, deviceNames []string) (*Response, error)

	// Get returns one device's stored result (GET ...connectivityTests/{deviceName}); a device never tested is a 404
	// ("Connectivity test result not found").
	Get(ctx context.Context, networkID string, deviceName string) (*ConnectivityTestResult, *Response, error)

	// List returns every stored connectivity test result of the network, one per tested device (GET ...connectivityTests;
	// VIEW_NETWORK_AND_SNAPSHOTS). It can be large on a big network: for a few devices use Get.
	List(ctx context.Context, networkID string) ([]ConnectivityTestResult, *Response, error)

	// Start queues a connectivity test of one classic device (POST ...connectivityTests/{deviceName}/start; 204;
	// TEST_COLLECTION_SOURCES).
	Start(ctx context.Context, networkID string, deviceName string) (*Response, error)

	// Stop cancels a queued or running connectivity test of one device (POST ...connectivityTests/{deviceName}/stop; 204). A device
	// with nothing queued is ignored. It also cancels a connectivity test a running collection queued for that device.
	Stop(ctx context.Context, networkID string, deviceName string) (*Response, error)
}

// CustomCommandGroups is the CustomCommandGroups API family: what Client.CustomCommandGroups sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CustomCommandGroups interface {
	// Create adds a group (POST .../custom-command-groups; 201, no body). A name already used (ignoring case) is a 400. A command
	// sequence (":::") needs a collector that supports sequences, else 400.
	Create(ctx context.Context, networkID string, group NewCustomCommandGroup) (*Response, error)

	// Delete removes a group (DELETE .../custom-command-groups/{name}; 204), matching the stored name exactly. A group that is not
	// there counts as deleted (Forward's 404 "Custom command group ... not found"); a missing network does not, and is returned as the
	// error. The commands stop being collected from the next collection on.
	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// Get returns one group by name, ignoring case. Forward has no per-name read, so this filters the list; a name that is not there
	// is ErrCustomCommandGroupNotFound.
	Get(ctx context.Context, networkID string, name string) (*CustomCommandGroup, *Response, error)

	// List returns the network's custom command groups (GET .../custom-command-groups).
	List(ctx context.Context, networkID string) ([]CustomCommandGroup, *Response, error)

	// Update changes a group (PATCH .../custom-command-groups/{name}; 204). A name that does not exist is a 404 "Custom command
	// group ... not found". Disable or enable a group with Disabled.
	Update(ctx context.Context, networkID string, name string, update CustomCommandGroupUpdate) (*Response, error)

	// Validate asks Forward which of commands the device types do not allow, without creating anything (POST
	// .../custom-command-groups?action=validate; EDIT_CUSTOM_COLLECTION_GROUPS). An empty result means every command is accepted.
	Validate(ctx context.Context, networkID string, deviceConnTypes []string, commands []string) ([]UnsupportedCustomCommand, *Response, error)
}

// CloudManagedSetups is the CloudManagedSetups API family: what Client.CloudManagedSetups sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CloudManagedSetups interface {
	// CreateMist adds a Mist setup. Idempotency is the caller's: Forward refuses a
	// duplicate name, so List first.
	CreateMist(ctx context.Context, networkID string, input NewMistSetup) (*MistSetup, *Response, error)

	// DeleteMist removes a setup by name. A 404 is success: the desired state,
	// that the setup is absent, already holds.
	DeleteMist(ctx context.Context, networkID string, setupName string) (*Response, error)

	// DiscoverMist asks the bound collector to enumerate the org's APs. Requires
	// an ONLINE collector (400 offline, 404 unbound) and blocks up to a minute;
	// pass a ctx with at least DiscoverTimeout. Best-effort for collection --
	// an undiscovered setup with an empty Hosts filter still collects every AP.
	DiscoverMist(ctx context.Context, networkID string, setupName string) (*MistDiscovery, *Response, error)

	// ListMist returns the network's Mist setups.
	ListMist(ctx context.Context, networkID string) ([]MistSetup, *Response, error)
	// ListMeraki returns the network's Meraki setups. Forward lists every cloud-managed setup on one route; this keeps those whose
	// type is MERAKI. GET /api/networks/{id}/cloud-managed-setups. Preview.
	ListMeraki(ctx context.Context, networkID string) ([]MerakiSetup, *Response, error)

	// GetMeraki returns one Meraki setup. A setup of another type is an error, never decoded as Meraki.
	// GET /api/networks/{id}/cloud-managed-setups/{name}. Preview.
	GetMeraki(ctx context.Context, networkID, setupName string) (*MerakiSetup, *Response, error)

	// CreateMeraki adds a Meraki setup. Forward refuses a duplicate name, so list first. Forward requires a test result; when
	// input.TestResult is nil it is sent as the current time with no error. POST .../cloud-managed-setups?type=MERAKI. Preview.
	CreateMeraki(ctx context.Context, networkID string, input NewMerakiSetup) (*MerakiSetup, *Response, error)

	// PatchMeraki changes part of a Meraki setup (JsonProp semantics; see MerakiSetupPatch).
	// PATCH .../cloud-managed-setups/{name}?type=MERAKI. Preview.
	PatchMeraki(ctx context.Context, networkID, setupName string, patch MerakiSetupPatch) (*MerakiSetup, *Response, error)

	// TestMeraki tests Meraki connectivity with the given credentials through the collector (204 on success).
	// POST .../cloud-managed-setup-tests?type=MERAKI. Preview.
	TestMeraki(ctx context.Context, networkID string, test MerakiConnectivityTest) (*Response, error)

	// DiscoverMeraki asks the collector to enumerate the account's devices. Forward answers when the collector task ends, so pass a
	// ctx with at least DiscoverTimeout. POST .../cloud-managed-setups/{name}?action=discover&type=MERAKI. Preview.
	DiscoverMeraki(ctx context.Context, networkID, setupName string) (*MerakiDiscovery, *Response, error)

	// DeleteMeraki removes a Meraki setup by name. A 404 is success: the setup is already absent.
	// DELETE .../cloud-managed-setups/{name}. Preview.
	DeleteMeraki(ctx context.Context, networkID, setupName string) (*Response, error)
}

// SaseSetups is the SaseSetups API family: what Client.SaseSetups sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type SaseSetups interface {
	// Create adds a setup and returns it as saved (no test results yet).
	// Forward saves the setup FIRST and then queues a tenant test and a test of
	// every connector on the collector (ZscalerConnectivityService); the controller
	// is not transactional, so when no collector is online or supported that second
	// step answers 400 AFTER the setup exists. After any Create error, Get or List
	// before retrying: a retry meets the duplicate-name refusal. The tests run
	// asynchronously and the call does not wait for them; poll Get until
	// TestResult.SavedAt appears. Create is not idempotent.
	Create(ctx context.Context, networkID string, setup ZscalerSetup) (*ZscalerSetup, *Response, error)

	// Delete removes a setup. A 404 for the setup is success: the desired state,
	// that it is absent, already holds. A 404 because this deployment does not
	// serve the route (ErrEndpointNotServed) is still an error.
	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// Get returns one setup; a missing name is a 404.
	Get(ctx context.Context, networkID string, name string) (*ZscalerSetup, *Response, error)

	// List returns the network's setups. The response is {"setups":[...]}.
	List(ctx context.Context, networkID string) ([]ZscalerSetup, *Response, error)

	// NewlyDiscovered lists connectors the ZPA API reported that are not in any
	// setup yet (GET ...?view=newlyDiscovered). Each connector's setup name says
	// which setup's tenant reported it.
	NewlyDiscovered(ctx context.Context, networkID string) ([]NewlyDiscoveredConnector, *Response, error)

	// Patch changes a setup and returns it. Renaming (a patch Name differing from
	// name) replaces the setup under the new name.
	Patch(ctx context.Context, networkID string, name string, patch ZscalerSetupPatch) (*ZscalerSetup, *Response, error)

	// Test queues a tenant probe before any setup exists (POST ...?action=test,
	// 204). The probe runs on the collector afterwards and nothing is stored or
	// returned over REST (Forward reports it only as a server-sent event), so a
	// 204 says the probe was queued, not that the tenant works. Errors are body
	// validation and "no online or supported collector"; to learn whether a tenant
	// works, Create the setup and read TestResult.
	Test(ctx context.Context, networkID string, input ZscalerConnectivityTest) (*Response, error)

	// TestConnector queues a test of one App Connector (204), asynchronously. Forward answers 404 for an
	// unknown connectorId and 400 when the connector is not SSH.
	TestConnector(ctx context.Context, networkID string, name string, connectorID string) (*Response, error)

	// TestConnectors queues a test of every SSH App Connector of a saved setup
	// (204); like TestSetup it returns first and each outcome lands on its
	// connector's TestResult later.
	TestConnectors(ctx context.Context, networkID string, name string) (*Response, error)

	// TestSetup queues a re-test of a saved setup's tenant (204). It returns before
	// the probe runs; the collector saves the outcome onto the setup later, so poll
	// Get until TestResult.SavedAt moves. Needs an online collector (400 otherwise).
	TestSetup(ctx context.Context, networkID string, name string) (*Response, error)
}

// Decorators is the Decorators API family: what Client.Decorators sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Decorators interface {
	// Decorate runs the named decorators for the given entities on a snapshot, which is
	// how to SEE what a decorator attaches. POST /api/networks/{id}/decorations
	// ?type=&snapshotId=. SnapshotID empty uses the network's latest processed snapshot.
	//
	// The result lists an entity under a decorator only when the decorator has rows for
	// it (status SUCCESS, rows non-empty) or when something failed (a failure status,
	// no rows); an entity the query has nothing for is simply absent, not an empty
	// result. A user decorator runs whether or not it is enabled; a system-managed one
	// only when enabled; a name that does not exist, or whose query the licence tier
	// denies, is silently absent. Name only decorators of the given entity type:
	// Forward does not check that they match ?type=.
	//
	// Forward answers when its search worker finishes, up to its 5-minute limit, so the
	// call uses a DecorateTimeout HTTP timeout. Entities of the wrong shape for the type
	// are a 400 ("Invalid path descriptor", an interface not in "<device> <port>" form);
	// an unknown device or a non-numeric host id is an error from Forward whose status
	// is unverified.
	Decorate(ctx context.Context, networkID string, snapshotID string, entityType DecoratorEntityType, req DecorateRequest) ([]EntityDecoration, *Response, error)

	// Delete removes a user decorator (DELETE /api/decorators/{name}, 204). A
	// system-managed name is refused by Forward with 400 (use SetManagedEnabled).
	// A 404 for the decorator is success: the desired state, that it is absent,
	// already holds. A 404 because this deployment does not serve the route
	// (ErrEndpointNotServed) is still an error.
	Delete(ctx context.Context, name string) (*Response, error)

	// List returns the org's decorators: Forward's four system-managed ones (named
	// "fwd-...", with their enabled flag for this org) followed by the org's own.
	// GET /api/decorators.
	List(ctx context.Context) ([]Decorator, *Response, error)

	// Put adds or merges a user decorator and returns it (PATCH /api/decorators/{name}).
	// Forward answers 400 for a system-managed ("fwd-") name, an unknown query id, a
	// type that differs from the existing decorator's, or missing required fields on a
	// new one, and 403 when the org's licence tier does not permit the query (only
	// checked while the decorator is enabled). The columns are NOT checked against the
	// query's output here: a mismatch is saved and, for DEVICE, INTERFACE and HOST
	// decorators, reported by Decorate as DecorationInvalidDecorator.
	Put(ctx context.Context, name string, put DecoratorPut) (*Decorator, *Response, error)

	// SetManagedEnabled turns one of Forward's system-managed decorators on or off
	// for the org (PATCH /api/system-managed-decorators/{name}?enabled=, 204).
	// Enabling an unknown name is a 404 and needs the licence tier to permit the
	// underlying query (403); disabling an unknown name is accepted (204) and does
	// nothing.
	SetManagedEnabled(ctx context.Context, name string, enabled bool) (*Response, error)
}

// Trace is the Trace API family: what Client.Trace sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Trace interface {
	// Facets lists the distinct paths a flow takes, grouped by trace type. An empty
	// snapshotID uses the network's latest. A predicted snapshot id is accepted by
	// Forward's snapshot resolution but was not run live here.
	//
	// This is a read sent as a POST, so a retry policy resends it only after a 429
	// or 503 or a failure to connect.
	Facets(ctx context.Context, networkID string, snapshotID string, q TraceQuery, opts TraceFacetsOptions) (*TraceFacets, *Response, error)

	// FacetsQuery is Facets for a TracePathQuery: the distinct paths the GUI Search page's query takes (POST /api/networks/{id}/trace-facets).
	// An empty snapshotID uses the network's latest. See TraceFacets for the response, and Total and PossibleLocations for the typed readings
	// of totalResults and possibleIpLocations. Read-only, sent as a POST, so a retry policy resends it only after a 429 or 503 or a failure to
	// connect. A TraceQuery-based Facets is unchanged.
	FacetsQuery(ctx context.Context, networkID string, snapshotID string, q TracePathQuery, opts TraceFacetsOptions) (*TraceFacets, *Response, error)

	// Trace returns the per-hop detail of one path chosen from Facets, with the
	// same query. An empty snapshotID uses the network's latest.
	Trace(ctx context.Context, networkID string, snapshotID string, q TraceQuery, sel TraceSelection) (*TraceResult, *Response, error)

	// TraceFor is Trace for a TracePathQuery: the per-hop detail of ONE path chosen from FacetsQuery's PathGroups, with the same query, so a
	// query with a Chain (through, ingress or egress hops), a To location or headers, or the PERMIT_ALL mode can be traced as well as counted
	// (POST /api/networks/{id}/trace, the body the GUI Search page sends). An empty snapshotID uses the network's latest. Select the path with
	// TracePathGroup.Selection. Source-only for every shape FacetsQuery marks source-only; the /trace route itself is the one Trace uses.
	TraceFor(ctx context.Context, networkID string, snapshotID string, q TracePathQuery, sel TraceSelection) (*TraceResult, *Response, error)
}

// SyntheticNodes is the SyntheticNodes API family: what Client.SyntheticNodes sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type SyntheticNodes interface {
	// Backdate applies the network's CURRENT configuration of this kind of synthetic device from snapshotID onward and INVALIDATES every
	// snapshot from that one on, so they reprocess and their answers are unavailable until they finish -- the Forward UI's "backdate". POST
	// ?op=backdate&snapshotId= on the kind's collection route, or the internet node's own route (e.g. InternetNodeService.backdateTo:
	// repo.backdateTo, then invalidateAffectedSnapshots from the snapshot's creation instant with no end). Needs MANAGE_COLLECTION_SOURCES and
	// INVALIDATE_SNAPSHOTS. Marked SyntheticNodes.Backdate<Kind> so a hook can recognise it as destructive. Preview: not in the published spec.
	Backdate(ctx context.Context, networkID string, kind SyntheticNodeKind, snapshotID string) (*Response, error)

	// CompatibleQueries lists the saved NQE queries a node of this kind can use: parameterless queries whose row type is the kind's connection
	// record, typed against the network's latest processed snapshot (empty when the network has none). Preview: GET
	// /api/synthetic-device-queries?type=&networkId= (SyntheticDeviceController; served on primary 15398425a69 and stable 67e89c87124).
	CompatibleQueries(ctx context.Context, networkID string, kind SyntheticNodeKind) ([]SyntheticDeviceQuery, *Response, error)

	// ComputeQuery runs queryID as Forward would for a node of this kind and returns the connections it would generate, or the error it hits,
	// without changing anything: a dry run for SetQuery. Forward runs the query's last commit against the network's latest processed snapshot,
	// and checks its row type against the kind's connection record (InetConnection for internet and intranet nodes, L3VpnConnection for L3
	// VPNs and adjacent networks, L2VpnConnection for L2 VPNs). Preview: POST ?action=computeNqeBasedConnections&queryId= on the kind's
	// collection route, or the internet node's own route (served on primary 15398425a69 and stable 67e89c87124).
	ComputeQuery(ctx context.Context, networkID string, kind SyntheticNodeKind, queryID string) (*SyntheticQueryResult, *Response, error)

	// Delete removes one synthetic node. A 404 is success. The internet node
	// cannot be deleted -- there is one per network and no route for it.
	Delete(ctx context.Context, networkID string, kind SyntheticNodeKind, name string) (*Response, error)

	// Get returns one synthetic node, or (nil, nil) when it does not exist.
	// Absence is not an error: callers read before writing precisely to find out
	// whether to create.
	Get(ctx context.Context, networkID string, kind SyntheticNodeKind, name string) (*SyntheticNode, *Response, error)

	// InternetConnectionSuggestions returns the connections Forward suggests for the internet node, computed from the latest processed snapshot
	// (GET /api/networks/{id}/internet-node/connection-suggestions): the GUI's "suggested connections". It is read-only; add one with Put or Patch on the
	// internet node. Preview: not in the published spec.
	InternetConnectionSuggestions(ctx context.Context, networkID string) ([]InternetConnectionSuggestion, *Response, error)

	// List returns the synthetic nodes of a kind. The internet node has no
	// collection and yields nothing.
	//
	// Forward returns them as an ARRAY under a per-kind key -- {"l3Vpns": [...]},
	// {"intranetNodes": [...]} -- which is worth stating because the Java field
	// behind it is a MAP keyed by name; the serialized form is the array, pinned
	// by Forward's own L3VpnListTest. A bare array is accepted too.
	List(ctx context.Context, networkID string, kind SyntheticNodeKind) ([]SyntheticNode, *Response, error)

	// Put creates or replaces one synthetic node.
	Put(ctx context.Context, networkID string, kind SyntheticNodeKind, name string, node SyntheticNode) (*Response, error)

	// SetInternetExcludedSubnets REPLACES the internet node's excluded subnets -- public CIDRs that Forward will not treat as reachable through
	// the internet node -- with subnets, and returns the node as Forward now holds it. An empty slice clears them; nil is refused, so a caller
	// cannot clear the list by forgetting to fill it (Forward reads an absent or null value as "leave unchanged"). Forward validates each entry
	// (a valid subnet with its host bits zero, public addresses only) and applies the change from the next processed snapshot. PATCH
	// /api/networks/{id}/internet-node with only {"subnetsToExclude": [...]} (published, InternetNodePatch).
	SetInternetExcludedSubnets(ctx context.Context, networkID string, subnets []string) (*SyntheticNode, *Response, error)

	// SetQuery points a node at a saved NQE query (queryID "Q_..." in the organization's library, "FQ_..." in Forward's) so Forward generates dynamic connections from its rows, or
	// clears it (queryID ""). It changes nothing else on the node, and returns the node as Forward now holds it, including QueryResult (check its
	// Error: a query of the wrong row type is accepted and reported there, not refused). Preview: PATCH {"queryId": ...} on the node route; the property
	// is hidden in Forward's spec (FWD-37393). Clearing sends an explicit null, which the server reads as "remove" (JsonProp).
	SetQuery(ctx context.Context, networkID string, kind SyntheticNodeKind, name string, queryID string) (*SyntheticNode, *Response, error)
}

// Credentials is the Credentials API family: what Client.Credentials sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Credentials interface {
	CreateCLI(ctx context.Context, networkID string, credential CLICredentialRequest) (*CLICredential, *Response, error)

	CreateHTTP(ctx context.Context, networkID string, credential HTTPCredentialRequest) (*HTTPCredential, *Response, error)

	// CreateOrgHTTP adds an HTTP credential to the organization (POST /api/http-credentials; org EDIT_CREDENTIALS). The body is the one the
	// network-level CreateHTTP sends (NewHttpCredential): an empty Type is LOGIN, and Name and Password are required. For a Tenable VM scanner
	// make a LOGIN credential with the access key as Username and the secret key as Password. Forward assigns the id and answers 400 if the
	// name is already taken in the org.
	CreateOrgHTTP(ctx context.Context, credential HTTPCredentialRequest) (*OrgHTTPCredential, *Response, error)

	// CreateSNMP creates an SNMP credential using a version-specific payload.
	// Preview: this route is not in the published OpenAPI contract.
	CreateSNMP(ctx context.Context, networkID string, credential SNMPCredentialRequest) (*SNMPCredential, *Response, error)

	// CreateSNMPCredential is the typed form of CreateSNMP. Prefer it: the map
	// form cannot tell V2C's communityString from V3's authSettings, and sending
	// the wrong half is a 400 that names nothing.
	CreateSNMPCredential(ctx context.Context, networkID string, credential SNMPCredentialRequest) (*SNMPCredential, *Response, error)

	DeleteCLI(ctx context.Context, networkID string, credentialID string) (*Response, error)

	DeleteHTTP(ctx context.Context, networkID string, credentialID string) (*Response, error)

	// DeleteOrgHTTP removes a credential (DELETE /api/http-credentials/{credentialId}; org EDIT_CREDENTIALS; 204). At origin/master Forward
	// refuses it with 400 while a vulnerability scanner uses it (OrgHttpCredentialService.validateCredentialUnused: "HTTP Credential ... is
	// used by N sources"); an older Forward build may not have that check, so do not rely on it as a safety net. An unknown id is a 404. Delete* name: Skyforge's destructive-SDK catalogue must list it.
	DeleteOrgHTTP(ctx context.Context, credentialID string) (*Response, error)

	DeleteSNMP(ctx context.Context, networkID string, credentialID string) (*Response, error)

	GetCLI(ctx context.Context, networkID string, credentialID string) (*CLICredential, *Response, error)

	GetHTTP(ctx context.Context, networkID string, credentialID string) (*HTTPCredential, *Response, error)

	// GetOrgHTTP reads one credential (GET /api/http-credentials/{credentialId}; org VIEW_CREDENTIALS). An unknown id is a 404.
	GetOrgHTTP(ctx context.Context, credentialID string) (*OrgHTTPCredential, *Response, error)

	ListCLI(ctx context.Context, networkID string) ([]CLICredential, *Response, error)

	ListHTTP(ctx context.Context, networkID string) ([]HTTPCredential, *Response, error)

	// ListOrgHTTP lists the organization's HTTP credentials (GET /api/http-credentials; org VIEW_CREDENTIALS).
	ListOrgHTTP(ctx context.Context) ([]OrgHTTPCredential, *Response, error)

	// ListSNMP returns the unpublished, version-specific SNMP credential objects.
	ListSNMP(ctx context.Context, networkID string) ([]SNMPCredential, *Response, error)

	// ListSNMPCredentials is the typed form of ListSNMP. It accepts the wrapped
	// envelopes Forward uses across list routes as well as a bare array.
	ListSNMPCredentials(ctx context.Context, networkID string) ([]SNMPCredential, *Response, error)

	// UpdateCLI patches version-specific credential fields. A nil map value emits
	// JSON null; password values are never exposed to client hooks.
	UpdateCLI(ctx context.Context, networkID string, credentialID string, patch CLICredentialPatch) (*CLICredential, *Response, error)

	UpdateHTTP(ctx context.Context, networkID string, credentialID string, patch HTTPCredentialPatch) (*Response, error)

	// UpdateHTTPWithResult decodes the updated credential when the endpoint
	// returns one and falls back to the typed request fields for an empty 2xx body.
	UpdateHTTPWithResult(ctx context.Context, networkID string, credentialID string, patch HTTPCredentialRequest) (*HTTPCredential, *Response, error)

	// UpdateOrgHTTP changes part of a credential (PATCH /api/http-credentials/{credentialId}; org EDIT_CREDENTIALS) and returns the stored
	// credential, unlike the network-level update, which answers 204. Nil fields are not sent, so a field cannot be cleared with this call (an
	// explicit empty string is a 400 for a username). Forward answers 400 for a name already taken.
	UpdateOrgHTTP(ctx context.Context, credentialID string, patch HTTPCredentialPatch) (*OrgHTTPCredential, *Response, error)

	UpdateSNMP(ctx context.Context, networkID string, credentialID string, patch map[string]any) (*Response, error)

	// UpdateSNMPCredential changes part of a stored SNMP credential (PATCH /api/networks/{id}/snmpCredentials/{credentialId};
	// CredentialController.patchSnmpCredential, SnmpCredentialUpdate; EDIT_DEVICE_CREDENTIALS; 204; Preview: not in the published
	// spec). It is the typed form of
	// UpdateSNMP, whose map the caller has to spell right. Forward checks the merged credential, so a patch that leaves a V3
	// credential without a username, or a V2C one with authSettings, is a 400.
	UpdateSNMPCredential(ctx context.Context, networkID string, credentialID string, patch SNMPCredentialPatch) (*Response, error)
}

// CollectorTasks is the CollectorTasks API family: what Client.CollectorTasks sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CollectorTasks interface {
	Get(ctx context.Context, taskID string) (*CollectorTask, *Response, error)

	// GetWithSubTasks returns a task with its subtasks. Forward returns at most
	// maxSubTasks of them (default 1000 when 0): the RUNNING, FAILED, TIMED_OUT
	// and CANCELED ones first, then SUCCEEDED ones to fill the rest, and QUEUED
	// ones never. So a short list is a complete picture of what went wrong, but
	// not of what succeeded; raise maxSubTasks past the device count for every
	// device's start and finish. GET /api/collector-tasks/{taskId}?for=ui&max=
	// (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
	GetWithSubTasks(ctx context.Context, taskID string, maxSubTasks int) (*CollectorTaskWithSubTasks, *Response, error)

	List(ctx context.Context, options CollectorTaskListOptions) ([]CollectorTask, *Response, error)

	// Progress reports active network collection work using the network-filtered
	// collector-task route present on some appserver builds.
	Progress(ctx context.Context, networkID string) (*CollectionProgress, *Response, error)

	// SnapshotProgress returns how a snapshot's collection ran over time: queued,
	// running and finished subtasks, and the concurrency in use against its limit,
	// at each sample. GET /api/collector-tasks?snapshotId=
	// (CollectorTaskController.getSnapshotTaskProgress; VIEW_NETWORK_AND_SNAPSHOTS,
	// snapshot at the START stage). Preview: not in the published spec.
	SnapshotProgress(ctx context.Context, snapshotID string) (*SnapshotTaskProgress, *Response, error)

	// Start begins a network collection and returns its durable task identifier.
	Start(ctx context.Context, networkID string) (string, *Response, error)

	// Stop cancels or skips an in-progress task. SKIP creates a snapshot from data
	// already collected; CANCEL does not.
	Stop(ctx context.Context, taskID string, action string, note string) (*Response, error)

	// SubTasksAt returns the subtasks of a task that were in progress at an
	// instant -- started by then and not finished before it -- oldest start first.
	// It reads every subtask, with no cap, so it stays complete on a collection of
	// tens of thousands of devices. GET /api/collector-tasks/{taskId}?view=subtasks
	// &at= (VIEW_COLLECTOR_TASK_QUEUES plus permission to view the task). Preview.
	SubTasksAt(ctx context.Context, taskID string, at time.Time) ([]CollectorSubTask, *Response, error)
}

// CloudAccounts is the CloudAccounts API family: what Client.CloudAccounts sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CloudAccounts interface {
	AWSAssumeRoleExternalID(ctx context.Context, networkID string) (string, *Response, error)

	Create(ctx context.Context, networkID string, request CloudAccountRequest) (*CloudAccount, *Response, error)

	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// Discover asks the setup's collector to list the accounts, subscriptions or
	// projects its stored credentials can see. POST
	// /api/networks/{networkId}/cloudAccounts/{name}/discovery
	// (getExistingCloudAccountDiscoveryResult, Preview; MANAGE_COLLECTION_SOURCES)
	// with no body, so it uses the stored setup unchanged. It changes no setup, but
	// it records a collector task, needs the collector online, and can take
	// minutes. For a CAP setup it lists ALL portal accounts, whether the setup's
	// filter pins them or not (ID is the account id), so a filter can be checked
	// against pairs the portal really offers: a wrong id or role matches nothing,
	// with no error. Azure and GCP return subscriptions and projects. An AWS setup
	// without CAP is not supported and reports Error "UNSUPPORTED_VENDOR". A failed
	// discovery is an HTTP 200 whose Error is not "NONE", not a Go error. A 503
	// from Forward's own timeout is retried like any 503 if a RetryPolicy is set,
	// which starts another discovery; leave retries off for this call if that
	// matters.
	Discover(ctx context.Context, networkID string, name string) (*CloudDiscovery, *Response, error)

	// GetAWSCapCredential reads a CAP credential by the id an AWS account carries
	// in awsCapCredentialId (CloudAccount.Raw). GET
	// /api/networks/{networkId}/aws-cap-credentials/{credentialId}
	// (CloudAccountController.getAwsCapCredential, Preview; VIEW_DEVICE_CREDENTIALS).
	// A caller without that permission gets Forward's typed 403 (ErrPermissionDenied,
	// MissingPermission), and an unknown id its 404.
	//
	// Forward's body carries clientCertificateBase64, the client's secret .p12
	// certificate; it is read off the wire and dropped here, never stored in the
	// result or in an error.
	GetAWSCapCredential(ctx context.Context, networkID string, credentialID string) (*AWSCapCredential, *Response, error)

	List(ctx context.Context, networkID string) ([]CloudAccount, *Response, error)

	// NewlyDiscovered lists, per cloud setup name, the accounts collection found
	// that the setup does not collect from yet. GET
	// /api/networks/{networkId}/cloudAccounts?view=newlyDiscovered
	// (CloudAccountController.getNewlyDiscoveredAccounts, Preview;
	// VIEW_COLLECTION_SOURCES). It reads what the last snapshot recorded; it does
	// not ask the cloud. It covers only multi-account setups (an AWS setup with
	// assumeRoleInfos, Azure, GCP). It does NOT cover AWS C2S Access Portal (CAP)
	// accounts a filter leaves out: those never reach a snapshot, and a CAP setup
	// in "collect all" mode (no assumeRoleInfos) is not in the result at all. Use
	// Discover for the portal's accounts.
	NewlyDiscovered(ctx context.Context, networkID string) (map[string][]DiscoveredCloudAccount, *Response, error)

	// Patch updates part of a cloud account and returns it. PATCH /api/networks/{networkId}/cloudAccounts/{accountName}.
	// Only the properties set on patch are sent; see CloudAccountPatch.
	Patch(ctx context.Context, networkID string, name string, patch CloudAccountPatch) (*CloudAccount, *Response, error)

	// Test triggers the account connectivity probe.
	Test(ctx context.Context, networkID string, name string) (*Response, error)

	Update(ctx context.Context, networkID string, name string, request CloudAccountRequest) (*CloudAccount, *Response, error)

	UpdateCredential(ctx context.Context, networkID string, name string, request CloudAccountCredentialRequest) (*Response, error)
}

// Webhooks is the Webhooks API family: what Client.Webhooks sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Webhooks interface {
	Create(ctx context.Context, request WebhookRequest) (*Response, error)

	Delete(ctx context.Context, name string) (*Response, error)

	// List returns configured webhooks and the last test result for each, keyed by
	// webhook name.
	List(ctx context.Context) ([]Webhook, map[string]WebhookTestResult, *Response, error)

	// TestNew tests a webhook without persisting it and returns the dynamic test result.
	TestNew(ctx context.Context, request WebhookRequest) (json.RawMessage, *Response, error)

	Update(ctx context.Context, name string, patch WebhookPatch) (*Response, error)
}

// Snapshots is the Snapshots API family: what Client.Snapshots sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Snapshots interface {
	// CollectionExceptions returns the collectors' exceptions for a snapshot. GET
	// /api/collector/exceptions?snapshotId= (CollectorLogController).
	CollectionExceptions(ctx context.Context, snapshotID string, options CollectionExceptionOptions) (*CollectionExceptions, *Response, error)

	// CollectionLog writes up to maxBytes of a snapshot's CLI execution log (the
	// commands the collector ran) to dst; an empty log writes nothing. GET
	// /api/snapshots/{id}/collection-log (SnapshotExportController.getSnapshotLog).
	CollectionLog(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error)

	// CollectionMetrics returns a snapshot's per-device collection metrics -- the
	// one source of per-device collection slowness. An empty snapshotID uses the
	// latest processed snapshot. GET /api/networks/{id}/collection-metrics
	// (MetricController); 404 when the snapshot has none, such as an import.
	CollectionMetrics(ctx context.Context, networkID string, snapshotID string) (*SnapshotCollectionMetrics, *Response, error)

	// ComputeAdvancedReachability asks Forward to compute advanced reachability for a snapshot -- the analysis internet-exposure and other
	// security answers need, which a reprocessed or imported snapshot can be left without (AdvancedReachabilityState UNPROCESSED). It returns
	// as soon as Forward accepts (204) and runs asynchronously; from 26.11 (FWD-61031) it also reruns a computation that failed, was canceled or timed out
	// (it still does nothing for one that succeeded or is in progress); watch the snapshot's AdvancedReachabilityState or Snapshots.Progress. POST
	// /api/snapshots/{id}?action=computeAdvancedReachability (published, computeAdvancedReachability; SnapshotController on primary 15398425a69
	// and stable 67e89c87124). The handler requires the REACHABILITY stage, so a snapshot that has not finished it is ErrSnapshotNotProcessed
	// (409 SNAPSHOT_UNAVAILABLE) and one whose processing failed is ErrSnapshotProcessingFailed. Compute-heavy, but it changes no data.
	ComputeAdvancedReachability(ctx context.Context, snapshotID string) (*Response, error)

	// Delete removes a snapshot.
	Delete(ctx context.Context, snapshotID string) (*Response, error)

	// Download writes an exported snapshot ZIP to dst.
	Download(ctx context.Context, snapshotID string, dst io.Writer) (*Response, error)

	// Exceptions returns the exceptions Forward recorded while parsing, modeling
	// and indexing a snapshot. GET /api/snapshots/{snapshotId}/exceptions?view=json
	// (SnapshotController.getExceptionsJson, served on primary 15398425a69 and
	// stable 67e89c87124). It needs the DEBUG_SNAPSHOTS network permission; a
	// caller without it gets a 403.
	Exceptions(ctx context.Context, snapshotID string) ([]SnapshotException, *Response, error)

	// ExceptionsText writes up to maxBytes of a snapshot's processing exceptions
	// as Forward's plain-text report (device name, then full stack trace) to dst.
	// GET /api/snapshots/{id}/exceptions (SnapshotController.getExceptions); needs
	// DEBUG_SNAPSHOTS. It reads the same stored exceptions as Exceptions, so when
	// that is empty this is too: Forward can mark a device PARSER_EXCEPTION
	// without storing an exception (SnapshotParsingHelper.determineProcessingError).
	ExceptionsText(ctx context.Context, snapshotID string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error)

	// Export streams a snapshot's ZIP to dst and returns the bytes written, so a
	// large export never sits in memory (ExportSubset returns the whole ZIP as a
	// slice). POST /api/snapshots/{snapshotId}[?only=CONFIG] with a JSON body of
	// includeDevices or excludeDevices, obfuscationKey and obfuscateNames
	// (exportSnapshotSubset, published; VIEW_NETWORK_AND_SNAPSHOTS; the snapshot
	// must have reached the STORED_FILES stage).
	//
	// Nothing is retried, because bytes may already be in dst: write to a
	// temporary file and move it into place on success. If the response is not a
	// ZIP the call returns an error naming the start of what came back, and dst
	// holds that body, so discard it. A snapshot that is not ready yields the typed
	// errors (ErrSnapshotNotProcessed and friends) before any body is written.
	Export(ctx context.Context, snapshotID string, options SnapshotExportOptions, dst io.Writer) (int64, *Response, error)

	// ExportSubset exports selected devices from a snapshot as a ZIP, returned
	// whole in memory. For a large snapshot, obfuscation or a whole-snapshot export
	// use Export, which streams to a writer. Forward can
	// temporarily reject a fresh snapshot until processing finishes; only the typed
	// snapshot-not-processed error (or a successful non-ZIP placeholder response)
	// is retried. Other API errors return immediately.
	ExportSubset(ctx context.Context, snapshotID string, options SnapshotSubsetRequest) ([]byte, *Response, error)

	// ExportSubsetCompatibility is the explicit changedemo retry policy: retry
	// every non-ZIP response, including HTTP errors, for the configured bounded
	// attempts. It is opt-in and applies only to this read-like export POST.
	ExportSubsetCompatibility(ctx context.Context, snapshotID string, options SnapshotSubsetRequest) ([]byte, *Response, error)

	Favorite(ctx context.Context, snapshotID string) (*Response, error)

	// FileExcerpts reads line ranges of a device's stored file in a snapshot (GET /api/snapshots/{snapshotId}/files/{device},{file}?lines=a-b;
	// VIEW_COLLECTED_FILES; the snapshot must have its stored files). At least one range is required. Forward names the file with the
	// device in front, "<device>,<file>" (this method builds it; a global file with no device cannot be read this way), and on a predicted
	// snapshot the answer also says how many lines the prediction changed (see FileExcerpts.PredictedFileInfo). Several ranges go in ONE
	// comma-separated lines value ("7-45,60-80"), which is the form Forward's converter takes; Forward needs them sorted and refuses overlaps
	// (400), so this method sorts them and refuses overlaps first. A range that starts past the end of the file is a 400; one that ends past
	// it is cut short.
	FileExcerpts(ctx context.Context, snapshotID string, deviceName string, fileName string, ranges []FileLines) (*FileExcerpts, *Response, error)

	// Invalidate clears computed snapshot data without starting inline reprocess.
	Invalidate(ctx context.Context, snapshotID string) (*SnapshotStateTransition, *Response, error)

	// List returns snapshots for a network.
	List(ctx context.Context, networkID string, options SnapshotListOptions) ([]Snapshot, *Response, error)

	// ListDocument is the operation-specific typed binding for consumers whose
	// public contract intentionally preserves Forward's JSON envelope.
	ListDocument(ctx context.Context, networkID string, options SnapshotListOptions) (JSONDocument, *Response, error)

	// Logs writes up to maxBytes of a snapshot's collection log -- every device's,
	// or one device's -- to dst. GET /api/snapshots/{id}/logs
	// (SnapshotExportController.getSnapshotLogs). Forward answers 404 for a
	// snapshot with no collection map, such as an import or a fork.
	Logs(ctx context.Context, snapshotID string, options SnapshotLogOptions, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error)

	// MergeIntoLatest uploads one snapshot ZIP and merges it with the network's LATEST snapshot into a NEW snapshot; it does not overwrite
	// the latest one (POST /api/networks/{id}/snapshots?action=merge&mergeType=..., multipart "file"; SnapshotController.mergeAndCreateSnapshot,
	// UPLOAD_SNAPSHOT; SnapshotService.mergeSnapshotZipIntoLatestSnapshot). The body is streamed, so a large ZIP is not buffered.
	//
	// Answers: 202 with the new snapshot (state UNPACKING; poll it as after Upload). With mergeType NONE and a device in both, a 409
	// "Conflicting devices" whose error satisfies errors.Is(err, ErrSnapshotMergeConflict) and carries the sorted names in
	// ErrorResponse.ConflictDevices; pick REPLACE_EXISTING or PRESERVE_EXISTING to resolve it. A 400 when synthetic objects collide (L3 VPN,
	// L2 VPN, intranet nodes, WAN circuits and encryptors need unique connection names) or a name is used by a device or synthetic object in
	// more than one ZIP, and a 503 when disk or backup space is short. Forward refuses an empty file; MergeFileIntoLatest checks that
	// before sending, since a stream's size is unknown here. Preview: from the controller at release 26.11.0-10; not run against a live Forward.
	MergeIntoLatest(ctx context.Context, networkID string, file SnapshotUploadFile, options SnapshotMergeOptions) (*SnapshotMergeResult, *Response, error)

	// Metrics returns a snapshot's collection and processing health: device and
	// endpoint success/failure counts, failures by error name, durations and index
	// statuses. GET /api/snapshots/{snapshotId}/metrics (SnapshotController,
	// served on primary 15398425a69 and stable 67e89c87124). Forward answers 404
	// when the snapshot has no metrics, and ErrSnapshotNotProcessed before
	// processing has started.
	Metrics(ctx context.Context, snapshotID string) (*SnapshotMetrics, *Response, error)

	// ProcessEstimate returns Forward's per-stage processing-time estimate for a
	// snapshot. GET /api/snapshots/{snapshotId}/processEstimate (SnapshotController;
	// served on primary 15398425a69 and stable 67e89c87124). Preview: not in the
	// published spec.
	ProcessEstimate(ctx context.Context, snapshotID string) (*SnapshotProcessEstimate, *Response, error)

	// ProcessingQueue lists the organization's snapshot processing queue (GET /api/snapshot-processing-queue,
	// SnapshotController.getSnapshotProcessingQueue; the org operation VIEW_SNAPSHOT_QUEUE), oldest request first: the snapshots waiting
	// for one of the org's in-progress processing slots. It is the admission queue in front of compute, not the compute queue: it holds
	// only snapshot processing, reprocessing and import (never Predict, NQE or search tasks), and a snapshot that leaves it can still wait
	// in the job scheduler. It is always empty when the deployment has THROTTLE_SNAPSHOT_PROCESSING off, so an empty list does not mean the
	// org is idle. Forward has no route for the per-org compute worker cap; running and queued tasks with their worker class are only in the
	// system-admin debug view, Jobs.DebugView.
	ProcessingQueue(ctx context.Context) ([]SnapshotProcessingQueueEntry, *Response, error)

	// Progress returns a snapshot's processing progress, stage by stage. GET
	// /api/snapshots/{snapshotId}/progress (SnapshotController; served on primary
	// 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
	Progress(ctx context.Context, snapshotID string) (*SnapshotProgress, *Response, error)

	// Reprocess invalidates and reprocesses an existing snapshot.
	//
	// Preview: this action route is used by current Forward builds but is not part
	// of the published snapshot contract.
	Reprocess(ctx context.Context, snapshotID string) (*SnapshotStateTransition, *Response, error)

	// ReprocessInvalidated reprocesses a snapshot that is already invalidated
	// (Snapshots.Invalidate, or any other invalidation); it does not invalidate
	// first, which is what Reprocess does. POST /api/snapshots/{id}?action=reprocess
	// answers 202 with the state change (SnapshotController.reprocessSnapshot,
	// Forward 26.11, FWD-61280; published). A snapshot that is not invalidated is a
	// 400 "must be invalidated before reprocessing". Unlike invalidation, which needs
	// the INVALIDATE_SNAPSHOTS permission, this needs only network view access, but an
	// org can set a minimum role for reprocessing (SnapshotReprocessMinimumRole;
	// verifyMinimumRoleRequirement refuses a caller below it). Not verified live: the
	// status a build before 26.11 answers for the unmapped action, or the refusal's
	// status and body. Reprocess works on every build.
	ReprocessInvalidated(ctx context.Context, snapshotID string) (*SnapshotStateTransition, *Response, error)

	// SetNote records a note against a snapshot.
	//
	// Collection takes no note, so a caller that wants one sets it afterwards.
	// The updated snapshot is returned.
	SetNote(ctx context.Context, snapshotID string, note string) (*Snapshot, *Response, error)

	// Unfavorite clears a snapshot's favorite flag (Favorite sets it).
	// PATCH /api/snapshots/{snapshotId}?action=unfavorite (SnapshotController.unfavoriteSnapshot).
	Unfavorite(ctx context.Context, snapshotID string) (*Response, error)

	// Upload imports one or more snapshot ZIP files. The multipart body is
	// streamed, so large snapshots are not buffered in memory by the SDK.
	Upload(ctx context.Context, networkID string, files []SnapshotUploadFile, options SnapshotUploadOptions) (*Snapshot, *Response, error)

	// UploadMergeCompatibility preserves the changedemo merge contract: any HTTP
	// status is decoded as a merge result, an empty body yields a zero result, and
	// a non-empty malformed body is an error. Prefer Upload for new code.
	UploadMergeCompatibility(ctx context.Context, networkID string, files []SnapshotUploadFile) (*Snapshot, *Response, error)
}

// NQE is the NQE API family: what Client.NQE sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type NQE interface {
	// CheckResult reads the rows an NQE check's result holds, page by page, from the result key the check reports (Check.NQEResultKey, from
	// Checks.List or Checks.Get): POST /api/nqe/queryResults?snapshotId=&resultKey= (NqeController.getNqeQueryResults, USE_NQE, with the
	// check's snapshot; the key is only valid for the snapshot it was computed on, and Forward answers a 400 otherwise). This is the
	// full list of a failing NQE check's violating rows, where CheckDiagnosis.Details is only a sample.
	//
	// Seen live (26.11.0-03, by forward-python-sdk): the answer is Forward's INTERNAL run result, not the published one:
	// {"type":{...schema},"diagnostics":[],"executionInfo":{...},"result":{"values":[row...],"totalNumValues":30,...}}, whose cells are typed:
	// a scalar is {"value":x[,"metadata":{...}]}, a collection a list of cells, and an enum or one-of {"type":"OS","alternative":{"value":"NXOS"}}.
	// CheckResult unwraps the cells (an enum or one-of comes back as its alternative's value, without the type tag), puts the rows in
	// Items and totalNumValues in TotalNumItems, so Truncated, RowsAny and DecodeNQE work as on any other result. A body in the published
	// shape (items and totalNumItems), as another build may send, decodes as is.
	//
	// options pages and sorts (Offset, Limit, SortBy, ColumnFilters); nil reads Forward's default first page. The body is the internal
	// NqeQueryOptions, which refuses ItemFormat (a 400 naming it), so ItemFormat is never sent from here. Compare TotalNumItems with the
	// rows read (NQEResult.Truncated) and page on. Preview: not in the published OpenAPI. Only NQE checks carry a result key; the other
	// check types have no equivalent route.
	CheckResult(ctx context.Context, snapshotID string, resultKey string, options *NQEOptions) (*NQEResult, *Response, error)

	// Diff compares a committed NQE query's results across two snapshots.
	Diff(ctx context.Context, beforeSnapshotID string, afterSnapshotID string, query NQEDiffRequest) (*NQEDiffResult, *Response, error)

	// ExplainPath is ExplainQuery for the query at path in the caller's own NQE
	// workspace: the draft when one exists, else the committed query. path is the
	// library path without the repository, as the UI sends it (for example
	// "/Operations/my query"). POST /api/internal/users/current/nqe/queries/explain
	// (NqeController.explainInUserWorkspace, Preview and internal; org
	// VIEW_NQE_LIBRARY). A draft at a path with no committed query yet resolves
	// (getUserEffectiveSourceSet reads the workspace change first and needs a
	// committed query only for an unedited path); a path the workspace has deleted,
	// moved away from, or that exists nowhere, is a 404.
	ExplainPath(ctx context.Context, path string, snapshotID string, params map[string]any) (string, *Response, error)

	// ExplainQuery returns the optimized plan Forward's Query Debug Panel shows for
	// a SAVED library query: a text rendering of the optimized query (it may not
	// itself be valid NQE) with the parallelism, totality and cardinality marks the
	// optimizer chose. POST /api/internal/nqe/queries/{queryId}/explain
	// (NqeController.explainCommittedQuery, Preview and internal: unpublished;
	// org VIEW_NQE_LIBRARY). commitID empty explains HEAD. snapshotID empty
	// validates without a snapshot, so the schema is not extended with that
	// snapshot's data. params binds a parameterized query's parameters (nil for
	// none; a nil value inside it is a 400). A query with diagnostics (it does
	// not compile) is a 400 with the diagnostics, never a plan. Only a query
	// saved in the library can be explained by id: Forward has no optimized-plan
	// explain for raw query text (explainSchemaAccess takes raw text but returns
	// the schema paths a query reads, not a plan).
	ExplainQuery(ctx context.Context, queryID string, commitID string, snapshotID string, params map[string]any) (string, *Response, error)

	// ListQueries lists committed NQE library queries, optionally under dir.
	ListQueries(ctx context.Context, dir string) ([]NQEQuery, *Response, error)

	// Result returns one JSON result page for a completed asynchronous execution.
	Result(ctx context.Context, networkID string, executionKey string, options NQEResultOptions) (*NQEResult, *Response, error)

	// ResultJSONLines streams a completed asynchronous result as newline-delimited
	// JSON without loading the full result into memory.
	ResultJSONLines(ctx context.Context, networkID string, executionKey string, options NQEResultOptions, dst io.Writer) (*Response, error)

	// Run executes an NQE query synchronously against a network or snapshot.
	//
	// With no snapshotID, Forward picks the newest processed snapshot that is not
	// a draft or a Predict fork. An explicit snapshotID is used as given, and on a
	// predicted snapshot queries over live state (counters, performance) come
	// back empty whatever the network looks like. The result carries SnapshotID;
	// a caller that needs to tell the two apart checks Snapshot.Predicted() on
	// that snapshot. A snapshot that is still processing fails with
	// ErrSnapshotNotProcessed, one whose processing failed with
	// ErrSnapshotProcessingFailed.
	Run(ctx context.Context, networkID string, snapshotID string, query NQEQueryRequest) (*NQEResult, *Response, error)

	// RunMetrics lists the org's slow NQE runs, for finding which queries cost the
	// most. GET /api/nqe-run-metrics (NqeController.getNqeQueryRunMetrics, Preview;
	// org DEBUG_NQE, which most users lack: that is a typed 403,
	// errors.Is(err, ErrPermissionDenied) and MissingPermission).
	RunMetrics(ctx context.Context, options NQERunMetricsOptions) ([]NQERunMetric, *Response, error)

	// Schema returns the NQE data model as this org sees it, as raw InlinedType
	// JSON. It is filtered by the org's feature settings and license tier
	// (NqeAppService.getSchemaTree), and includes network.extensions.<nqeName>
	// for each of the org's data files. GET /api/nqe/schema (NqeController;
	// VIEW_NQE_LIBRARY). Preview: not in the published spec.
	Schema(ctx context.Context) (json.RawMessage, *Response, error)

	// SchemaAt is Schema extended with one snapshot's context: the structure
	// Forward inferred from that snapshot's endpoint and data connector responses
	// (ExternalSourceSchemaLoader). GET /api/nqe/schema?snapshotId= (needs the
	// snapshot created; ErrSnapshotNotProcessed and friends apply). Pass
	// Snapshots.LatestProcessed's ID for a network's current view.
	SchemaAt(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error)

	// Start submits an asynchronous NQE execution.
	Start(ctx context.Context, networkID string, snapshotID string, query NQEExecutionRequest) (*NQEExecution, *Response, error)

	// Status returns the current state of an asynchronous NQE execution.
	Status(ctx context.Context, networkID string, executionKey string) (*NQEExecution, *Response, error)
}

// Predict is the Predict API family: what Client.Predict sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Predict interface {
	// AddChangeSetCheck attaches a check to a change set (POST .../change-sets/{id}/checks) and returns it with its result on the change
	// set's BASE snapshot: Forward runs a check that has not executed before it answers, so this call can take as long as the check, and
	// a check that fails to run comes back as a 2xx with Status ERROR or TIMEOUT, not as an error. Newer Forward also sends Status SKIPPED (a config-only change set skips checks that need behavioral modelling) and can send UNLICENSED, PROCESSING and REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING; Status is a string, so compare, do not assume a closed set. Forward validates the definition (400
	// with the reason: an intent check against the snapshot, an NQE check that its query exists in the NQE Library) and refuses a check
	// type the licence does not include (403). Forward does not deduplicate: if this call times out and is retried, there may be two
	// checks, so list them before adding again. MANAGE_CHANGE_SETS; the organization property PREDICT_MODELING must be on. Results on the snapshots predicted from
	// the change set are read with ListChangeSetChecks. An NQE check is removed by Forward when its query is deleted.
	//
	// Preview: ChangeSetCheckController, not in the published spec.
	AddChangeSetCheck(ctx context.Context, networkID string, changeSetID string, check NewChangeSetCheck) (*ChangeSetCheckResult, *Response, error)

	// AddChangeSetDirectory creates the directory name under parentPath (POST .../change-set-directories/{parentPath}?action=addDir&name=).
	AddChangeSetDirectory(ctx context.Context, networkID string, parentPath string, name string) (*Response, error)

	// AddSecurityRule stages a new rule in the device's rulebase.
	AddSecurityRule(ctx context.Context, networkID string, changeSetID string, deviceName string, scopeID string, rulebaseID string, rule NewSecurityRule) (*Response, error)

	// AffectedDevices lists the devices a change set affected in a PREDICTED snapshot, from its protocol modeling (GET .../protocol-modeling?
	// view=affected-devices; VIEW_NETWORK_AND_SNAPSHOTS and PREDICT_MODELING; protocol modeling must have run). A base snapshot gives
	// ErrNoAffectedDevices. Forward compresses the list; it arrives already decompressed. On a PAN-OS prediction it answered EVERY device of
	// the network (49 of 49 seen live), not the ones the change set changed: the list is the devices that got a protocol-modeling output saved,
	// and Forward's config generator emits interface and static routes for every device, so any device with an L3 address appears (not
	// specific to PAN-OS). Preview: an internal route.
	AffectedDevices(ctx context.Context, snapshotID string) ([]string, *Response, error)

	// AllowedCommands returns, as text, the commands Predict accepts for one protocol on one vendor OS (GET /api/predict-oses/{vendorOs}/
	// allowed-commands?protocol=): Forward's rendering of its mode trie. An OS Predict does not model answers an empty string, not an
	// error. vendorOS is a name from PredictOSes, lowercase as Forward sends it; a name Forward does not know is a 400.
	//
	// The empty answer is normal for PAN-OS and Junos: neither has a command tree (PAN-OS changes are security rules; Junos is checked in
	// code, not by a grammar).
	//
	// Forward's own code fails (500) for every protocol on the Cisco-like OSes (cisco_ios, cisco_ios_xe, cisco_ios_xr, cisco_nxos, cisco_asa
	// and arista_eos): only juniper_srx answers. Reproduced against Forward's source at fe0daf63c899: ModeTriePrettyPrinter.render walks each
	// OS's command tree without a cycle guard, and those trees loop where a command takes an unbounded list (EOS "ip community-list standard
	// <name> deny <asn>:<value> ...", IOS "access-list <n> <n> deny ... any any ...", NX-OS "ip as-path access-list <name> deny (<string>)*
	// ..."), so the walk never ends and the request dies with a StackOverflowError. Nothing the caller sends changes it, whatever the protocol.
	// Bounds: reproduced by running that code on the real trees (EOS, IOS, IOS-XE, IOS-XR, NX-OS, ASA, SRX; BGP, OSPF and CONFIG on each,
	// every protocol on EOS). ModeTriePrettyPrinter and all the tree files are byte-identical at primary 5f7e4ec1841, stable b1feb6fe1c34
	// and fe0daf63c899, so every build this SDK targets has it. Not run: the Forward service itself, and the other protocols on the other
	// OSes. Only Forward can fix it (a visited set or depth cap in render).
	// Any 500 is returned as ErrAllowedCommandsUnavailable, which also wraps the underlying error. juniper_srx's BGP answer is about 19 MB.
	AllowedCommands(ctx context.Context, vendorOS string, protocol PredictProtocol) (string, *Response, error)

	// BGPAdvertisementDiffs returns what the draft's staged BGP advertisements change on a device, compared with the change set's base
	// snapshot (GET .../change-sets/{id}/draft/devices/{device}/bgp-advertisements?view=diffs; MANAGE_CHANGE_SETS). Forward computes it in
	// Predict and sends the JSON array already decompressed, so it decodes as an ordinary body. A device with no BGP changes is an empty list.
	BGPAdvertisementDiffs(ctx context.Context, networkID string, changeSetID string, deviceName string) ([]BGPAdvertisementDiff, *Response, error)

	// BGPCollectionRequirements returns the groups of devices whose collection setup must include BGP advertisements (GET
	// /api/networks/{networkId}/bgp-collection-requirements, SyntheticDeviceController.getBgpCollectionRequirements,
	// VIEW_NETWORK_AND_SNAPSHOTS; the body is {"requirements": [...]}). Preview: not in the published OpenAPI; source-only.
	BGPCollectionRequirements(ctx context.Context, networkID string) ([]BGPCollectionRequirement, *Response, error)

	// BGPNode returns one modeled BGP node, a device's VRF, with its neighbors (GET .../devices/{device}/vrfs/{vrf}/bgp-node;
	// BenchmarkController.getBgpNode). Forward answers {"bgpNode": null} when the VRF runs no BGP and 404 for an unknown device or VRF.
	BGPNode(ctx context.Context, snapshotID string, device string, vrf string) (json.RawMessage, *Response, error)

	// BGPNodeDevices lists the devices that host at least one modeled BGP node, as an object of device id to device name (GET
	// .../bgp-nodes?devices; BenchmarkController.getBgpNodeDevices). Raw: the ids are Forward's internal device ids.
	BGPNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error)

	// BGPRules returns the import and export policy rules of a BGP node's neighbors, per neighbor address family (GET
	// .../bgp-rules?deviceName=&vrfName=&neighborAddr=; BenchmarkController.getBgpNodeRulesByName). With neighborAddr empty every neighbor of
	// the node is included. A neighborAddr that is not an IP address is refused here, because Forward does not catch the parse failure and
	// answers 500 (and would repeat it under a retry policy); a well-formed address that is not a neighbor of the node is a 400, and a VRF
	// without a BGP node gives an empty rule list whatever the address.
	BGPRules(ctx context.Context, snapshotID string, device string, vrf string, neighborAddr string) (json.RawMessage, *Response, error)

	// ChangeSetNQEChecks lists the NQE checks added to a change set (GET /api/nqe/verificationQueries?networkId=&changeSetId=; USE_NQE
	// with MANAGE_CHANGE_SETS, and PREDICT_MODELING). A change set without NQE checks gives an empty list. Preview.
	ChangeSetNQEChecks(ctx context.Context, networkID string, changeSetID string) ([]NQECheckInfo, *Response, error)

	// ChangeSetSubset returns the names, sorted, of the devices in the route-protocol blast radius of changedDevices: at most NumIterations
	// expansion rounds, stopping once the subset reaches Threshold devices, plus the L2 switches OSPF adjacencies between subset devices cross
	// (GET .../rp-nodes?view=changeSetSubset&changedDevices=; BenchmarkController.getChangeSetSubset). The answer is {"names": [...]}. A device
	// that is not in the snapshot, a negative NumIterations or a Threshold below 1 is a 400, and the SDK refuses the numbers before sending.
	// With OSPF nodes present but no stored L2 domain for the snapshot Forward answers 404.
	ChangeSetSubset(ctx context.Context, snapshotID string, changedDevices []string, options ChangeSetSubsetOptions) ([]string, *Response, error)

	// Commit records the staged draft as a commit on the change set, which is what Run then predicts from. With a draft the draft is
	// committed; with no draft but an earlier commit nothing new is created (the head commit stands); with neither, the unchanged base
	// change set is committed. A blank note is sent as DefaultCommitNote (Forward refuses a blank one). CommitWithOptions also returns the
	// commit's metadata.
	Commit(ctx context.Context, networkID string, changeSetID string, note string) (*Response, error)

	// CommitWithOptions is Commit with an optional analysis mode, returning the commit's metadata (id, note, who and when).
	CommitWithOptions(ctx context.Context, networkID string, changeSetID string, options CommitOptions) (*ChangeSetCommitMetadata, *Response, error)

	// CommittedBGPAdvertisementDiffs is BGPAdvertisementDiffs for a commit (GET .../change-sets/{id}/commits/{commitId}/devices/{device}/
	// bgp-advertisements?view=diffs).
	CommittedBGPAdvertisementDiffs(ctx context.Context, networkID string, changeSetID string, commitID string, deviceName string) ([]BGPAdvertisementDiff, *Response, error)

	// ConfigNodeDevices lists the devices that host at least one modeled config node (the routes derived from configuration, static or direct),
	// as an object of device id to device name (GET .../config-nodes?devices; BenchmarkController.getConfigNodeDevices, Forward master after
	// 26.10; not mapped at the 26.10 builds, so those answer 404 "No endpoint" and ErrEndpointNotServed).
	ConfigNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error)

	// ConfigNodes returns the modeled config nodes on a device, one per VRF with direct, local or static routes, as {"configNodes": [...]} (GET
	// .../devices/{device}/config-nodes; BenchmarkController.getConfigNodes). A non-empty vrf limits it to that VRF's node. A device without any
	// gives an empty list; an unknown device or VRF is a 404.
	ConfigNodes(ctx context.Context, snapshotID string, device string, vrf string) (json.RawMessage, *Response, error)

	// CreateChangeSet creates a Predict change set.
	CreateChangeSet(ctx context.Context, networkID string, request ChangeSetCreateRequest) (*ChangeSet, *Response, error)

	// DeleteChangeSet deletes a Predict change set (DELETE .../change-sets/{id}). It cannot be undone, and it also deletes the change set's
	// predicted snapshots (ChangeSetDeletionListener; they answer 404 afterwards), so read what you need from them first.
	DeleteChangeSet(ctx context.Context, networkID string, changeSetID string) (*Response, error)

	// DeleteChangeSetCheck removes a check and its results from a change set (DELETE .../checks/{checkId}; 204). It cannot be undone.
	//
	// Preview: ChangeSetCheckController, not in the published spec.
	DeleteChangeSetCheck(ctx context.Context, networkID string, changeSetID string, checkID string) (*Response, error)

	// DeleteChangeSetDirectory deletes the directory at dirPath (DELETE .../change-set-directories/{dirPath}). By default the change sets
	// in it are kept; Forward moves them up (the shallow delete). With alsoDeleteChangeSets it deletes them too, in the directory and in
	// every directory below it (?retainChangeSets=false), which cannot be undone and also deletes those change sets' predicted snapshots.
	DeleteChangeSetDirectory(ctx context.Context, networkID string, dirPath string, alsoDeleteChangeSets bool) (*Response, error)

	// DeleteChangeSets deletes several change sets of the network (POST .../change-sets?action=delete, body {changeSetIds}). Use
	// DeleteChangeSet for one. It cannot be undone, and like DeleteChangeSet it also deletes their predicted snapshots.
	DeleteChangeSets(ctx context.Context, networkID string, changeSetIDs []string) (*Response, error)

	// DeleteCommit removes one version from the change set's history (DELETE .../commits/{commitId}; 204) AND deletes the predicted
	// snapshots made from it (ChangeSetService.deleteCommit runs deletePredictedSnapshots in the same transaction). It cannot be undone.
	DeleteCommit(ctx context.Context, networkID string, changeSetID string, commitID string) (*Response, error)

	// DiscardAllDeviceChanges clears EVERY change to one device, committed or not, so the next commit has the device as the base snapshot
	// has it (POST .../change-sets/{id}/devices/{device}?action=discardChanges; 204). It works from the draft, or from the head commit when
	// there is no draft, and answers 404 when that state has no such device. Unlike DiscardDraftDevice, which only drops the caller's
	// uncommitted edits back to the last commit, this one reaches the committed changes too.
	DiscardAllDeviceChanges(ctx context.Context, networkID string, changeSetID string, deviceName string) (*Response, error)

	// DiscardDraftDevice throws away the caller's uncommitted edits to one device (DELETE .../draft/devices/{device}).
	DiscardDraftDevice(ctx context.Context, networkID string, changeSetID string, deviceName string) (*Response, error)

	// DiscardSecurityRuleChange undoes one rule's staged add, edit, move or removal (POST .../security-rules/{ruleId}?action=discardChanges; 204).
	DiscardSecurityRuleChange(ctx context.Context, networkID string, changeSetID string, deviceName string, scopeID string, rulebaseID string, ruleID string) (*Response, error)

	// DownloadProtocolModeling writes the snapshot's protocol-modeling blob, a ZIP holding one entry named protocol-modeling.blob (GET
	// .../protocol-modeling with Accept: application/zip; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). The blob is Forward's internal
	// serialization for its own debugging; the SDK does not decode it. Forward builds the whole answer before sending, so the call waits up to
	// ProtocolModelingReadTimeout; if it fails part way, dst may hold a partial ZIP.
	DownloadProtocolModeling(ctx context.Context, snapshotID string, dst io.Writer) (*Response, error)

	// DuplicateChangeSet copies a change set within its network (POST ...change-sets/{id}?action=duplicate) and returns the copy. The copy
	// starts with no analysis mode, whatever the source had; its first commit fixes one again.
	DuplicateChangeSet(ctx context.Context, networkID string, changeSetID string, options DuplicateChangeSetOptions) (*ChangeSet, *Response, error)

	// DuplicateChangeSetToNetwork copies a change set into another network (?action=duplicate&targetNetworkId=) and returns the copy.
	// Forward needs the right to manage change sets in both networks. TargetBaseSnapshotID is required here: it must be a snapshot of
	// the target network (without it Forward reuses the source's base snapshot, which belongs to the source network, and answers 400).
	DuplicateChangeSetToNetwork(ctx context.Context, networkID string, changeSetID string, targetNetworkID string, options DuplicateChangeSetOptions) (*ChangeSet, *Response, error)

	// EditCommitNote replaces a version's note (PATCH .../commits/{commitId}?note=); the note must not be blank.
	EditCommitNote(ctx context.Context, networkID string, changeSetID string, commitID string, note string) (*ChangeSetCommitMetadata, *Response, error)

	// EditSecurityRule replaces a staged or existing rule's definition (PATCH .../security-rules/{ruleId} {"definition": ...}).
	// Only the local rulebase is editable (FirewallScopeLocal, FirewallRulebasePrimary). Needs EDIT_CHANGE_SET_SECURITY_DATA and the
	// FIREWALL_PREDICT organization property. The definition replaces the rule's whole definition, and Enabled is always sent: leave
	// it false and the rule is staged disabled.
	EditSecurityRule(ctx context.Context, networkID string, changeSetID string, deviceName string, scopeID string, rulebaseID string, ruleID string, definition SecurityRuleDefinition) (*Response, error)

	// ExportChangeSetZip writes the change set's head commit as a zip to w (GET .../change-sets/{id}?format=zip). The pinned builds also
	// need the organization property PREDICT_DIFF_INSIGHT_ENHANCEMENTS (Forward master retired it; there PREDICT_MODELING and
	// MANAGE_CHANGE_SETS suffice). Forward audits the export and answers 404 when the change set has no commit.
	// Forward sends the 200 headers before it writes the zip, so a failure mid-stream leaves partial bytes in w along with the error; the
	// client's timeout covers the whole export.
	ExportChangeSetZip(ctx context.Context, networkID string, changeSetID string, w io.Writer) (*Response, error)

	// FirewallObjects returns one object collection of a firewall as the change set's BASELINE snapshot has it (GET .../change-sets/{id}/
	// devices/{device}/{kind}; VIEW_CHANGE_SET_SECURITY_DATA and the organization property FIREWALL_PREDICT, plus device access). The body
	// is Forward's collection type for that kind (AddressObjectsCollection, SecurityZoneCollection, ...), returned as raw JSON: their
	// shapes differ per kind and per vendor, and this SDK does not model them.
	FirewallObjects(ctx context.Context, networkID string, changeSetID string, deviceName string, kind FirewallObjectKind) (json.RawMessage, *Response, error)

	// ListAddressGroups returns the device's address groups (key "addressGroups").
	ListAddressGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallAddressGroup, *Response, error)

	// ListAddressObjects returns the device's address objects (key "addressObjects").
	ListAddressObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallAddressObject, *Response, error)

	// ListApplicationGroups returns the device's application groups (key "applicationGroups").
	ListApplicationGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallApplicationGroup, *Response, error)

	// ListApplicationObjects returns the device's application objects (key "applicationObjects").
	ListApplicationObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallApplicationObject, *Response, error)

	// ListFirewallZones returns the device's security zones (key "zones"); see also Networks.SecurityZones for a network's zones.
	ListFirewallZones(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallZone, *Response, error)

	// ListServiceGroups returns the device's service groups (key "serviceGroups").
	ListServiceGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallServiceGroup, *Response, error)

	// ListServiceObjects returns the device's service objects across all scopes (key "serviceObjects").
	ListServiceObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallServiceObject, *Response, error)

	// ListURLObjects returns the device's URL objects (key "urlObjects").
	ListURLObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallURLObject, *Response, error)

	// ListUserObjects returns the device's user objects (key "userObjects").
	ListUserObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallUserObject, *Response, error)

	// GetChangeSetSummary reads one change set's base information (GET .../change-sets/{id}?view=summary): the cheap way to learn which
	// snapshot a change set is based on and which ServiceNow requests it is linked to, since Forward has no plain per-change-set read.
	// Needs MANAGE_CHANGE_SETS and the PREDICT_MODELING organization property; an unknown change set is a 404. The route is not in Forward's
	// published OpenAPI (preview), and Forward plans to add change-set types to this response (FWD-44083).
	GetChangeSetSummary(ctx context.Context, networkID string, changeSetID string) (*ChangeSetSummary, *Response, error)

	// GetCommit returns one version of the change set (GET .../commits/{commitId}).
	GetCommit(ctx context.Context, networkID string, changeSetID string, commitID string) (*ChangeSetCommit, *Response, error)

	// GetCommittedConfig returns a device's configuration as one committed version has it (GET
	// .../commits/{commitId}/devices/{device}/config). Compare it with GetDraftConfig or another version for a per-device config diff.
	GetCommittedConfig(ctx context.Context, networkID string, changeSetID string, commitID string, deviceName string) (string, *Response, error)

	// GetDraft returns the caller's uncommitted edits to the change set (GET .../change-sets/{id}/draft); Draft is nil when there are none.
	GetDraft(ctx context.Context, networkID string, changeSetID string) (*ChangeSetDraft, *Response, error)

	// GetDraftConfig returns a device's configuration as the draft would leave it (GET .../draft/devices/{device}/config). Forward
	// answers 404 when there is no draft, or when the device has no commands, config or security changes in it.
	GetDraftConfig(ctx context.Context, networkID string, changeSetID string, deviceName string) (string, *Response, error)

	// GetHeadCommit returns the latest version of the change set (GET .../commits/head).
	GetHeadCommit(ctx context.Context, networkID string, changeSetID string) (*ChangeSetCommit, *Response, error)

	// ListChangeSetChecks returns the change set's attached checks with their
	// results on snapshotID (GET .../change-sets/{cs}/checks?snapshotId=).
	// snapshotID must be the change set's base or one of its predicted snapshots;
	// Forward refuses anything else. An empty snapshotID omits the parameter.
	ListChangeSetChecks(ctx context.Context, networkID string, changeSetID string, snapshotID string) ([]ChangeSetCheckResult, *Response, error)

	// ListChangeSetDirectories returns the directory tree as path to change-set ids (GET .../change-set-directories). A network with
	// no directories answers an empty map.
	ListChangeSetDirectories(ctx context.Context, networkID string) (ChangeSetDirectories, *Response, error)

	// ListChangeSets lists Predict change sets for a network.
	ListChangeSets(ctx context.Context, networkID string) ([]ChangeSet, *Response, error)

	// ListCommits returns the change set's version history, newest first as Forward orders it (GET .../change-sets/{id}/commits).
	ListCommits(ctx context.Context, networkID string, changeSetID string) ([]ChangeSetCommit, *Response, error)

	// ListPredictedSnapshots returns the prediction history for a change set.
	ListPredictedSnapshots(ctx context.Context, networkID string, changeSetID string) ([]PredictedSnapshot, *Response, error)

	// MoveChangeSetDirectory moves the directory at dirPath, with what is in it, under destPath (?action=move&to=).
	MoveChangeSetDirectory(ctx context.Context, networkID string, dirPath string, destPath string) (*Response, error)

	// MoveChangeSetToDirectory puts one change set in the directory at dirPath (?action=moveChangeSetTo&changeSetId=).
	MoveChangeSetToDirectory(ctx context.Context, networkID string, dirPath string, changeSetID string) (*Response, error)

	// MoveChangeSetsToDirectory puts several change sets in the directory at dirPath (?action=moveChangeSetsTo, body {changeSetIds}).
	MoveChangeSetsToDirectory(ctx context.Context, networkID string, dirPath string, changeSetIDs []string) (*Response, error)

	// MoveSecurityRule moves a rule to sit right after the rule afterRuleID in its rulebase; an empty afterRuleID moves it to the top
	// (PATCH .../security-rules/{ruleId} {"predecessor": ...}).
	MoveSecurityRule(ctx context.Context, networkID string, changeSetID string, deviceName string, scopeID string, rulebaseID string, ruleID string, afterRuleID string) (*Response, error)

	// OSPFNodeDevices is BGPNodeDevices for OSPF (GET .../ospf-nodes?devices; BenchmarkController.getOspfNodeDevices).
	OSPFNodeDevices(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error)

	// OSPFNodes returns the modeled OSPF nodes on a device, one per process and area, as {"ospfNodes": [...]} (GET
	// .../devices/{device}/ospf-nodes; BenchmarkController.getOspfNodes). A non-empty vrf limits it to that VRF's nodes. A device that runs no
	// OSPF gives an empty list, not an error; an unknown device or VRF is a 404.
	OSPFNodes(ctx context.Context, snapshotID string, device string, vrf string) (json.RawMessage, *Response, error)

	// PatchChangeSetsTags adds or removes tags on several change sets at once (PATCH .../change-sets). ChangeSetTagsRemoveAll takes no
	// names; the other two need at least one.
	PatchChangeSetsTags(ctx context.Context, networkID string, changeSetIDs []string, action ChangeSetTagAction, names []string) (*Response, error)

	// PredictOSes lists the vendor operating systems Predict has a command grammar or editor for (GET /api/predict-oses): the eight in
	// PredictCommandOSes plus "pan_os" (edited as security rules) on current Forward, which is why it has more names than that list. It needs only the organization property
	// PREDICT_MODELING, no network permission, and it is not scoped to a network.
	PredictOSes(ctx context.Context) ([]string, *Response, error)

	// PrettifyCommands returns the commands reformatted the way Forward would store them for the device (POST .../change-sets/{id}/devices/
	// {device}/commands?action=prettify). It changes nothing in the change set. The body is sent as plain text.
	PrettifyCommands(ctx context.Context, networkID string, changeSetID string, deviceName string, commands string) (string, *Response, error)

	// PreviewCommandsConfig returns the configuration a device would have if commands were applied to its BASE snapshot's config
	// (the draft is ignored), without staging them (POST .../devices/{device}/config?action=preview, text in and text out): the
	// "preview config changes" view. Commands are cleaned the way StageCommands cleans them; Forward answers 404 for blank commands,
	// and this SDK refuses commands that are blank after cleaning.
	PreviewCommandsConfig(ctx context.Context, networkID string, changeSetID string, deviceName string, commands string) (string, *Response, error)

	// ProtocolModelingDone reports whether protocol modeling has finished for a snapshot (GET /api/internal/snapshots/{snapshotId}/
	// protocol-modeling?view=status; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING, so a read-only user is refused). Forward sends
	// {"done": bool}: true once a successful result is stored. A modeling job that failed also reads false, so bound the wait on the
	// caller's side. Preview: an internal route, not in the published OpenAPI.
	ProtocolModelingDone(ctx context.Context, snapshotID string) (bool, *Response, error)

	// ProtocolModelingPartitions returns how protocol modeling partitioned the snapshot (GET .../protocol-modeling?view=partitions;
	// MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). Protocol modeling must have run. The answer is Forward's debug output, returned raw;
	// Forward compresses it and the server decompresses it before sending, as for ProtocolSeedRoutes. When partitioning was not used for the
	// snapshot Forward answers 200 with {"error": "..."} instead of an error status, so check the raw answer for an "error" key. The call waits up
	// to ProtocolModelingReadTimeout.
	ProtocolModelingPartitions(ctx context.Context, snapshotID string) (json.RawMessage, *Response, error)

	// ProtocolPathSelection returns the path-selection candidates for a prefix per protocol (GET .../protocol-modeling?view=path-selection&prefix=),
	// including the candidates the search and trace routes prune (the ABSENT ones), for debugging a missing predicted entry; the BGP part also
	// has the predecessor links between candidate route sets. Same gates and raw shape as ProtocolSeedRoutes.
	ProtocolPathSelection(ctx context.Context, snapshotID string, q ProtocolModelingQuery) (json.RawMessage, *Response, error)

	// ProtocolSeedRoutes returns the seed route sets containing the prefix, as they were created before propagation, across every protocol
	// modeled together (GET .../protocol-modeling?view=seed-routes&prefix=; VIEW_NETWORK_AND_SNAPSHOTS and PREDICT_MODELING). Protocol modeling
	// must have run (see ProtocolModelingDone). Forward's source says BGP and OSPF; the answer's shape is Forward's debug output and is
	// returned raw. Forward compresses it and the server decompresses it before sending.
	ProtocolSeedRoutes(ctx context.Context, snapshotID string, q ProtocolModelingQuery) (json.RawMessage, *Response, error)

	// RemoveSecurityRule stages the removal of the rule with the given id in the change set's draft (DELETE .../security-rules/{ruleId}). The
	// baseline snapshot is untouched. For a rule from the baseline, DiscardSecurityRuleChange brings it back (as the baseline had it, so an
	// earlier edit is lost). For a rule added in this change set, removing it just drops the addition and there is nothing to restore.
	// Removing the same rule twice does nothing the second time.
	RemoveSecurityRule(ctx context.Context, networkID string, changeSetID string, deviceName string, scopeID string, rulebaseID string, ruleID string) (*Response, error)

	// RenameChangeSetDirectory renames the directory at dirPath (?action=rename&name=).
	RenameChangeSetDirectory(ctx context.Context, networkID string, dirPath string, newName string) (*Response, error)

	// RestoreBGPAdvertisement drops whatever is staged for the key on the device, an injected or modified advertisement or a
	// withdrawal, so the received advertisement stands as collected (?action=restore, body the key; 204;
	// ChangeSetController.restoreAdvertisement). A key with nothing staged is ignored. It works on the draft or, when there is none,
	// on the latest commit (it then creates a draft that undoes the committed edit for that key).
	RestoreBGPAdvertisement(ctx context.Context, networkID string, changeSetID string, deviceName string, key BGPAdvertisementKey) (*Response, error)

	// RestoreBGPAdvertisements is RestoreBGPAdvertisement for several keys on one device (?action=bulkRestore; ChangeSetController.restoreAdvertisements). A repeated key is refused.
	RestoreBGPAdvertisements(ctx context.Context, networkID string, changeSetID string, deviceName string, keys []BGPAdvertisementKey) (*Response, error)

	// RestoreCommit makes an earlier version the change set's current content (POST .../commits/{commitId}?action=restore) and
	// returns the commit it created. The earlier versions stay in the history. If the caller has a pending draft, Forward commits it
	// first with the note "Draft changes auto-saved", so one restore can create two commits.
	RestoreCommit(ctx context.Context, networkID string, changeSetID string, commitID string) (*ChangeSetCommitMetadata, *Response, error)

	// RestoreCommitAsNew copies an earlier version into a new change set (?action=restoreAsNew) and returns it. targetName and
	// targetBaseSnapshotID are optional ("" lets Forward choose); a given snapshot must have finished processing up to its creation
	// stage, or Forward answers with a snapshot-unavailable or processing error.
	RestoreCommitAsNew(ctx context.Context, networkID string, changeSetID string, commitID string, targetName string, targetBaseSnapshotID string) (*ChangeSetCommit, *Response, error)

	// RetriggerProtocolModeling is TriggerProtocolModeling for a snapshot that already has a modeling result (?action=retrigger; 202).
	RetriggerProtocolModeling(ctx context.Context, snapshotID string) (*Response, error)

	// Run starts predictive analysis and returns the predicted snapshot's metadata (not its state: use ListPredictedSnapshots). Processing
	// continues asynchronously. Forward commits an uncommitted draft first with the note "Draft changes auto-saved", answers 404 when the
	// change set has neither a draft nor a commit, and returns the EXISTING predicted snapshot when one already matches the commit and
	// analysis mode instead of starting another. A config-only change set whose changes are only BGP advertisements is a 400. A prediction
	// over a STALE base snapshot (one processed before a Forward upgrade's reprocess) can fail with no reason through any route (seen live as a
	// 400 or 500 from run or commit, and a predicted snapshot that never processes): reprocess the base first.
	Run(ctx context.Context, networkID string, changeSetID string, note string) (*PredictedSnapshot, *Response, error)

	// RunIEntryChecks runs unit packet tests against a snapshot's model (POST /api/snapshots/{snapshotId}/ientrychecks; VIEW_NETWORK_AND_SNAPSHOTS
	// and the organization property UNIT_PACKET_TEST; the snapshot must be past creation). Needs at least one check. The results are NOT in
	// the order of the checks (Forward groups them by device and table); match each to its check by the check fields they carry. A check
	// whose table the device lacks comes back with Status ERROR.
	RunIEntryChecks(ctx context.Context, snapshotID string, checks []IEntryCheck) ([]IEntryCheckResult, *Response, error)

	// SaveDeviceConfig is the one route that takes ANY vendor, PAN-OS included, because Forward stores the text unchecked: it is not validated
	// input, and nothing says Predict can model what it holds. Replaces a device's whole running configuration in the change set's draft (PUT
	// .../draft/devices/{device}/config, text/plain): the "config file" option. Forward stores the text as given, checks nothing about
	// it, and refuses more than 2 MB (this SDK refuses it first); it replaces any staged commands for that device (a device has either
	// commands or a config file). Send the complete configuration. Commit the draft, then Run. Whether Predict can model the result
	// depends on the device type, which Forward finds out when it runs.
	SaveDeviceConfig(ctx context.Context, networkID string, changeSetID string, deviceName string, config string) (*Response, error)

	// SecurityRulesDiff reads the device's rulebases with every rule marked by how the change set alters it.
	SecurityRulesDiff(ctx context.Context, networkID string, changeSetID string, deviceName string) (*SecurityRuleTableDiff, *Response, error)

	// StageBGPAdvertisement injects an external BGP advertisement into a device draft, or modifies a received one that has the same
	// key and peer type (see BGPAdvertisement).
	StageBGPAdvertisement(ctx context.Context, networkID string, changeSetID string, advertisement BGPAdvertisement) (*Response, error)

	// StageBGPAdvertisements stages every advertisement one device receives in a
	// single draft update (?action=bulkAdd, a JSON array of the records
	// StageBGPAdvertisement sends one at a time). Forward appends them to the
	// device's ordered list in slice order, so the order of ads is the order the
	// draft records. An empty slice is a no-op that sends nothing.
	//
	// A build without bulkAdd answers 400 "Parameter conditions ... not met";
	// callers that must support such a build fall back to StageBGPAdvertisement.
	StageBGPAdvertisements(ctx context.Context, networkID string, changeSetID string, deviceName string, advertisements []BGPAdvertisement) (*Response, error)

	// StageCommands replaces a device's draft CLI commands. Interactive wrapper
	// lines are removed because Predict accepts configuration-mode content only.
	// A CLI `network <prefix>` is still not a reliable BGP origination; use the
	// structured StageBGPAdvertisement path described above.
	StageCommands(ctx context.Context, networkID string, changeSetID string, deviceName string, commands string) (*Response, error)

	// TriggerProtocolModeling starts protocol modeling for a snapshot and returns at once (POST /api/internal/snapshots/{snapshotId}/
	// protocol-modeling?action=trigger; 202; MANAGE_PROTOCOL_MODELING and PREDICT_MODELING). Poll ProtocolModelingDone. Preview: an internal
	// route. RetriggerProtocolModeling runs it again for a snapshot that already has a result.
	TriggerProtocolModeling(ctx context.Context, snapshotID string) (*Response, error)

	// UpdateChangeSet changes a change set's name, description, tags or base snapshot (PATCH .../change-sets/{id}; MANAGE_CHANGE_SETS)
	// and returns it. Edits to its devices are staged with StageCommands, SaveDeviceConfig and the firewall calls, then Commit and Run
	// produce a new version and predicted snapshot.
	UpdateChangeSet(ctx context.Context, networkID string, changeSetID string, patch ChangeSetPatch) (*ChangeSet, *Response, error)

	// UpdateChangeSetCheck changes a check's name or definition (PATCH .../checks/{checkId}; 204). Changing the definition changes what
	// the check means, so results from before are outdated until it runs again.
	//
	// Preview: ChangeSetCheckController, not in the published spec.
	UpdateChangeSetCheck(ctx context.Context, networkID string, changeSetID string, checkID string, patch ChangeSetCheckPatch) (*Response, error)

	// ValidateCommands is ADVISORY: it reports problems and nothing rejects a bad command at staging (StageCommands accepts text Forward
	// would flag here), so an empty CommandErrors proves nothing, and a vendor without a command grammar has little to check. Only a
	// Predict run shows whether the change was modelled. Every error seen live was ErrorType INVALID (Forward also has INCOMPLETE and UNSUPPORTED), with the line, columns
	// and the expected arguments; PAN-OS answers an empty result for anything, invented lines included; and EOS marks some real commands
	// INVALID (extended ACL lines, "management api"). Treat INVALID as evidence to review, not as proof the command is wrong.
	//
	// ValidateCommands checks CLI text for a device in a change set without
	// staging it (POST .../change-sets/{cs}/devices/{d}/commands?action=validate).
	// Forward rejects empty commands and a cursor outside the text.
	ValidateCommands(ctx context.Context, networkID string, changeSetID string, deviceName string, commands string, options ...CommandValidationOptions) (*CommandValidation, *Response, error)

	// VerifyPredictModeling compares Predict's model of a BASE snapshot with the entries the snapshot collected (POST /api/internal/snapshots/
	// {snapshotId}/verify-predict-modeling?mode=base&returnDiffs=; IEntryCheckController at master fe0daf63c899, read from source, not run live).
	// With returnDiffs false only the scores come back and diffs is an empty object; the server default is true, which returns the
	// missing and extra entries of every device that has a difference and can be very large. Needs MANAGE_PROTOCOL_MODELING (the network ADMIN role; PREDICT licence facet)
	// and a snapshot past creation; the route is not gated on the PREDICT_MODELING org property, but with it off no protocol is supported, so the scores should
	// come back zero (or the call fails if the prerequisite jobs do; not traced). It is computed on every call, with no stored result to read instead: the master first triggers (or reuses the
	// finished result of) the snapshot's protocol-modeling and predictable-routes jobs, which cache derived data for the snapshot, then runs
	// the comparison on a search worker with a 90 minute task timeout. Nothing in the comparison itself writes a result, and the route is
	// not audited. It can be slow on a large snapshot, so the call waits up to VerifyPredictModelingTimeout instead of the client's usual timeout.
	VerifyPredictModeling(ctx context.Context, snapshotID string, returnDiffs bool) (*PredictModelVerification, *Response, error)

	// WithdrawBGPAdvertisement stages the withdrawal of a received BGP advertisement on a device (POST
	// .../draft/devices/{device}/bgp-advertisements?action=remove, body the key; ChangeSetController.withdrawAdvertisement): the
	// "withdraw" of Forward's "modify received BGP advertisements", for failure and de-peering what-ifs. For a peer OUTSIDE the
	// snapshot (an ISP, say), shutting the neighbor with a CLI command removes the session from the predicted snapshot but still
	// seeds the routes learned from it (BgpDeviceSeedRoutesDiscoverer.applyInboundPolicy), so model that peer loss by withdrawing
	// its advertisements. A peer that is itself a collected device needs no withdrawal: its routes are never seeded once the
	// session is gone. Forward does not check at staging time that the key matches a received advertisement; that is decided when
	// the change set runs, and a key matching nothing is ignored there. It cancels a staged advertisement with the same key. The
	// device needs received-route data: collectBgpAdvertisements on, with the BGP table type BOTH or ADJ_RIB_IN covering the peer.
	// The key is trimmed before it is sent; its prefix must have no host bits set, and a bare address is a host route.
	WithdrawBGPAdvertisement(ctx context.Context, networkID string, changeSetID string, deviceName string, key BGPAdvertisementKey) (*Response, error)

	// WithdrawBGPAdvertisements is WithdrawBGPAdvertisement for several keys on one device (?action=bulkRemove; ChangeSetController.withdrawAdvertisements). A repeated key is refused.
	WithdrawBGPAdvertisements(ctx context.Context, networkID string, changeSetID string, deviceName string, keys []BGPAdvertisementKey) (*Response, error)
}

// AI is the AI API family: what Client.AI sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type AI interface {
	// AddMessage appends a prompt to an AI chat. Processing is asynchronous; poll
	// GetChat and then call ListMessages for the completed answer.
	AddMessage(ctx context.Context, networkID string, chatID string, prompt string) (*Response, error)

	// DeleteChat deletes an AI chat.
	DeleteChat(ctx context.Context, chatID string) (*Response, error)

	// GetChat returns AI chat metadata, including its processing status.
	GetChat(ctx context.Context, chatID string) (*AIChat, *Response, error)

	// ListChats lists the authenticated user's AI chats.
	ListChats(ctx context.Context) ([]AIChat, *Response, error)

	// ListMessages returns messages in an AI chat.
	ListMessages(ctx context.Context, chatID string, options AIMessageListOptions) ([]AIMessage, *Response, error)

	// RenameChat updates an AI chat's display name.
	RenameChat(ctx context.Context, chatID string, name string) (*AIChat, *Response, error)

	// StartChat starts an asynchronous AI chat grounded in a processed snapshot.
	StartChat(ctx context.Context, networkID string, snapshotID string, prompt string) (*AIChat, *Response, error)
}

// Diffs is the Diffs API family: what Client.Diffs sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Diffs interface {
	// ACLDevices lists the devices whose ACLs changed, each with its change counts.
	// GET /api/diffs/{a}/{b}/acl (getAclDiffStats, appserver controller, Preview;
	// VIEW_ACL_DIFFS). It carries counts, not rules: DeviceACL lists a device's
	// entries. Count with DiffACL is the device total without this body.
	ACLDevices(ctx context.Context, snapshotA string, snapshotB string) ([]ACLDiffDevice, *Response, error)

	// ARPDevices lists the devices whose ARP tables differ, each with its change counts (GET .../arp, VIEW_ARP_DIFFS).
	//
	// Preview: DiffController, not in the published spec.
	ARPDevices(ctx context.Context, snapshotA string, snapshotB string) ([]DeviceDiffSummary, *Response, error)

	// Checks returns the per-check diff rows of one check type between two
	// snapshots (GET /api/diffs/{a}/{b}/checks?type=). checkType is a Forward
	// CheckType such as PREDEFINED, EXISTS, ISOLATION, REACHABILITY or NQE. Bare
	// arrays and {"checks": [...]} are both accepted.
	Checks(ctx context.Context, snapshotA string, snapshotB string, checkType string) ([]CheckDiffEntry, *Response, error)

	// ChecksCount returns the per-check-type diff counts
	// (GET /api/diffs/{a}/{b}/checks?counts), keyed by Forward's CheckType.
	ChecksCount(ctx context.Context, snapshotA string, snapshotB string) (map[string]DiffCount, *Response, error)

	// ConnectivityDiffLocationPair returns the changed subnet pairs from location
	// sourceLocationID to destinationLocationID (view=locationPair&loc1=&loc2=).
	ConnectivityDiffLocationPair(ctx context.Context, beforeSnapshotID string, afterSnapshotID string, sourceLocationID string, destinationLocationID string) ([]SubnetPairConnDiffStat, *Response, error)

	// ConnectivityDiffLocationPairs returns, for one diff type (ConnectivityDiffAdded, Deleted or Modified), how many connectivity changes each
	// pair of locations has (GET .../subnet-connectivity?view=locationPairs&type=[&loc=]; VIEW_CONNECTIVITY_DIFFS and the org property
	// LOCATION_CONNECTIVITY_DIFFS, which only Forward support can set: an org admin gets the 403 ErrFeatureGated; advanced reachability must
	// be on for the network). loc narrows it to pairs involving one location; empty reads all. While Forward is still computing the
	// comparison the answer is an EMPTY list, which is not "no change" (seen live: the first ask answers {"counts":[]}, a later one the real
	// answer), so use WaitConnectivityDiffLocationPairs unless you already waited with WaitForSubnetConnectivity. Preview: the shape is from
	// Forward's source (LocationConnChangeCounts), the empty-while-computing behaviour was seen live.
	ConnectivityDiffLocationPairs(ctx context.Context, beforeSnapshotID string, afterSnapshotID string, diffType string, locationID string) ([]LocationPairChangeCount, *Response, error)

	// ConnectivityDiffLocations returns per-location incoming and outgoing change
	// counts (view=locations). Empty while the comparison is still partial.
	ConnectivityDiffLocations(ctx context.Context, beforeSnapshotID string, afterSnapshotID string) ([]LocationConnDiffStat, *Response, error)

	// Count returns the diff count for one family. routing-loop's path already
	// ends in /count and takes no query. kind is a path tail, so it must be lowercase letters and digits separated by single dashes
	// or slashes (every family in CountableDiffKinds is); anything else is refused before a request is sent. Count is for the
	// families that answer ?count, plus routing-loop.
	Count(ctx context.Context, snapshotA string, snapshotB string, kind DiffKind) (*DiffCount, *Response, error)

	// DeviceACL lists one device's ACL entries that were added, deleted or
	// modified between two snapshots. GET /api/diffs/{a}/{b}/acl/devices/{device}
	// (getDeviceAclDiff, appserver controller, Preview; VIEW_ACL_DIFFS, and the
	// caller must be allowed to see the device). Entries carry the rule's name,
	// action, match conditions and the config lines it came from (see
	// ACLDiffEntry).
	DeviceACL(ctx context.Context, snapshotA string, snapshotB string, device string) (*DeviceACLDiff, *Response, error)

	// DeviceARP lists one device's ARP entries that were added, deleted or modified (GET .../arp/devices/{device}; the caller must be
	// allowed to see the device).
	//
	// Preview: DiffController, not in the published spec.
	DeviceARP(ctx context.Context, snapshotA string, snapshotB string, device string) (*DeviceARPDiff, *Response, error)

	// DeviceL3 returns one device's L3 routing diff as Forward sent it (GET .../l3/devices/{device}; the caller must be allowed to see
	// the device): the routes added, removed and
	// changed, with their resolved next hops. The entry JSON comes from a custom serializer, so it is left raw.
	//
	// Preview: DiffController, not in the published spec.
	DeviceL3(ctx context.Context, snapshotA string, snapshotB string, device string) (json.RawMessage, *Response, error)

	// DeviceMAC lists one device's MAC-table entries that were added, deleted or modified (GET .../mac/devices/{device}; the caller must
	// be allowed to see the device).
	//
	// Preview: DiffController, not in the published spec.
	DeviceMAC(ctx context.Context, snapshotA string, snapshotB string, device string) (*DeviceMACDiff, *Response, error)

	// DeviceNAT returns one device's NAT diff as Forward sent it (GET .../nat/devices/{device}; the caller must be allowed to see the
	// device). The entry JSON comes from a custom serializer that varies by NAT kind, so it is not modelled; the top level is
	// {"diffs": [{diffType, a, b}], "stats": {...}}.
	//
	// Preview: DiffController, not in the published spec.
	DeviceNAT(ctx context.Context, snapshotA string, snapshotB string, device string) (json.RawMessage, *Response, error)

	// Devices returns the full device inventory diff.
	Devices(ctx context.Context, snapshotA string, snapshotB string) ([]DiffEntry[InventoryDeviceInfo], *Response, error)

	// Files lists the devices whose collected files differ between two snapshots
	// and which files changed. GET /api/diffs/{a}/{b}/files (DiffController.
	// getFilesDiff, on primary 15398425a69 and stable 67e89c87124); A is the older
	// snapshot. fileType narrows it to CONFIG, STATE or CUSTOM; empty lists all.
	//
	// Forward has no route that returns the diff text itself -- its UI fetches
	// both versions and diffs them in the browser. Do the same: download each side
	// with Devices.DownloadFile at snapshot A and B, using DeviceFileName.
	Files(ctx context.Context, snapshotA string, snapshotB string, fileType string) ([]DeviceFilesDiff, *Response, error)

	// FilesCount returns the collected-files diff count with its per-type breakdown.
	FilesCount(ctx context.Context, snapshotA string, snapshotB string) (*FilesDiffCount, *Response, error)

	// L3Devices lists the devices whose L3 routing differs, each with its change counts (GET .../l3?view=devices, VIEW_ROUTE_DIFFS). Forward
	// computes and caches the routing diff on the first call, which can take a while on a large network.
	//
	// Preview: DiffController, not in the published spec.
	L3Devices(ctx context.Context, snapshotA string, snapshotB string) ([]DeviceDiffSummary, *Response, error)

	// L3PrefixDetails returns how one prefix's routing changed, on every device that has it, as Forward sent it (GET
	// .../l3/prefixes/{prefix}; the prefix is escaped into the path as the UI does).
	//
	// Preview: DiffController, not in the published spec.
	L3PrefixDetails(ctx context.Context, snapshotA string, snapshotB string, prefix string) (json.RawMessage, *Response, error)

	// L3Prefixes returns the routing diff by prefix as Forward sent it, one page at a time (GET .../l3?view=prefixes&offset=&limit=&
	// prefixFilter=). Page with Offset and Limit; the body carries the total.
	//
	// Preview: DiffController, not in the published spec.
	L3Prefixes(ctx context.Context, snapshotA string, snapshotB string, options L3PrefixOptions) (json.RawMessage, *Response, error)

	// L3Summary returns the network-wide L3 routing diff summary as Forward sent it (GET .../l3/summary, VIEW_ROUTE_DIFFS). Forward
	// computes and caches the routing diff on the first call.
	//
	// Preview: DiffController, not in the published spec.
	L3Summary(ctx context.Context, snapshotA string, snapshotB string) (json.RawMessage, *Response, error)

	// MACDevices lists the devices whose MAC tables differ, each with its change counts (GET .../mac, VIEW_MAC_DIFFS).
	//
	// Preview: DiffController, not in the published spec.
	MACDevices(ctx context.Context, snapshotA string, snapshotB string) ([]DeviceDiffSummary, *Response, error)

	// NATDevices lists the devices whose NAT entries differ, each with its change counts (GET .../nat, VIEW_NAT_DIFFS).
	//
	// Preview: DiffController, not in the published spec.
	NATDevices(ctx context.Context, snapshotA string, snapshotB string) ([]DeviceDiffSummary, *Response, error)

	// RoutingLoopsBidirectional says whether the second snapshot introduced a routing loop (Warnings) or resolved one (Improvements) (GET
	// .../routing-loop/count?view=bidirectional, VIEW_CHECK_DIFFS; the pinned builds also need PREDICT_DIFF_INSIGHT_ENHANCEMENTS, which master retired). Each Count is 0 or 1, not
	// a number of loops, and Complete=false with 0 means Forward could not tell, not "none yet". Diffs.Count with DiffRoutingLoop is the
	// one-directional count.
	//
	// Preview: DiffController, not in the published spec.
	RoutingLoopsBidirectional(ctx context.Context, snapshotA string, snapshotB string) (*BidirectionalRoutingLoops, *Response, error)

	// SubnetConnectivity returns the connectivity comparison as Forward has it
	// right now, which may be partial. Prefer WaitForSubnetConnectivity.
	SubnetConnectivity(ctx context.Context, beforeSnapshotID string, afterSnapshotID string) (*SubnetConnectivityDiff, *Response, error)

	// VulnerabilitiesBidirectional returns the vulnerability change in both directions (GET .../vulnerabilities/counts?view=bidirectional,
	// VIEW_CVE_DIFFS; the pinned builds also need the organization property PREDICT_DIFF_INSIGHT_ENHANCEMENTS, otherwise Forward refuses it;
	// Forward master retired that property and needs only the permission).
	//
	// Preview: DiffController, not in the published spec.
	VulnerabilitiesBidirectional(ctx context.Context, snapshotA string, snapshotB string) (*BidirectionalVulnerabilities, *Response, error)

	// VulnerabilitiesCounts (VIEW_CVE_DIFFS, held from READ_ONLY up and needing the security licence facet) returns the new CVEs and new internet-exposed vulnerable devices from the first snapshot to the second.
	//
	// Preview: DiffController, not in the published spec.
	VulnerabilitiesCounts(ctx context.Context, snapshotA string, snapshotB string) (*VulnerabilitiesDiffSummary, *Response, error)
}

// AIAssist is the AIAssist API family: what Client.AIAssist sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type AIAssist interface {
	// AskNQEDocs asks a question grounded in Forward's NQE documentation.
	AskNQEDocs(ctx context.Context, request NQEDocAssistRequest) (*NQEDocAssist, *Response, error)

	// GenerateNQEQuery turns a natural-language prompt into NQE source.
	GenerateNQEQuery(ctx context.Context, prompt string) (*NQEQueryAssist, *Response, error)

	// GeneratePredictCLI generates device-specific CLI for a change-set intent.
	GeneratePredictCLI(ctx context.Context, networkID string, changeSetID string, deviceName string, prompt string) (string, *Response, error)

	// GeneratePredictOverview generates a name, description, and tags for a change set.
	GeneratePredictOverview(ctx context.Context, networkID string, changeSetID string) (*PredictOverviewAssist, *Response, error)

	// SuggestNQEDocFollowups returns suggested questions for an existing answer.
	SuggestNQEDocFollowups(ctx context.Context, assistID string, answer string) ([]string, *Response, error)

	// SummarizeChangeImpact generates an AI summary of a snapshot's modeled impact.
	SummarizeChangeImpact(ctx context.Context, beforeSnapshotID string, afterSnapshotID string) (string, *Response, error)

	// SummarizeConfigDiff generates an AI summary of configuration differences.
	SummarizeConfigDiff(ctx context.Context, beforeSnapshotID string, afterSnapshotID string) (string, *Response, error)

	// SummarizeFailures generates an AI summary of what failed between two snapshots: regressions and new warnings the change introduced
	// from the base to the predicted snapshot (GET /api/diffs/{a}/{b}/failure-summary-assist, PredictAssistController; VIEW_CHECK_DIFFS and
	// the organization properties AI_ALLOWED and PREDICT_FAILURE_SUMMARY_ASSIST; preview). Predefined and intent checks and routing loops
	// are always covered; NQE checks need USE_NQE, and new CVEs and newly exposed vulnerable devices need VIEW_CVE_DIFFS and a licence that
	// allows it. A caller without those rights still gets an answer with those parts missing.
	// Checks still processing do not block it: Forward then summarizes what is done, tells the model the counts are partial and does not
	// cache the answer. No field reports that, only the text may say so, so call again later and compare rather than treating one answer as
	// final. A complete summary is cached for an hour per user in each appserver's memory. Every call that is not a cache hit is a model
	// call, and this is a GET, so a client with a retry policy resends it after a 502, a 504 or a transport failure: use WithoutRetry where
	// that cost matters. The text comes from a language model: report it as such.
	SummarizeFailures(ctx context.Context, beforeSnapshotID string, afterSnapshotID string) (string, *Response, error)

	// SummarizeNQEQuery generates a natural-language description of NQE source.
	SummarizeNQEQuery(ctx context.Context, query string) (*NQESummaryAssist, *Response, error)
}

// Properties is the Properties API family: what Client.Properties sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Properties interface {
	// ClearCurrent deletes an override for the authenticated user's organization.
	ClearCurrent(ctx context.Context, property OrgProperty) (*Response, error)

	// ClearGlobal deletes a global default override. Forward-admin permission is required.
	ClearGlobal(ctx context.Context, property OrgProperty) (*Response, error)

	// ClearNetwork removes one network's override of a network property, so the network takes the org's value again (DELETE
	// /api/networks/{networkId}/config/{property}, 204; ConfigController.deleteNetworkConfigOverride). Preview, like SetNetwork.
	ClearNetwork(ctx context.Context, networkID string, property NetworkProperty) (*Response, error)

	// ClearOrganization deletes an organization override. Forward support permission is required.
	ClearOrganization(ctx context.Context, orgID string, property OrgProperty) (*Response, error)

	// Current returns property values for the authenticated user's organization.
	Current(ctx context.Context, filter PropertyFilter) (PropertyValues, *Response, error)

	// CurrentValue reads the effective value of ONE organization property for the caller's organization (GET /api/config/{property};
	// ConfigController.getConfigValue, @RequiresUser: any signed-in user). Use it instead of Current when only a few properties are needed:
	// Current returns the whole map, including the organization's ALLOWED_IPS and its password policy, which a restricted session may not read.
	// Forward answers {"<property>": value}, with the key in lower case on a 26.11 build seen live (predict_modeling), so the value is found
	// ignoring case. The name must be an OrgProperty name (letters, digits, underscore); it is sent upper-cased, as the enum spells it. A name
	// Forward does not define is its 400 "No enum constant ...OrgProperty.X", which is ErrUnknownOrgProperty (IsUnknownOrgProperty). Preview:
	// from Forward's source, not a live call to this method.
	CurrentValue(ctx context.Context, property OrgProperty) (json.RawMessage, *Response, error)

	// Global returns default property values. Global writes require Forward-admin
	// permission, while visibility depends on the requested filter.
	Global(ctx context.Context, filter PropertyFilter) (PropertyValues, *Response, error)

	// NetworkOverrides reads, for one network property, the value each network of the CALLER'S org overrides it to, keyed by network id (GET
	// /api/config/{property}/overrides; only networks the caller may edit settings of). It reads the caller's own org; there is no cross-org form.
	NetworkOverrides(ctx context.Context, property NetworkProperty) (map[string]json.RawMessage, *Response, error)

	// NetworkValue reads one network property's effective value for a network (GET /api/networks/{networkId}/config/{property};
	// EDIT_NETWORK_SETTINGS, or Forward support). The answer maps the property name to its value.
	NetworkValue(ctx context.Context, networkID string, property NetworkProperty) (map[string]json.RawMessage, *Response, error)

	// Organization returns property values for orgID. Forward support permission
	// is required.
	Organization(ctx context.Context, orgID string, filter PropertyFilter) (PropertyValues, *Response, error)

	// OrganizationOverrides reads, for ONE property, the stored org-level rows, keyed by org id (GET /api/global-config/{property}?
	// view=overrides; Forward support: a support user sees the orgs it supports, a Forward admin all). It is NOT effective values:
	// ConfigController.getConfigPropertyOverrides returns configService.getOrgOverrides(property), every stored org-level row
	// (ObjType.CONFIG_PROPERTY_ORG), uncached, including rows equal to the default and nothing from the global level. Forward does not
	// remove an org's config rows when the org is deleted, so the map can hold ORPHAN rows of orgs that no longer exist (measured on
	// primary 1.0.0-261118: 332 PROD rows, all for ids whose GET /api/orgs/{id}/config is 404); intersect with the live org list before
	// trusting a count. An org absent from the map has no org-level row and takes the global value (the global override if one is set,
	// else the built-in default); an org with a row reads that row whatever the global value is. For what an org actually gets, use
	// EffectiveOrganizationValue or OrganizationValue. One call per property, not one per org.
	OrganizationOverrides(ctx context.Context, property OrgProperty) (map[string]json.RawMessage, *Response, error)

	// OrganizationValue reads one property's effective value for one org (GET /api/orgs/{orgId}/config/{property}; Forward support).
	OrganizationValue(ctx context.Context, orgID string, property OrgProperty) (PropertyValues, *Response, error)

	// PatchNetworkOverrides sets or clears one network property's per-network overrides in a single call (PATCH /api/config/{property}/overrides,
	// 204), keyed by network id. A non-nil value sets that network's override; a nil value deletes it, so the network falls back to the org's
	// value. Every id must be a network of the caller's own org (400 otherwise) and one the caller may edit settings of (403 otherwise).
	// Forward checks the ids, the permissions and every value before it writes anything, so a refused call wrote nothing; the one step that is not
	// atomic is that the sets are stored first and the deletes after, so a failure in between keeps the sets. A value of the wrong type may come
	// back as a 500 rather than a 400 (the parse throws IllegalArgumentException). Preview: ConfigController.patchNetworkPropertyOverrides at
	// master fe0daf63c899; the same mapping exists at both pins. An empty map sends nothing and returns nil.
	PatchNetworkOverrides(ctx context.Context, property NetworkProperty, updates map[string]any) (*Response, error)

	// SetCurrent sets an override for the authenticated user's organization.
	SetCurrent(ctx context.Context, property OrgProperty, value string) (PropertyValues, *Response, error)

	// SetGlobal sets a global default. Forward-admin permission is required. A property whose Level is ORG (not BOTH) has no global default to
	// change: Forward answers 403 "Permission denied. Global default value cannot be changed for <property>", which is
	// IsGlobalDefaultNotAllowed (ErrorResponse.GateProperty names it); use SetOrganization for those, one org at a time.
	SetGlobal(ctx context.Context, property OrgProperty, value string) (PropertyValues, *Response, error)

	// SetNetwork sets one network's override of a network property and returns the value as Forward parsed it (PUT
	// /api/networks/{networkId}/config/{property}?value=; ConfigController.setNetworkConfigOverride, EDIT_NETWORK_SETTINGS, or Forward support).
	// value is the text form (for example AdvancedReachabilityAsync). An org admin's key can set the properties Forward defines per network
	// (NetworkProperty); one defined only for orgs, such as LOCATION_CONNECTIVITY_DIFFS, is a 400 (ErrUnknownNetworkProperty) and only an org
	// property write by Forward support can set it. A value the property's type rejects is a 400 that changes nothing; to set one property on
	// many networks in a single call use PatchNetworkOverrides. Seen live on 26.10.6 by the AutoCon6 workshop (advanced_reachability_analysis
	// to ASYNC on 67 networks). Preview: ConfigController, not in the published spec.
	SetNetwork(ctx context.Context, networkID string, property NetworkProperty, value string) (map[string]json.RawMessage, *Response, error)

	// SetOrganization sets an organization override. Forward support permission is required. This is the only way to set a property whose
	// Level is ORG: Forward answers a global write to one (SetGlobal) with a 403 (IsGlobalDefaultNotAllowed), so such a property is always
	// per organization and has to be set on each org you want it on. Most properties are ORG-level (for example
	// EDGE_ASSETS_IN_SOURCES and THREAT_EXPOSURE); Forward exposes no list of which, so the 403 is how you find out.
	SetOrganization(ctx context.Context, orgID string, property OrgProperty, value string) (PropertyValues, *Response, error)
}

// Organizations is the Organizations API family: what Client.Organizations sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Organizations interface {
	// Create creates an organization using the support-admin API.
	Create(ctx context.Context, input OrganizationCreateRequest) (*Organization, *Response, error)

	Current(ctx context.Context) (*Organization, *Response, error)

	// Delete deletes an organization (DELETE /api/admin/orgs/{orgId}).
	//
	// READ THIS BEFORE CALLING IT. Forward removes the org's networks with a database cascade, NOT through its network deletion path: the
	// networks' snapshot rows are deleted by SQL but SnapshotService.deleteAll does not run and the network deletion listeners do not, so the jobs-v2 FDB data stored under the networks
	// (fdb_blob_list rows under n/<org>/<network>/ and their large objects) is never reclaimed, and the garbage collector only walks networks
	// that still exist (FWD-20216, NetworkDeletionListener). Use DeleteOrgCleanly, which deletes each network properly first, unless the org
	// has no networks. Forward also answers 400 when the org still has users, disabled ones included. Needs the internal EDIT_ORG permission,
	// which only the system ADMIN role holds.
	Delete(ctx context.Context, orgID string) (*Organization, *Response, error)

	// DeleteNetworkInOrg deletes one network of ANY org the login supports (DELETE /api/orgs/{orgId}/networks/{networkId};
	// NetworkController.deleteNetworkForForwardSupport, @RequiresForwardSupport). Forward deletes the network's snapshots first and then the
	// network, the same clean path as Networks.Delete, and answers the network as it was. Unlike Networks.Delete it needs no permission in
	// the network's own org, so no impersonation. The NQE tutorial network is refused (400). It cannot be undone. Preview: not in the published
	// spec. Forward deletes the snapshots synchronously, so a large network can outlast a client or proxy timeout (a 504 says nothing about
	// whether it finished; read the network again).
	DeleteNetworkInOrg(ctx context.Context, orgID string, networkID string) (*Network, *Response, error)

	// DeleteOrgNetworks deletes EVERY network of an org, snapshots first, the clean way (DELETE /api/orgs/{orgId}/networks;
	// NetworkController.deleteOrgNetworks, internal EDIT_ORG). Forward refuses (400) unless the org is already disabled (SetEnabled false) and
	// refuses the NQE tutorial org. It cannot be undone, and one failing network stops it part way with the earlier ones already gone;
	// DeleteOrgCleanly reports each network instead. Preview: not in the published spec. Only the system ADMIN role holds EDIT_ORG.
	DeleteOrgNetworks(ctx context.Context, orgID string) (*Response, error)

	// List returns organizations visible to a Forward support principal.
	List(ctx context.Context) ([]Organization, *Response, error)

	// SetEnabled enables or disables an organization.
	SetEnabled(ctx context.Context, orgID string, enabled bool) (*Response, error)

	// Update renames an organization. Current updates the authenticated tenant;
	// an explicit ID uses the Forward-admin route.
	Update(ctx context.Context, orgID string, name string) (*Response, error)
}

// Checks is the Checks API family: what Client.Checks sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Checks interface {
	// CheckCategoryReport is ChecksReport for one category of check: NQE,
	// PREDEFINED, INTENT or ADVANCED. Directory, INTENT only, limits it to one
	// directory of intent checks. GET /api/snapshots/{snapshotId}/checks-report
	// ?type=&dir= (VIEW_CHECKS; NQE also needs the USE_NQE permission). Unpublished.
	CheckCategoryReport(ctx context.Context, snapshotID string, category string, directory string, options ChecksReportOptions, dst io.Writer) (int64, *Response, error)

	// ChecksReport streams the snapshot's checks as an .xlsx workbook to dst and
	// returns the bytes written. GET /api/snapshots/{snapshotId}/checksReport
	// (CheckController.getChecksReportXlsx; VIEW_CHECKS; the snapshot must be past
	// the CREATION stage). Unpublished: not in Forward's OpenAPI set. Nothing is
	// retried; write to a temporary file. A workbook is a ZIP, so a body that does
	// not start like one is an error.
	ChecksReport(ctx context.Context, snapshotID string, options ChecksReportOptions, dst io.Writer) (int64, *Response, error)

	// Create adds a check to snapshotID and returns it as evaluated.
	//
	// A persistent check is inherited and re-evaluated by later snapshots of the
	// same network, including Predict output; a non-persistent one belongs to this
	// snapshot alone. persistent is a pointer so that leaving it unstated defers
	// to whatever the appserver defaults to, rather than asserting a default here.
	Create(ctx context.Context, snapshotID string, check NewCheck, persistent *bool) (*CheckDetail, *Response, error)

	// CreatePersistent creates a check on snapshotID with persistent=true. The
	// snapshot must be a real snapshot; a successful check is inherited and
	// evaluated by later snapshots of that same network, including Predict output.
	CreatePersistent(ctx context.Context, snapshotID string, check NewCheck) (Identifier, *Response, error)

	// Deactivate disables one check on a snapshot.
	//
	// Forward deactivates rather than deletes: the check stops evaluating but its
	// history stays readable, so a later snapshot can still be explained.
	Deactivate(ctx context.Context, snapshotID string, checkID string) (*Response, error)

	// DeactivateAll disables every check on a snapshot.
	DeactivateAll(ctx context.Context, snapshotID string) (*Response, error)

	// Get returns one check with its diagnosis.
	Get(ctx context.Context, snapshotID string, checkID string) (*CheckDetail, *Response, error)

	// List returns checks evaluated on snapshotID, optionally filtered. The first GET primes Forward's
	// evaluation cache and can outlive an http.Client deadline while computation
	// continues server-side, so client-side timeout failures are re-issued up to
	// four times. Cancellation of the caller's context is never retried.
	List(ctx context.Context, snapshotID string, options ...CheckListOptions) ([]Check, *Response, error)

	// ListPredefined returns the built-in checks this appserver offers. GET
	// /api/predefinedChecks (getAvailablePredefinedChecks, published;
	// CheckController on primary 15398425a69 and stable 67e89c87124). It needs the
	// VIEW_PREDEFINED_CHECKS org permission.
	ListPredefined(ctx context.Context) ([]PredefinedCheck, *Response, error)

	// Update edits a check in place, keeping its id, and returns it (PATCH /api/snapshots/{snapshotId}/checks/{checkId};
	// CheckController.updateCheck, EDIT_CHECKS). Preview: the route is not in the published OpenAPI; the body is from Forward's source, not
	// yet seen live. On a predicted snapshot Forward refuses check edits unless the PREDICT_UNSANDBOXED property is on. An empty patch is
	// refused here rather than sent, since it changes nothing.
	Update(ctx context.Context, snapshotID string, checkID string, patch CheckPatch) (*Check, *Response, error)
}

// DeviceTags is the DeviceTags API family: what Client.DeviceTags sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type DeviceTags interface {
	// AddBatch creates tag definitions. Forward ignores definitions that already
	// exist.
	AddBatch(ctx context.Context, networkID string, tags []DeviceTag) (*Response, error)

	// AddBatchTo applies existing tag names to devices. It sends no snapshotId, so Forward writes it at the
	// network's STAGING instant: an instant after every snapshot, used for timeline entities that apply to
	// any future snapshot (NetworkInstant.staging). The devices must be collection sources (Forward validates
	// unless told not to; this SDK always lets it). From Forward's source, not a live test: List with
	// "devices" also sends no snapshotId and reads "active tags as of the staging instant"
	// (DeviceTagsService.getAllActiveDeviceTags), so a tag applied here should show in that List at once,
	// before any snapshot exists. A read AT a snapshot (not offered by this SDK) shows only what was active then.
	AddBatchTo(ctx context.Context, networkID string, devices []string, tags []string) (*Response, error)

	// AddBatchToAt is AddBatchTo with the change taking effect from a snapshot (POST ?action=addBatchTo&snapshotId=; DeviceTagController
	// addDeviceTagsToDevices, MANAGE_COLLECTION_SOURCES). Forward's spec: "the change will take effect starting from the identified network
	// Snapshot", so an NQE read of that snapshot (device.tagNames) shows the tags at once, where AddBatchTo (no snapshot, the staging instant)
	// shows them only from the next snapshot. The devices are validated against the SNAPSHOT's device and endpoint catalogs, not the network's
	// collection sources, so a virtual system or an access point the collection-source form refuses ("Unrecognized device name(s)") is
	// accepted if the snapshot has it; a name the snapshot lacks is refused with that same 400. From the spec and source, not a live test. The
	// SDK never sends validateDevices=false.
	AddBatchToAt(ctx context.Context, networkID string, snapshotID string, devices []string, tags []string) (*Response, error)

	// DeleteTag deletes a tag definition AND removes it from every device across
	// the network's whole timeline, not just the next snapshot (deleteDeviceTag).
	// RemoveBatchFrom only takes a tag off named devices and leaves the
	// definition. A tag that does not exist counts as success. DELETE
	// /api/networks/{networkId}/device-tags/{tagName} (MANAGE_COLLECTION_SOURCES;
	// 204). There is no undo: re-creating the tag does not restore its
	// assignments.
	DeleteTag(ctx context.Context, networkID string, tag string) (*Response, error)

	// List returns tag definitions. with is an optional appserver expansion such
	// as "devices". Bare arrays and {"tags": [...]} are both accepted.
	// With "devices" and no snapshot it reflects the staging instant, i.e. tags already applied by AddBatchTo and
	// RemoveBatchFrom, whether or not a snapshot has been taken since.
	List(ctx context.Context, networkID string, with string) ([]DeviceTag, *Response, error)

	// ListAt returns the tags with the devices that carried each as of a snapshot (GET /device-tags?with=devices&snapshotId=;
	// getDeviceTagsWithDevices, VIEW_NETWORK_AND_SNAPSHOTS). Without a snapshot List reads the staging instant instead. Forward uses the
	// snapshot to canonicalize the case of device names. Only the with=devices form takes a snapshot, so it is always sent.
	ListAt(ctx context.Context, networkID string, snapshotID string) ([]DeviceTag, *Response, error)

	// RemoveBatchFrom takes the named tags off the named devices (POST ?action=removeBatchFrom, operationId removeDeviceTagsFromDevices in
	// Forward's published device-tags spec): the undo of AddBatchTo. It sends no snapshotId, so the change applies to the network's next
	// snapshot and the devices must be collection sources, exactly as AddBatchTo behaves. Tag definitions are left in place; removing a tag
	// from a device that does not carry it is not an error on the server.
	RemoveBatchFrom(ctx context.Context, networkID string, devices []string, tags []string) (*Response, error)

	// RemoveBatchFromAt is RemoveBatchFrom taking effect from a snapshot (POST ?action=removeBatchFrom&snapshotId=), validated against the
	// snapshot's devices and endpoints like AddBatchToAt.
	RemoveBatchFromAt(ctx context.Context, networkID string, snapshotID string, devices []string, tags []string) (*Response, error)

	// UpdateTag renames a device tag or changes its colour, and returns it. PATCH
	// /api/networks/{networkId}/device-tags/{tagName} (updateDeviceTag;
	// MANAGE_COLLECTION_SOURCES).
	//
	// Forward's service documents this as "creates or updates", so patching a tag
	// that does not exist creates it with the patched values; check List first if
	// that matters. Renaming moves the tag's assignments with it. An empty patch is
	// refused.
	UpdateTag(ctx context.Context, networkID string, tag string, patch DeviceTagPatch) (*DeviceTag, *Response, error)
}

// Performance is the Performance API family: what Client.Performance sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Performance interface {
	// CollectorDevices fetches the devices a collector should poll for performance data, as Forward sends them: the serialized
	// PerfDevicesPB protobuf (GET /api/networks/{id}/performance/devices; PerformanceController.getDevicesForPerfMonitoring,
	// @RequiresCollector; Preview: not in the published spec). The collector identity is the one Collectors.Register returned, and it
	// is sent for this request only. A collector is assigned to devices, not to a network: the answer holds the network's devices that
	// name this collector, plus the devices that name none when this collector is the network's default (Collectors.Attachment),
	// limited to devices with a known vendor OS, collection on and SNMP performance collection on. The bytes are empty when there are
	// none. The SDK has no protobuf dependency, so decoding them is the caller's.
	CollectorDevices(ctx context.Context, networkID string, identity CollectorIdentity) ([]byte, *Response, error)

	// DefaultThresholds returns the default performance thresholds a device's own thresholds fall back to (GET
	// /api/default-performance-thresholds; PerformanceController.getDefaultPerformanceThresholds; any signed-in user; Preview: not in
	// the published spec). The per-device overrides are ClassicDevicePatch's High* and Medium* fields.
	DefaultThresholds(ctx context.Context) (*DefaultPerformanceThresholds, *Response, error)

	// DeviceHealth returns one device's utilization level and health issues in the 24 hours before query.EndTime, as Forward sent it
	// (GET /api/networks/{id}/devices/{device}/health; VIEW_PERFORMANCE_DATA; Preview: not in the published spec). Only query's EndTime
	// and SnapshotID are used.
	DeviceHealth(ctx context.Context, networkID string, device string, query MetricQuery) (JSONDocument, *Response, error)

	DeviceMetricHistory(ctx context.Context, networkID string, query MetricQuery, input DeviceMetricHistoryRequest) (*DeviceMetricHistoryResponse, *Response, error)

	DeviceMetricHistoryDocument(ctx context.Context, networkID string, query MetricQuery, input DeviceMetricHistoryRequest) (JSONDocument, *Response, error)

	DeviceMetrics(ctx context.Context, networkID string, query MetricQuery) (*DeviceMetrics, *Response, error)

	DeviceMetricsDocument(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error)

	// DevicesHealth is DeviceHealth for several devices, keyed by device name (POST /api/networks/{id}/device-health;
	// VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
	DevicesHealth(ctx context.Context, networkID string, devices []string, query MetricQuery) (JSONDocument, *Response, error)

	// DevicesWithData lists the devices that have performance data in a time window around a snapshot (GET
	// /api/networks/{id}/performance/devices-with-data; VIEW_PERFORMANCE_DATA; Preview: not in the published spec). Only query's
	// StartTime, EndTime and SnapshotID are used; with none, the window is the 24 hours before the snapshot's end. It is the quick
	// way to see whether an upload or a collector's polling reached Forward.
	DevicesWithData(ctx context.Context, networkID string, query MetricQuery) ([]string, *Response, error)

	GenerateSynthetic(ctx context.Context, networkID string, input SyntheticPerformanceRequest) (SyntheticPerformanceResult, *Response, error)

	// InterfaceHealth returns one interface's utilization and problems in the 24 hours before query.EndTime, as Forward sent it (GET
	// /api/networks/{id}/devices/{device}/interfaces/{interface}/health; VIEW_PERFORMANCE_DATA; Preview: not in the published spec).
	// A "/" in the interface name is percent-encoded.
	InterfaceHealth(ctx context.Context, networkID string, device string, interfaceName string, query MetricQuery) (JSONDocument, *Response, error)

	InterfaceMetricHistory(ctx context.Context, networkID string, query MetricQuery, input InterfaceMetricHistoryRequest) (*InterfaceMetricHistoryResponse, *Response, error)

	InterfaceMetricHistoryDocument(ctx context.Context, networkID string, query MetricQuery, input InterfaceMetricHistoryRequest) (JSONDocument, *Response, error)

	InterfaceMetrics(ctx context.Context, networkID string, query MetricQuery) (*InterfaceMetrics, *Response, error)

	InterfaceMetricsDocument(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error)

	// PatchSettings changes a network's SNMP performance-collection settings and returns the result (PATCH
	// /api/networks/{id}/performance/settings; PerformanceController.patchPerformanceSettings, JsonProp semantics:
	// an absent property is left alone; EDIT_PERFORMANCE_SETTINGS; Preview: not in the published spec). Forward pushes the new
	// settings to the collectors on the way, so enabling starts SNMP polling of every device that is eligible (a known vendor OS,
	// collection on, SNMP performance collection on). Collectors.SetPerformanceCollection sends the same route but always writes
	// enabled and cannot set the interval.
	//
	// Dependencies: while enabled is true the network needs at least one SNMP credential (Credentials.CreateSNMPCredential), and any
	// patch that leaves it true is a 400 "At least one SNMP credential has to be configured in the network" without one. Forward stores
	// the new settings BEFORE it checks, so that 400 does not undo the change. Polling also needs, per device, collection on and SNMP
	// collection on (ClassicDevicePatch.EnableSNMPCollection) with a known vendor OS, and a collector (the device's CollectorID, else
	// the network's default). Readiness reports what is missing.
	PatchSettings(ctx context.Context, networkID string, patch PerformanceSettingsPatch) (*PerformanceSettings, *Response, error)

	// RawSNMPData has a collector walk a device over SNMP with a stored credential and returns the raw readings as Forward sent them
	// (GET /api/networks/{id}/performance/raw-data; PerformanceController.getRawSnmpData, answered when the collector is done;
	// MANAGE_COLLECTION_SOURCES; Preview: not in the published spec). It is the check to run before enabling performance collection
	// for a device: it shows whether the credential, port and address work and which OIDs answer. The walk runs from the network's
	// DEFAULT collector (Collectors.Attachment), not from the collector the device is pinned to, and Forward answers 400 when the network
	// has no online, supported default collector or when Device is not a classic device with a known vendor OS.
	RawSNMPData(ctx context.Context, networkID string, request RawSNMPRequest) (JSONDocument, *Response, error)

	// Settings reads a network's SNMP performance-collection settings (GET /api/networks/{id}/performance/settings;
	// PerformanceController.getPerformanceSettings; VIEW_PERFORMANCE_SETTINGS; Preview: not in the published spec).
	Settings(ctx context.Context, networkID string) (*PerformanceSettings, *Response, error)

	UnhealthyDevices(ctx context.Context, networkID string, query MetricQuery) (JSONDocument, *Response, error)

	UnhealthyInterfaces(ctx context.Context, networkID string, query MetricQuery, input UnhealthyInterfacesRequest) (JSONDocument, *Response, error)

	// UploadWithIdentity uploads performance data using the collector identity
	// returned by Collectors.Register. payload is a serialized PerfDataPB
	// (PerfProtos.proto in Forward's client-structs); the SDK has no protobuf
	// dependency, so encoding it is the caller's. As far as the
	// controller and PerformanceService.save show, Forward checks that the
	// collector belongs to the network's organization and nothing else: nothing
	// there checks that the network is the collector's default, that the device
	// names exist, or that this collector is the one a device names. Cumulative
	// interface counters are expected; Forward derives rates and utilization from
	// the difference to the device's previous sample. A sample whose CPU or
	// memory is above 1 is dropped for the whole device; one interface whose
	// utilization is above 1 is dropped on its own. Within a batch
	// keep one device's entries in time order. Forward answers 204.
	UploadWithIdentity(ctx context.Context, networkID string, identity CollectorIdentity, payload []byte) (*Response, error)
}

// Capabilities is the Capabilities API family: what Client.Capabilities sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Capabilities interface {
	// All returns known statuses in stable capability-name order.
	All() []CapabilityStatus

	// Profile returns a copy of the selected deployment profile.
	Profile() CapabilityProfile

	// Support reports whether capability is supported, unsupported, or unknown.
	Support(capability Capability) CapabilityStatus
}

// Raw is the Raw API family: what Client.Raw sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Raw interface {
	// Do executes an arbitrary Forward API request and decodes dst using the same
	// rules as Client.Do.
	Do(ctx context.Context, request RawRequest, dst any) (*Response, error)

	// DoJSON JSON-encodes payload and executes an arbitrary Forward API request.
	DoJSON(ctx context.Context, method string, path string, payload any, dst any) (*Response, error)
}

// Collectors is the Collectors API family: what Client.Collectors sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Collectors interface {
	// Attach makes the collector with the given username the network's DEFAULT collector, used by the devices that name none. PUT /api/networks/{networkId}/collector
	// (JSON {"username": ...}). The username is the collector's (for example "collector-7ddceb66"), not its name or id.
	// Check the result with Attachment, which reads the same route.
	Attach(ctx context.Context, networkID string, input CollectorAttachmentRequest) (*Response, error)

	Attachment(ctx context.Context, networkID string) (*CollectorAttachment, *Response, error)

	Delete(ctx context.Context, collectorIDOrName string) (*Response, error)

	// GetOrganizationSettings returns the collection settings stored for the
	// organization's collectors: only what was set, so a nil field is a default,
	// not zero. GET /api/collection-settings (CollectionController;
	// VIEW_ORG_COLLECTION_SETTINGS). The sibling ?for=collector form is the
	// collectors' own binary feed, not this. Preview: not in the published spec.
	GetOrganizationSettings(ctx context.Context) (*OrgCollectionSettings, *Response, error)

	// GetSettings returns the collection settings stored for a collector: only
	// what was set, so a nil field is a default, not zero. GET
	// /api/collectors/{collectorId}/collection-settings (CollectionController;
	// VIEW_COLLECTORS). Preview: not in the published spec.
	GetSettings(ctx context.Context, collectorID string) (*CollectorCollectionSettings, *Response, error)

	List(ctx context.Context) ([]Collector, *Response, error)

	// Metrics reads a collector's own resource metrics over [start, end], to tell
	// whether the collector itself was the bottleneck during a collection. GET
	// /api/collectors/{collectorName}/metrics (CollectorMetricsController
	// .getCollectorMetrics, Preview; org DEBUG_COLLECTORS, which most logins lack:
	// a typed 403, errors.Is(err, ErrPermissionDenied) and MissingPermission). The
	// collector is named by its NAME, not its id.
	//
	// samplesPerSeries is how many samples each series keeps when the window holds
	// more (Forward downsamples, keeping each series' peaks and troughs, and unions
	// the kept samples, so the result can exceed it); zero uses Forward's default
	// of 500, and Forward caps it at 5000. It must otherwise be at least 2.
	// Samples come oldest first. A collector that has uploaded nothing in the
	// window returns none. An unknown collector name is a 400, not a 404.
	Metrics(ctx context.Context, collectorName string, start time.Time, end time.Time, samplesPerSeries int) ([]CollectorMetricSample, *Response, error)

	PatchOrganizationSettings(ctx context.Context, patch OrgCollectionSettingsPatch) (*Response, error)

	PatchSettings(ctx context.Context, collectorID string, patch CollectorCollectionSettingsPatch) (*Response, error)

	Register(ctx context.Context, input CollectorRegistrationRequest) (*CollectorRegistration, *Response, error)

	// ResetKey issues a new password for the named collector, keeping its username,
	// and invalidates the old one at once: a running collector stops authenticating
	// until it is given the new token. POST /api/orgs/current/collectors/resetPassword
	// (ClientController.resetCollectorPassword, @RequiresUser; the appserver then
	// requires the caller to be allowed to modify that collector). Preview.
	ResetKey(ctx context.Context, collectorName string) (*CollectorCredentials, *Response, error)

	SetPerformanceCollection(ctx context.Context, networkID string, settings PerformanceCollectionSettings) (*Response, error)

	// Upgrade asks Forward to upgrade one collector to the current release and returns once the upgrade is queued, not
	// finished. Poll List until CollectorUpdating clears, then read UpdateStatus and Version: UP_TO_DATE means it worked,
	// while a failed or timed-out upgrade also clears CollectorUpdating and leaves UpdateStatus OUTDATED. The upgrade is a
	// blocking task, so it first waits for a collection running on that collector to finish (Forward gives the task 15
	// minutes), then restarts the collector: collections queued behind it fail with "Collector version changed". POST
	// /api/collectors/{collectorName}?action=upgrade (CollectorSetupController.upgradeCollector; MANAGE_COLLECTORS, org
	// admin; Preview: not in the published spec). Forward answers 400 when the collector is offline, never connected, not
	// remotely upgradeable, already up to date, at end of life, bundled with an on-prem deployment, or upgrades are off
	// (BUNDLED_ONLY); 404 for an unknown name; and 409 when an upgrade is already in progress. See Collector.UpgradeBlocker
	// for the checks that can be made first.
	Upgrade(ctx context.Context, collectorName string) (*Response, error)

	// UpgradeAll queues an upgrade for every collector in the org that can be upgraded remotely (online, outdated or
	// unsupported but not end of life, not bundled, installed as a service) and returns their names; collectors with an
	// upgrade already queued are left out. An empty list means nothing qualified. POST /api/collectors?action=upgrade
	// (CollectorSetupController.upgradeCollectors; MANAGE_COLLECTORS; Preview: not in the published spec). The order of
	// the names is unspecified. Like Upgrade, it returns when queued, with the same polling and restart behaviour.
	UpgradeAll(ctx context.Context) ([]string, *Response, error)
}

// Backups is the Backups API family: what Client.Backups sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Backups interface {
	// BackupProgress returns the running (or latest) cluster backup or restore, or
	// (nil, nil) when there is none. GET /api/backups?view=progress (any signed-in
	// user: Forward uses it to show the restore banner). Preview.
	BackupProgress(ctx context.Context) (*ClusterOperation, *Response, error)

	// CancelOperation cancels the backup or restore that is running, whichever it
	// is; with none running it does nothing. It is not addressed by ID. POST
	// /api/backups?action=cancel (ADMINISTER_SYSTEM). Preview. Check BackupProgress
	// first to know what it would stop.
	CancelOperation(ctx context.Context) (*Response, error)

	// DeleteBackup deletes one backup. storageType INTERNAL or S3 deletes it from
	// that storage only; StorageTypeAll deletes it from every storage it is in.
	// There is no default, so the choice is always stated. There is no undo. DELETE
	// /api/backups/{backupId}[?storageType=] (ADMINISTER_SYSTEM; 204). Preview.
	DeleteBackup(ctx context.Context, backupID int64, storageType StorageType) (*Response, error)

	// GetS3Storage returns the stored S3 settings, or nil when none are configured. The secret key is write-only: Forward
	// marks S3StorageSettings.secretKey @JsonIgnore, so no response carries it, and the SDK clears SecretKey on every
	// decoded response anyway so a caller can never read one back.
	GetS3Storage(ctx context.Context) (*S3StorageSettings, *Response, error)

	GetSettings(ctx context.Context, storageType StorageType) (*BackupSettings, *Response, error)

	Last(ctx context.Context, storageType StorageType, triggerType BackupTriggerType) (*BackupResult, *Response, error)

	// LastRestoreResult returns how the last completed restore went, or (nil, nil)
	// when there has been none. GET /api/backups?view=lastRestoreResult
	// (ADMINISTER_SYSTEM). Preview.
	LastRestoreResult(ctx context.Context) (*ClusterOperation, *Response, error)

	// ListBackups returns the backups Forward holds, in every storage. GET
	// /api/backups (ClusterBackupRestoreController.getBackups; ADMINISTER_SYSTEM).
	// Preview: not in the published spec.
	ListBackups(ctx context.Context) ([]AppBackup, *Response, error)

	// RestoreSnapshot starts restoring one snapshot from a backup and returns its
	// initial status; follow it with SnapshotRestoreStatus. It adds a snapshot back
	// to its network and changes no existing one. Forward refuses with 409 when the
	// snapshot is already on disk, and 400 when the backup was written by a newer
	// Forward than the running one (upgrade first). The snapshot must have been
	// backed up. POST /api/snapshots/{snapshotId}?action=restore (network permission
	// RESTORE_SNAPSHOT). Preview.
	RestoreSnapshot(ctx context.Context, snapshotID string) (*SnapshotRestoreStatus, *Response, error)

	SetS3BucketOwnership(ctx context.Context, settings S3StorageSettings) (*Response, error)

	// SnapshotRestoreStatus returns how a snapshot's restore is going, or (nil, nil)
	// when none has been started (Forward answers 204). GET
	// /api/snapshots/{snapshotId}?view=restoreStatus (VIEW_NETWORK_AND_SNAPSHOTS).
	// Preview.
	SnapshotRestoreStatus(ctx context.Context, snapshotID string) (*SnapshotRestoreStatus, *Response, error)

	Trigger(ctx context.Context, input BackupTriggerRequest) (*Response, error)

	UpdateS3Storage(ctx context.Context, patch S3StorageSettingsPatch) (*S3StorageSettings, *Response, error)

	UpdateSettings(ctx context.Context, storageType StorageType, patch BackupSettingsPatch) (*BackupSettings, *Response, error)
}

// Browser is the Browser API family: what Client.Browser sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Browser interface {
	Cookies() []*http.Cookie

	// CurrentSession reads the session's principal together with its resolved
	// roles, so a caller can prove a sign-in carries the access it needs.
	CurrentSession(ctx context.Context) (*BrowserSession, *Response, error)

	CurrentUser(ctx context.Context) (*BrowserUser, *Response, error)

	Impersonate(ctx context.Context, targetUserID string) (*BrowserLoginResult, *Response, error)

	ImpersonateWithServiceCredential(ctx context.Context, targetUserID string) (*BrowserLoginResult, *Response, error)

	// Login performs Forward's browser login: a CSRF token from the public API
	// endpoint (GET /api/public/csrf), falling back to the token embedded in the
	// login page, then the Spring Security form login (POST /login).
	Login(ctx context.Context) (*BrowserLoginResult, error)

	LoginLegacy(ctx context.Context, input BrowserLoginRequest, csrf BrowserCSRFToken) (*BrowserLoginResult, *Response, error)

	LoginPageCSRF(ctx context.Context) (*BrowserCSRFToken, *Response, error)

	PublicCSRFAPI(ctx context.Context) (*BrowserCSRFToken, *Response, error)

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
	SAMLAssertionConsumer(ctx context.Context, registrationID string, fields url.Values) (*Response, error)

	// SAMLAuthenticationRequest starts SP-initiated SAML sign-in to the org that
	// owns registrationID (GET /saml2/authenticate/{registrationId}, served by
	// Forward's SAML filter chain). Forward answers with the POST-binding form
	// addressed to the IdP. The request runs on this client's cookie jar, so the
	// same client must carry the assertion back through SAMLAssertionConsumer.
	SAMLAuthenticationRequest(ctx context.Context, registrationID string) (*BrowserSAMLForm, *Response, error)
}

// Endpoints is the Endpoints API family: what Client.Endpoints sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Endpoints interface {
	AddBatch(ctx context.Context, networkID string, endpointType string, endpoints []Endpoint) (*Response, error)

	// ApprovedCLICommands returns the CLI commands the organization allows endpoint profiles to run. GET /api/approved-cli-commands
	// (CliCommandsController; VIEW_ENDPOINT_PROFILES; on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
	ApprovedCLICommands(ctx context.Context) (*ApprovedCLICommands, *Response, error)

	// AssessCLICommands says which of commands the organization's approved list allows. Forward matches each command against
	// every approved regex with find(), not a full match, and an endpoint profile is collected only if all of its commands
	// are approved. Nothing is stored. POST /api/approved-cli-commands?action=assess, JSON list of strings
	// (CliCommandsController.assessCliCommands; MANAGE_ENDPOINT_PROFILES, although nothing is written; on primary
	// 15398425a69 and stable 67e89c87124). Preview: not in the published spec. An empty list is answered without a request.
	AssessCLICommands(ctx context.Context, commands []string) (*CLICommandAssessment, *Response, error)

	CreateProfile(ctx context.Context, input EndpointProfileRequest) (*EndpointProfile, *Response, error)

	// CreateProfileDefinition creates an endpoint profile of any type from def -- the read type, so a profile read back can be copied -- and
	// returns it as Forward stored it, with its new ID ("SNMP-5"). Only the fields of def.Type are sent (CLI, SNMP or HTTP, per the published
	// CliEndpointProfileDef / SnmpEndpointProfileDef / HttpEndpointProfileDef); ID, attribution and Raw are never sent. POST
	// /api/endpoint-profiles?type= (createCli/Snmp/HttpEndpointProfile; 201 with the stored profile; MANAGE_ENDPOINT_PROFILES). A CLI
	// profile's commands run only if the organization has approved them (ApprovedCLICommands).
	CreateProfileDefinition(ctx context.Context, def EndpointProfile) (*EndpointProfile, *Response, error)

	// Delete removes one endpoint from a network.
	//
	// WHY THIS EXISTS. Forward's device-name namespace is SHARED between endpoints
	// and classic devices. A name left behind as an endpoint after the device was
	// reclassified as classic is a stale record of an earlier sync, and it
	// collides with the classic putBatch forever until it is removed. Deleting it
	// is therefore part of converging inventory, not a cleanup nicety.
	//
	// A 404 is SUCCESS: the desired state, that the endpoint is absent, already
	// holds. This matches DeleteGroup and DeleteDeviceAccessLabel, and it is what
	// makes the call safe to repeat.
	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// DeleteProfile deletes an endpoint profile by its full ID. A 404 is success (already gone). Forward REFUSES to delete a profile that any
	// endpoint in any of the organization's networks still uses -- a 400 "Profile is still used in N network(s)", ErrEndpointProfileInUse --
	// rather than orphaning the endpoints, so an undo reassigns those endpoints first (Endpoints.Patch) and deletes after. Deleting also drops
	// the profile's stored connectivity results. DELETE /api/endpoint-profiles/{profileId} (published deleteEndpointProfile;
	// NetworkEndpointService.deleteProfile; MANAGE_ENDPOINT_PROFILES).
	DeleteProfile(ctx context.Context, profileID string) (*Response, error)

	// GetProfile returns one endpoint profile by its full ID ("CLI-7"), or (nil, nil) when it does not exist. GET
	// /api/endpoint-profiles/{profileId} (published getEndpointProfile; VIEW_ENDPOINT_PROFILES).
	GetProfile(ctx context.Context, profileID string) (*EndpointProfile, *Response, error)

	List(ctx context.Context, networkID string) ([]Endpoint, *Response, error)

	ListProfiles(ctx context.Context, profileType string) ([]EndpointProfile, *Response, error)

	ListTestStatuses(ctx context.Context, networkID string) ([]SourceTestStatus, *Response, error)

	Patch(ctx context.Context, networkID string, name string, endpointType string, patch EndpointPatch) (*Response, error)

	// UpdateApprovedCLICommands replaces the organization's approved CLI command list with a Forward-signed file; a list
	// cannot be authored locally, because Forward verifies the signature and that the identifiers match the organization
	// and answers 400 ("Signature mismatch" or "Identifier mismatch") otherwise. The file is sent as is. This changes which
	// endpoint profiles are collected. POST /api/approved-cli-commands?action=update, multipart "file"
	// (CliCommandsController.updateApprovedCliCommands; MANAGE_ENDPOINT_PROFILES; on primary 15398425a69 and stable
	// 67e89c87124). Preview: not in the published spec.
	UpdateApprovedCLICommands(ctx context.Context, fileName string, signed []byte) (*ApprovedCLICommands, *Response, error)

	// UpdateProfile changes an endpoint profile in place and returns the stored
	// profile. PATCH /api/endpoint-profiles/{profileId}, one route per type
	// (updateCli/Snmp/HttpEndpointProfile; MANAGE_ENDPOINT_PROFILES). The change
	// applies to every endpoint using the profile from its next collection, so
	// check Endpoints.List for what uses it first. Forward answers 404 for an
	// unknown profile. All three routes are published.
	UpdateProfile(ctx context.Context, profileID string, patch EndpointProfilePatch) (*EndpointProfile, *Response, error)
}

// Locations is the Locations API family: what Client.Locations sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Locations interface {
	// Assign moves devices to locations in one atlas patch (PATCH .../atlas; the map is device name to location id). Forward refuses the WHOLE
	// patch with a 400 "Unrecognized devices cannot be moved: [r1, ...]" (ErrUnrecognizedDevices, ErrorResponse.UnrecognizedDevices) when
	// any device is not in the network's classic-device list or atlas, even if it is in every snapshot, as for a network fed only by snapshot
	// uploads. Seen live on 26.10.6 (the AutoCon6 workshop): create each such device with ClassicDevices.Create (collect false, no
	// credentials) and assign again.
	Assign(ctx context.Context, networkID string, patch AtlasPatch) (*Response, error)

	// Atlas returns the network's device atlas: which location each located device is in, as device name to
	// location ID, in Forward's name order. Cloud locations and devices with no fixed location are not in it.
	// GET /api/networks/{networkId}/atlas (getAtlas, published; VIEW_NETWORK_AND_SNAPSHOTS). Despite the name
	// this is a device-to-location map, not a geographic database: Forward has no city or coordinate lookup.
	Atlas(ctx context.Context, networkID string) (map[string]string, *Response, error)

	// AtlasByLocation returns the atlas grouped by location, telling apart devices placed by hand, by anchoring
	// and by a deviceGlobs match, in Forward's location-ID order. It is the way to confirm that a location's
	// DeviceGlobs put the devices you expect there. Empty locations are omitted, and so are cloud locations and
	// devices with no fixed location. GET /api/networks/{networkId}/atlas?v=2 (VIEW_NETWORK_AND_SNAPSHOTS).
	// Unpublished: not in Forward's OpenAPI set.
	AtlasByLocation(ctx context.Context, networkID string) ([]AtlasLocation, *Response, error)

	Create(ctx context.Context, networkID string, input LocationCreateRequest) (*Location, *Response, error)

	CreateCluster(ctx context.Context, networkID string, locationID string, input DeviceCluster) (*Response, error)

	// Delete removes a location; a location that does not exist counts as success.
	// DELETE /api/networks/{networkId}/locations/{locationId} (deleteLocation,
	// published; EDIT_TOPOLOGY_LAYOUT; 204). Forward recomputes dynamic assignment
	// afterwards. I did not trace where devices assigned to it end up.
	Delete(ctx context.Context, networkID string, locationID string) (*Response, error)

	// DeleteCluster removes a device cluster from a location; one that is already gone counts as success. A 404 that
	// means the route is not served (ErrEndpointNotServed) is still an error.
	// DELETE /api/networks/{networkId}/locations/{locationId}/clusters/{clusterName} (LocationController.deleteDeviceCluster).
	DeleteCluster(ctx context.Context, networkID string, locationID string, clusterName string) (*Response, error)

	// Get returns one location, or (nil, nil) when it does not exist. GET
	// /api/networks/{networkId}/locations/{locationId} (getLocation, published;
	// VIEW_NETWORK_AND_SNAPSHOTS).
	Get(ctx context.Context, networkID string, locationID string) (*Location, *Response, error)

	List(ctx context.Context, networkID string) ([]Location, *Response, error)

	ListClusters(ctx context.Context, networkID string, locationID string) ([]DeviceCluster, *Response, error)

	// Patch changes a location and returns it. PATCH
	// /api/networks/{networkId}/locations/{locationId} (patchLocation, published;
	// EDIT_TOPOLOGY_LAYOUT). Forward then recomputes dynamic device assignment. This
	// is the idempotent update: Get the location, compare, Patch what differs.
	Patch(ctx context.Context, networkID string, locationID string, patch LocationPatch) (*Location, *Response, error)

	PatchCluster(ctx context.Context, networkID string, locationID string, clusterName string, patch DeviceClusterPatch) (*Response, error)
}

// Proxies is the Proxies API family: what Client.Proxies sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Proxies interface {
	Create(ctx context.Context, networkID string, input ProxyServer) (*ProxyServer, *Response, error)

	List(ctx context.Context, networkID string) ([]ProxyServer, *Response, error)

	Update(ctx context.Context, networkID string, proxyID string, input ProxyServer) (*ProxyServer, *Response, error)
}

// Users is the Users API family: what Client.Users sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Users interface {
	// AddNetworkRole is POST /api/users/{id}/roles/network/{networkId}/{role}; it
	// keeps any role the user already holds on the network.
	AddNetworkRole(ctx context.Context, userID string, networkID string, role NetworkRole) (*Response, error)

	// BulkSetEnabled enables or disables several users at once. PATCH /api/users {userIds, enabled} (MANAGE_USER_ACCOUNTS). Forward refuses
	// the whole request if it includes your own account, a chatbot, or a Forward admin you may not change, and skips users already in the
	// requested state. Disabling is how access is cut off, so treat it as destructive.
	BulkSetEnabled(ctx context.Context, userIDs []string, enabled bool) (*Response, error)

	// ClearNetworkRoles is DELETE /api/users/{id}/roles/network/{networkId}: the
	// user keeps no role on the network.
	ClearNetworkRoles(ctx context.Context, userID string, networkID string) (*Response, error)

	// Create is POST /api/users: an org admin (MANAGE_USER_ACCOUNTS) creating a
	// LOCAL user in its own org. It returns the created user, including its ID.
	// Admin.CreateOrganizationUser is a different route (/api/orgs/{orgId}/users)
	// that Forward reserves for support admins and that returns no user.
	Create(ctx context.Context, input UserCreateRequest) (*User, *Response, error)

	// CreateToken mints an API token for the signed-in user. POST /api/users/current/tokens (UserController) requires the
	// user's password even on an authenticated session: confirmed live (v1.0.0-261005), where an impersonated cookie session
	// sending no password was answered 400 "Required request parameter 'password' ... is not present" and created nothing.
	// So Client.ImpersonatedClient cannot mint a token for a user whose password nobody holds; that needs the user's own
	// stored password or a mint on the platform side.
	CreateToken(ctx context.Context, tokenName string, password string) (*UserTokenRegistration, *Response, error)

	Current(ctx context.Context) (*User, *Response, error)

	// CurrentSession returns the caller's own session, roles included -- the way a token client learns what it may do. Current returns only
	// the user. GET /api/users/current (published getCurrentUser).
	CurrentSession(ctx context.Context) (*UserSession, *Response, error)

	// Delete deletes a user of the caller's org. A 404 is success (already gone). DELETE /api/users/{userId} (published deleteUser;
	// MANAGE_USER_ACCOUNTS). Forward refuses deleting yourself (400 "Can't delete yourself."), a chatbot account, and a Forward admin
	// unless you are one (403); those come back as the ErrorResponse with Forward's message. Admin.DeleteUser is the same route for a
	// service principal.
	Delete(ctx context.Context, userID string) (*Response, error)

	DeleteToken(ctx context.Context, tokenName string) (*Response, error)

	// DeleteTokenFor is DELETE /api/users/{id}/tokens/{tokenName}. A token that is
	// already gone counts as deleted.
	DeleteTokenFor(ctx context.Context, userID string, tokenName string) (*Response, error)

	// Get returns one user of the caller's org, or (nil, nil) when it does not exist. GET /api/users/{userId} (published getUser;
	// VIEW_USER_ACCOUNTS). Admin.LookupUser is the Forward-admin route.
	Get(ctx context.Context, userID string) (*User, *Response, error)

	// GrantOrgAdmin gives a user org ADMIN; already holding it is success. POST /api/users/{userId}/roles/org/ADMIN (MANAGE_USER_ACCOUNTS).
	// The undo of RevokeOrgAdmin. Admin.GrantOrganizationAdmin is the same route for a service principal.
	GrantOrgAdmin(ctx context.Context, userID string) (*Response, error)

	// List is GET /api/users: every user in the caller's org (VIEW_USER_ACCOUNTS).
	List(ctx context.Context) ([]User, *Response, error)

	ListTokens(ctx context.Context) ([]UserToken, *Response, error)

	// ListTokensFor is GET /api/users/{id}/tokens: another user's API tokens, as an
	// org admin sees them (names and access keys; secrets are never returned).
	ListTokensFor(ctx context.Context, userID string) ([]UserToken, *Response, error)

	// ListWithRoles returns the org's users with their roles, reachable networks, groups and API-token use. GET /api/users?view=roles
	// (VIEW_USER_ACCOUNTS; UserController, on primary 15398425a69 and stable 67e89c87124). Preview: not in the published spec.
	ListWithRoles(ctx context.Context) ([]UserWithRoles, *Response, error)

	// Patch changes a user of the caller's org and returns it. PATCH /api/users/{userId} (published updateUser; MANAGE_USER_ACCOUNTS).
	// Forward refuses changing your own password this way, disabling your own account, a chatbot account, and a Forward admin unless you
	// are one. Admin.PatchUser is the same route for a service principal.
	Patch(ctx context.Context, userID string, patch UserPatch) (*User, *Response, error)

	// RemoveNetworkRole is DELETE /api/users/{id}/roles/network/{networkId}/{role}.
	RemoveNetworkRole(ctx context.Context, userID string, networkID string, role NetworkRole) (*Response, error)

	// Reset2FA clears a user's two-factor setup, so the next sign-in enrolls again. DELETE /api/users/{userId}/2fa (MANAGE_USER_ACCOUNTS;
	// refused for a chatbot account).
	Reset2FA(ctx context.Context, userID string) (*Response, error)

	ResetPassword(ctx context.Context, input PasswordResetRequest) (*Response, error)

	// RevokeOrgAdmin takes org ADMIN away from a user. DELETE /api/users/{userId}/roles/org/ADMIN (MANAGE_USER_ACCOUNTS). Forward refuses
	// revoking your own org admin (a LOCAL user) unless you are a Forward admin, and a chatbot account. Undo with GrantOrgAdmin.
	RevokeOrgAdmin(ctx context.Context, userID string) (*Response, error)

	Roles(ctx context.Context, userID string) (*UserRoles, *Response, error)

	// RolesDirect returns only the roles assigned to the user directly, without access-control-group grants. Roles returns the merged
	// view. GET /api/users/{userId}/roles?type=directlyAssigned.
	RolesDirect(ctx context.Context, userID string) (*UserRoles, *Response, error)

	// SetNetworkRole is POST /api/users/{id}/roles/network/{networkId}?role=: the
	// user ends up holding exactly role on the network (Forward removes the pair's
	// existing roles first). Like every role grant it is not a no-op on the wire,
	// so a reconcile should read Roles first and only call this on a difference.
	SetNetworkRole(ctx context.Context, userID string, networkID string, role NetworkRole) (*Response, error)

	// TwoFactorStatuses returns each of the org's users' two-factor setup, by user ID. GET /api/users?view=2fa (VIEW_USER_ACCOUNTS).
	// Preview: not in the published spec.
	TwoFactorStatuses(ctx context.Context) (map[string]TwoFactorStatus, *Response, error)
}

// Admin is the Admin API family: what Client.Admin sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Admin interface {
	// AddSupportedOrganization grants or renews ONE supported org (POST /api/users/{id}/supported-orgs?orgId=&expiresIn=, a support-admin
	// route with the same rules as SetSupportedOrganizations). expiresDays is how long the grant stays valid after the user's last activity
	// in that org (impersonating refreshes it). Forward's own default is 30 and its maximum 365, and a value of 0 expires the grant by the end
	// of the current minute; this SDK, unlike Forward, sends 365 for any value of 0 or less, so it cannot send 0 (revoke with
	// SetSupportedOrganizations instead).
	AddSupportedOrganization(ctx context.Context, userID string, orgID string, expiresDays int) (*Response, error)

	CreateOrganizationUser(ctx context.Context, orgID string, input AdminUserCreateRequest) (*Response, error)

	DeleteUser(ctx context.Context, userID string) (*Response, error)

	// FDBSize reads the total stored size, in bytes, under an FDB key prefix (GET /api/system/fdb/size?prefix=). ADMINISTER_SYSTEM; Preview.
	FDBSize(ctx context.Context, prefix string) (int64, *Response, error)

	// FDBUsage reads the jobs-v2 FDB usage under a key prefix such as "n/<orgId>/<networkId>/" (GET /api/system/fdb/usage?prefix=).
	// ADMINISTER_SYSTEM (the system ADMIN role); the prefix is required. Preview: appserver WorkersController, not in the published spec.
	FDBUsage(ctx context.Context, prefix string) (*FDBUsage, *Response, error)

	GrantOrganizationAdmin(ctx context.Context, userID string) (*Response, error)

	ListNetworks(ctx context.Context) ([]AdminNetwork, *Response, error)

	ListUsers(ctx context.Context) ([]AdminUser, *Response, error)

	LookupUser(ctx context.Context, idOrUsername string) (*AdminUser, *Response, error)

	PatchUser(ctx context.Context, userID string, patch AdminUserPatch) (*Response, error)

	// PurgeFDBGarbage starts Forward's FDB garbage collection (POST /api/system/fdb/gc; ADMINISTER_SYSTEM): VACUUM ANALYZE, vacuumlo, and
	// dropping old FDB databases. It REMOVES data, though only what nothing references; it does not delete by prefix, and it does not
	// reclaim the data of a network deleted through an org cascade (see Organizations.Delete). Forward answers the text "Garbage collection
	// started" at once and works in the background. ADMINISTER_SYSTEM; Preview.
	PurgeFDBGarbage(ctx context.Context) (string, *Response, error)

	// SetSupportedOrganizations REPLACES a Forward support user's whole set of supported orgs (PUT /api/users/{id}/supported-orgs, a
	// support-admin route; the user must be in the Forward org, the org named "Forward Networks", and not a Forward admin, a chatbot or
	// supporting its own org). Orgs not listed are removed at once (this is also the only way to remove one: there is no per-org delete).
	// There is no expiry parameter: EVERY listed org, including ones already granted, is rewritten with last-active = now and 30 days, so
	// a grant made with AddSupportedOrganization for longer drops back to 30 days here. A grant stays valid for expires-in-days after the
	// user's last activity in that org (each impersonation refreshes it), not after the grant was made. Read the current set with
	// Users.Roles (SupportedOrgIDs) first.
	SetSupportedOrganizations(ctx context.Context, userID string, orgIDs []string) (*Response, error)
}

// NQERepository is the NQERepository API family: what Client.NQERepository sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type NQERepository interface {
	AddDirectory(ctx context.Context, path string) (*Response, error)

	// AddQuery stages a NEW query at path in the caller's workspace (a draft until Commit). It is only for a path that is not in HEAD:
	// Forward refuses an existing path with 409, reason ADD_QUERY_ALREADY_EXISTS ("Attempted to add a query at <path>, but that path
	// already exists in HEAD."). Change an existing query with EditQuery. Calling AddQuery again on its own draft replaces that draft.
	AddQuery(ctx context.Context, path string, source string) (*Response, error)

	Commit(ctx context.Context, input NQECommitRequest) (*Response, error)

	// CommitDryRun reports what committing the staged changes at paths would do,
	// without committing. With a snapshotID the queries are also typed against
	// that snapshot's data model; without one, only against the library. POST
	// /api/nqe/repos/org/commits?dryRun=true[&snapshotId=] (NqeLibController;
	// on primary 15398425a69 and stable 67e89c87124). Preview: not in the
	// published spec.
	CommitDryRun(ctx context.Context, paths []string, snapshotID string) (*NQECommitDryRun, *Response, error)

	DeleteDirectory(ctx context.Context, path string) (*Response, error)

	// DeleteQuery stages the deletion of a query in the caller's workspace. Like every other change here it is a draft: Commit on the same
	// path is what removes it from the org library, and DiscardChange drops the staged deletion instead. A 404 is tolerated so deleting
	// what is already gone succeeds.
	DeleteQuery(ctx context.Context, path string) (*Response, error)

	// DiscardChange drops the caller's draft at exactly path -- a staged add, edit or deletion of a query, or a directory's staged access
	// change -- leaving HEAD untouched. Having no draft there is success, so cleanup can always run. DELETE
	// /api/users/current/nqe/changes?path= (NqeLibController.discardChanges; WRITE_TO_NQE_LIBRARY). Preview.
	DiscardChange(ctx context.Context, path string) (*Response, error)

	// DiscardChanges drops every draft of the caller's at or under paths (query paths, or directories for everything inside them);
	// paths with no draft are ignored. POST /api/users/current/nqe/changes?action=bulkDiscard {paths}
	// (NqeLibController.discardChangesRecursiveBulk; WRITE_TO_NQE_LIBRARY). Preview.
	DiscardChanges(ctx context.Context, paths []string) (*Response, error)

	// EditQuery stages new source for a query that is already in HEAD (or renamed from one) in the caller's workspace, adding or
	// replacing the draft at path; Commit publishes it and DiscardChange drops it. POST /api/users/current/nqe/changes?action=editQuery
	// &path= {sourceCode, basis} (NqeLibController.editQueryDraft; WRITE_TO_NQE_LIBRARY). A path not in HEAD is refused with 409
	// (PATH_MISSING_IN_HEAD or UPDATE_EXISTING_FOR_NEW_QUERY): stage a new query with AddQuery. Preview: not in the published spec.
	EditQuery(ctx context.Context, path string, source string, basis NQEDraftBasis) (*Response, error)

	// ExportForwardSources is ExportOrgSources for the Forward Library, Forward's own shipped queries (POST
	// /api/nqe/repos/fwd/commits/head/queries?action=export; exportHeadPathsFwd). The queries the org's license tier does not permit are
	// left out by Forward.
	ExportForwardSources(ctx context.Context, paths []string) (map[string]string, *Response, error)

	// ExportOrgSources returns the source of every query in the organization library under paths, as a map from query path to source, in one
	// request (POST /api/users/current/nqe/changes?action=export; exportWorkspacePathsOrg, VIEW_NQE_LIBRARY). A path ending in "/" is a
	// directory (everything under it, so {"/"} is the whole library) and any other is one query. Two behaviours to know: it is the CALLING
	// USER'S view of the library, the committed head with that user's uncommitted drafts applied (getUserEffectiveQueries), not a fixed
	// commit; and it also includes the non-Forward queries the requested ones import. An empty paths list returns an empty map. A path
	// that matches nothing is simply absent.
	ExportOrgSources(ctx context.Context, paths []string) (map[string]string, *Response, error)

	// GetDraft returns the caller's draft of the query at path, or (nil, nil) when there is none ("No draft query at <path>"). GET
	// /api/users/current/nqe/changes?path= (getQueryDraftDetails; VIEW_NQE_LIBRARY). Preview.
	GetDraft(ctx context.Context, path string) (*NQEDraftQuery, *Response, error)

	GetQuery(ctx context.Context, commitID string, path string) (*NQECommittedQuery, *Response, error)

	// GetQueryByID reads a query's source by its STABLE query id ("Q_<sha>" for
	// the org repository, "FQ_<sha>" for the Forward Library) at an immutable
	// commit: GET /api/nqe/queries/{queryId}/source-code?commitId=.
	// NqeLibController.getQuerySourceCode, mapped at primary 15398425a69 and
	// stable 67e89c87124. The id survives a query being moved or renamed, which
	// a path does not; GetQuery is the by-path read. A committed query with empty
	// source is ErrNQEEmptySource, never an empty success.
	GetQueryByID(ctx context.Context, commitID string, queryID string) (*NQEQuerySourceAtCommit, *Response, error)

	Head(ctx context.Context) (Identifier, *Response, error)

	// History returns the commits that touched a query, by its stable ID ("Q_..."
	// or "FQ_..."). GET /api/nqe/queries/{queryId}/history (NqeLibController; on
	// primary 15398425a69 and stable 67e89c87124). Preview: not in the published
	// spec.
	History(ctx context.Context, queryID string) ([]NQEQueryCommit, *Response, error)

	// ListDrafts returns the caller's uncommitted NQE changes. GET /api/users/current/nqe/changes (getUserWorkspaceChanges;
	// VIEW_NQE_LIBRARY). Preview.
	ListDrafts(ctx context.Context) ([]NQEDraftChange, *Response, error)

	ListHeadQueries(ctx context.Context) ([]NQECommittedQueryInfo, *Response, error)
}

// Banners is the Banners API family: what Client.Banners sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Banners interface {
	// Announcement reads the current system announcement, or nil when none is
	// posted. GET /api/system-announcement (SaasAnnouncementController, Preview;
	// any signed-in user). The route exists on SaaS only (@ProfileCriteria(not =
	// ON_PREM)); an on-premises deployment answers with its typed
	// ErrEndpointNotServed 404, which is "no announcements possible", not a
	// failure. Posting and deleting are operator-only and not modelled.
	Announcement(ctx context.Context) (*SystemAnnouncement, *Response, error)

	Create(ctx context.Context, input CustomBannerRequest) (*CustomBanner, *Response, error)

	// CriticalStates reads the org's critical states. GET /api/critical-states
	// (NotificationController.getCriticalStates, Preview; any signed-in user).
	CriticalStates(ctx context.Context) (*CriticalStates, *Response, error)

	// Delete removes a custom banner; one that is already gone counts as success. A 404 that means the route is not
	// served (ErrEndpointNotServed) is still an error.
	// DELETE /api/custom-banners/{bannerId} (CustomBannerController.deleteBanner).
	Delete(ctx context.Context, bannerID string) (*Response, error)

	List(ctx context.Context) ([]CustomBanner, *Response, error)

	Replace(ctx context.Context, banner CustomBanner) (*Response, error)
}

// Jobs is the Jobs API family: what Client.Jobs sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Jobs interface {
	// Cancel cancels the job (or job group) identified by cancelLink, exactly as returned in an ActiveJobInfo's
	// CancelLink field. It stops the tracked execution; whether the underlying worker thread is interrupted
	// immediately or only once it reaches a checkpoint is up to that job's own implementation.
	//
	// cancelLink is already percent-encoded by Forward for direct insertion into the path (Forward's own GUI
	// builds the URL as a bare `/jobs/${cancelLink}` template, with no further encoding) -- it is NOT raw base64,
	// even though it is base64 underneath. Escaping it again here (as a normal path segment would need) corrupts
	// it: a real cancelLink contains literal "%2F"/"%3D" text, and re-escaping that "%" turns it into "%25..." and
	// Forward answers 400 Bad Request, not 404. So this is deliberately NOT url.PathEscape(cancelLink).
	//
	// DELETE /api/jobs/{cancelLink} (JobsController.cancelJob; ADMINISTER_SYSTEM). A single job that already finished or was already
	// canceled answers 404 (CANCELATION_STATUS_NOT_FOUND), returned as an error; a grouped or network-level link always answers 200.
	// Forward has no snapshot-level cancel; Cancel stops only the job or job group its cancelLink names.
	Cancel(ctx context.Context, cancelLink string) (*Response, error)

	// DebugView reads the job scheduler's debug view (GET /api/debug/jobs, JobsController.getDebugJobsView; needs ADMINISTER_SYSTEM): every
	// job and task the scheduler holds right now across ALL organizations, with each task's status, worker and worker class. It is the only
	// route that shows running and queued Predict and compute tasks per org with their worker class; the per-org worker cap is not in it.
	// It is a point-in-time debug dump with no stability promise (not a stream of state transitions). For job rows grouped for display
	// use ListActive.
	DebugView(ctx context.Context) (DebugJobsView, *Response, error)

	// ListActive returns currently running or queued top-level jobs, grouped by type/network/snapshot.
	ListActive(ctx context.Context) ([]ActiveJobInfo, *Response, error)

	// ListCompleted returns finished top-level jobs, grouped by type/network/snapshot/state.
	ListCompleted(ctx context.Context) ([]CompletedJobInfo, *Response, error)
}

// NetworkMaps is the NetworkMaps API family: what Client.NetworkMaps sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type NetworkMaps interface {
	// Directories returns the network's map directories (GET .../map-directories; VIEW_NETWORK_MAPS and the organization property
	// NETWORK_MAP_DIRECTORIES, default on; off is ErrFeatureGated). The root directory "/" is always first, so a network without maps still has one directory.
	Directories(ctx context.Context, networkID string) (*NetworkMapDirectories, *Response, error)

	// Get returns one stored map (GET .../maps/{mapId}; VIEW_NETWORK_MAPS). An unknown id is a 404.
	Get(ctx context.Context, networkID string, mapID string) (*NetworkMap, *Response, error)

	// GetWithDirectory is Get with the directory path filled in (GET .../maps/{mapId}?with=directory). A map in no directory reads "/".
	GetWithDirectory(ctx context.Context, networkID string, mapID string) (*NetworkMap, *Response, error)

	// List returns the network's maps, summary rows only (GET .../maps; VIEW_NETWORK_MAPS). A network without maps gives an empty slice.
	List(ctx context.Context, networkID string) ([]NetworkMapInfo, *Response, error)

	// Snapshot returns the map as it is in one snapshot, with the map's devices that snapshot has (GET .../maps/{mapId}?view=snapshot;
	// VIEW_NETWORK_MAPS; the snapshot must have passed the CREATION stage). An empty snapshot ID leaves the choice to the appserver, which
	// uses the network's latest snapshot that has passed CREATION (not a draft or fork; it may still be processing later stages), or
	// answers 409 NO_QUALIFYING_SNAPSHOT when there is none. The Network-Snapshot response header names the one it picked.
	Snapshot(ctx context.Context, networkID string, mapID string, snapshotID string) (*NetworkMapSnapshot, *Response, error)

	// Structures returns the structure hierarchy of the map's devices in a snapshot (GET .../maps/{mapId}/structures; VIEW_NETWORK_MAPS;
	// CREATION stage). Map devices the snapshot lacks are left out. An empty snapshot ID leaves the choice to the appserver, which picks as
	// Snapshot describes.
	Structures(ctx context.Context, networkID string, mapID string, snapshotID string) ([]NetworkMapStructure, *Response, error)
}

// Routing is the Routing API family: what Client.Routing sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Routing interface {
	// DevicePrefixInfo returns what one device has for a prefix: the advertisements it received, with their outcome and origin, and the
	// more specific routes it installed (GET .../devices/{device}?view=bgp-prefix-info&prefix=). A device that is not in the snapshot makes
	// Forward answer 500 (an unhandled lookup), and one it has no model for answers 200 with an error, returned as a RoutingLookupError;
	// with a retry policy set, the 500 is retried like any GET.
	DevicePrefixInfo(ctx context.Context, networkID string, snapshotID string, device string, prefix string) (*RoutingDevicePrefixInfo, *Response, error)

	// InstalledPrefixes lists the prefixes BGP nodes installed after modelling (GET .../bgp-installed-prefixes), for one device or, with no
	// device, across the snapshot (vendor-balanced and capped at Limit).
	InstalledPrefixes(ctx context.Context, networkID string, snapshotID string, options RoutingInstalledOptions) (*RoutingInstalledPrefixes, *Response, error)

	// PrefixDetails returns the route attributes and policies at one hop of a trace (POST .../bgp-prefix-details).
	PrefixDetails(ctx context.Context, networkID string, snapshotID string, request RoutingPrefixDetailsRequest) (*RoutingPrefixDetails, *Response, error)

	// PrefixSearch answers "where does this prefix come from and who has it?" on a snapshot (POST .../bgp-prefix-search). A prefix Forward
	// has no routing data for gives an empty result, not an error.
	PrefixSearch(ctx context.Context, networkID string, snapshotID string, query RoutingPrefixSearch) (*RoutingPrefixSearchResult, *Response, error)

	// PrefixTrace returns the propagation of a prefix from its origin to a router (POST .../bgp-prefix-trace): a list of paths, each a list
	// of hops from origin to the router. Take a router, origin and outcome from PrefixSearch. A prefix with no routing data gives no paths.
	PrefixTrace(ctx context.Context, networkID string, snapshotID string, request RoutingTraceRequest) ([][]RoutingNode, *Response, error)

	// RPDevicePrefixInfo returns what one device has for a prefix, per VRF and protocol (GET .../devices/{device}?view=rp-prefix-info&prefix=).
	// A device with no model for it answers a RoutingLookupError.
	RPDevicePrefixInfo(ctx context.Context, networkID string, snapshotID string, device string, prefix string) (*RPDevicePrefixInfo, *Response, error)

	// RPPrefixDetails returns the route attributes and policies at one hop of a trace (POST .../rp-prefix-details). A node that does not
	// resolve is a RoutingLookupError.
	RPPrefixDetails(ctx context.Context, networkID string, snapshotID string, request RPPrefixDetailsRequest) (*RPPrefixDetails, *Response, error)

	// RPPrefixSearch answers "where does this prefix come from and who has it?" for any modelled protocol (POST .../rp-prefix-search). The
	// answer has the shape of PrefixSearch's.
	RPPrefixSearch(ctx context.Context, networkID string, snapshotID string, query RPPrefixSearch) (*RoutingPrefixSearchResult, *Response, error)

	// RPPrefixTrace returns the propagation of a prefix from its origin to a router (POST .../rp-prefix-trace): a list of paths, each a list
	// of hops. The hops carry the node key RoutingNode.Ref needs.
	RPPrefixTrace(ctx context.Context, networkID string, snapshotID string, request RPPrefixTrace) ([][]RoutingNode, *Response, error)
}

// ThreatExposure is the ThreatExposure API family: what Client.ThreatExposure sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type ThreatExposure interface {
	// CreateDefinition saves a definition and returns it with its id (POST .../threat-exposure-definitions; 201; EDIT_THREAT_EXPOSURE and
	// the organization property THREAT_EXPOSURE). Forward refuses an invalid definition with 400 and the reason.
	CreateDefinition(ctx context.Context, networkID string, definition NewThreatExposureDefinition) (*ThreatExposureDefinition, *Response, error)

	// CriticalAssetsSummary returns the critical assets the analysis found exposed, one page at a time, as Forward sent it (POST
	// .../threat-exposure-analyses/{id}?view=criticalAssetsExposureSummary&snapshotId=; VIEW_THREAT_EXPOSURE). Rows come in a fixed order.
	CriticalAssetsSummary(ctx context.Context, networkID string, definitionID string, snapshotID string, paging ThreatExposurePaging) (json.RawMessage, *Response, error)

	// DeleteDefinition removes a definition (DELETE .../threat-exposure-definitions/{id}; 204; 404 when it does not exist; EDIT_THREAT_EXPOSURE;
	// not gated on the organization property). It cannot be undone.
	DeleteDefinition(ctx context.Context, networkID string, definitionID string) (*Response, error)

	// DirectExposureDetails returns how one critical asset is directly exposed, as Forward sent it (GET
	// .../threat-exposure-analyses/{id}?view=criticalAssetDirectExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE). assetId is the global host id
	// the summary gives the asset.
	DirectExposureDetails(ctx context.Context, networkID string, definitionID string, snapshotID string, assetID int64) (json.RawMessage, *Response, error)

	// Evaluate starts the analysis of a definition on a snapshot (POST .../threat-exposure-analyses/{id}?action=evaluate&snapshotId=; 202,
	// no body; EVALUATE_THREAT_EXPOSURE and THREAT_EXPOSURE; the snapshot must be processed to the reachability stage). It starts jobs and
	// returns at once (409 when the definition is already being analyzed against that snapshot): poll Status until it reports completion, then read the views. The snapshot is required.
	Evaluate(ctx context.Context, networkID string, definitionID string, snapshotID string) (*Response, error)

	// GetDefinition returns one definition (GET .../threat-exposure-definitions/{id}; 404 when it does not exist).
	GetDefinition(ctx context.Context, networkID string, definitionID string) (*ThreatExposureDefinition, *Response, error)

	// ListDefinitions returns the network's definitions (GET .../threat-exposure-definitions; VIEW_THREAT_EXPOSURE).
	ListDefinitions(ctx context.Context, networkID string) ([]ThreatExposureDefinitionInfo, *Response, error)

	// ResolveDefinition previews what a definition (saved or not) selects on a snapshot: the exposure points and critical assets it
	// resolves to, as Forward sent it (POST .../threat-exposure-definitions?action=resolve&view=summary&snapshotId=; VIEW_THREAT_EXPOSURE and
	// THREAT_EXPOSURE; the snapshot must be processed to the reachability stage). The body is not modelled.
	ResolveDefinition(ctx context.Context, networkID string, snapshotID string, definition NewThreatExposureDefinition) (json.RawMessage, *Response, error)

	// Status returns the progress and state of the analysis of a definition on a snapshot, as Forward sent it (GET
	// .../threat-exposure-analyses/{id}?view=status&snapshotId=; VIEW_THREAT_EXPOSURE). The snapshot is required: the latest may not have been
	// analyzed.
	Status(ctx context.Context, networkID string, definitionID string, snapshotID string) (json.RawMessage, *Response, error)

	// TransitiveExposureDetails returns how one critical asset is exposed through other hosts, one page at a time, as Forward sent it (POST
	// .../threat-exposure-analyses/{id}?view=criticalAssetTransitiveExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE).
	TransitiveExposureDetails(ctx context.Context, networkID string, definitionID string, snapshotID string, assetID int64, paging ThreatExposurePaging) (json.RawMessage, *Response, error)

	// UpdateDefinition changes a definition (PATCH .../threat-exposure-definitions/{id}; 204; EDIT_THREAT_EXPOSURE and THREAT_EXPOSURE).
	UpdateDefinition(ctx context.Context, networkID string, definitionID string, patch ThreatExposureDefinitionPatch) (*Response, error)
}

// EdgeAssets is the EdgeAssets API family: what Client.EdgeAssets sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type EdgeAssets interface {
	// Delete removes an edge asset AND its criticality (DELETE .../edge-assets/{name}; 204; MANAGE_EDGE_ASSETS; not gated on the
	// organization property, so it works after the feature is turned off). It cannot be undone.
	Delete(ctx context.Context, networkID string, assetName string) (*Response, error)

	// List returns the network's edge assets (GET .../edge-assets; VIEW_COLLECTION_SOURCES). It is not gated on the organization property
	// EDGE_ASSETS_IN_SOURCES, so assets added while it was on stay visible after it is turned off.
	List(ctx context.Context, networkID string) ([]EdgeAsset, *Response, error)

	// RemoveCriticality removes the criticality of the named assets (the same route with a null criticality; EDIT_ASSET_CRITICALITIES and
	// EDGE_ASSETS_IN_SOURCES), which takes them out of the critical assets threat-exposure analyses select.
	RemoveCriticality(ctx context.Context, networkID string, names []string) (*Response, error)

	// SetCriticality assigns one criticality to the named assets, edge assets and endpoints alike (PATCH .../assets?action=modifyCriticality;
	// 204; EDIT_ASSET_CRITICALITIES and EDGE_ASSETS_IN_SOURCES). level is BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL.
	SetCriticality(ctx context.Context, networkID string, names []string, level string) (*Response, error)

	// Update changes an edge asset (PATCH .../edge-assets/{name}; 204; MANAGE_EDGE_ASSETS and the organization property
	// EDGE_ASSETS_IN_SOURCES).
	Update(ctx context.Context, networkID string, assetName string, patch EdgeAssetPatch) (*Response, error)
}

// Licensing is the Licensing API family: what Client.Licensing sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Licensing interface {
	// ActivateForOrg activates a stored license on ONE named org (POST /api/orgs/{orgId}/licenses/{licenseId}?action=activate).
	// PLATFORM-scoped (@RequiresForwardSupport), and SaaS deployments only (on-prem answers ErrEndpointNotServed, see ListForOrg).
	// It changes what the org is entitled to and CANNOT BE UNDONE one license at a time: Forward refuses (400) to invalidate an
	// activated license, and a second activation is also a 400; only RemoveAllForOrg clears it, for the whole org. Name the org and
	// the license deliberately.
	//
	// Forward has two forms. Spring picks the handler from the Content-Type the caller sends, and the handler then refuses a form
	// that does not match the org's SOFTWARE_CENTRAL property (400). For an org with it off (a SaaS license) pass an empty
	// fingerprint: no body and no Content-Type are sent. For an org with it on (an on-prem license) pass the fingerprint of the
	// CUSTOMER'S on-prem deployment (Licensing.Fingerprint on a client pointed at that deployment, not this one); it goes as
	// {"fingerprint": ...}. A fingerprint from the wrong deployment is accepted and the license is bound to it for good.
	// Preview: the route is in the appserver controller, not the published spec. The answer is the stored license: Status is empty
	// (no status is computed here) and Signed is true.
	ActivateForOrg(ctx context.Context, orgID string, licenseID string, fingerprint string) (*LicenseDetails, *Response, error)

	// AllOrgUsage reads the counted usage of every enabled SaaS org the login supports (GET /api/admin/platform-usage; Forward-support login;
	// a Forward admin sees all). SaaS only: an on-prem appserver answers ErrEndpointNotServed (ForwardSupportLicenseController is not mapped
	// there). It is the one read that gives edge-asset use for another org. Pair it with SaaSOrgInfos for the caps.
	AllOrgUsage(ctx context.Context) ([]OrgUsageSummary, *Response, error)

	// Apply activates a key on the CALLER'S OWN org. There is no org parameter and
	// that is deliberate: the server reads the org from the session
	// (@Requires(org = MANAGE_LICENSING)), so this method is structurally incapable
	// of licensing somebody else's org.
	Apply(ctx context.Context, key string) (*LicenseDetails, *Response, error)

	// CCHDebug returns everything Forward holds to reproduce the caller's CCH history calculation (GET /api/platform-usage?view=cch-debug):
	// updatedAt, licenses, periods and counts, left raw because Forward builds it for support to read.
	CCHDebug(ctx context.Context) (map[string]json.RawMessage, *Response, error)

	// CCHHistory reads the CALLER'S OWN org's CCH per license (GET /api/platform-usage?view=cch-history; VIEW_LICENSING; not Software Central).
	// A zero start or end is left out: end defaults to now, start to the beginning. It cannot be pointed at another org by id; see OrgCloudUsage.
	CCHHistory(ctx context.Context, start time.Time, end time.Time) (*CCHHistory, *Response, error)

	// CCHUsage reads the caller's own org's CCH used and licensed (GET /api/platform-usage?view=cch; any signed-in user).
	CCHUsage(ctx context.Context) (*CCHUsage, *Response, error)

	// CCHUsedSince reads the CCH the caller's own org used in the last days days (GET /api/platform-usage?view=cch&days=).
	CCHUsedSince(ctx context.Context, days int) (int64, *Response, error)

	// Decode previews a key WITHOUT applying it, against the caller's own org.
	// This is the honest dry-run: it returns the same status the apply path would
	// judge the key by, so a caller can show why a key would be refused without
	// changing any state.
	Decode(ctx context.Context, key string) (*DecodedLicenseKey, *Response, error)

	// DeviceCounts reads the caller's own org's licensed and unlicensed device counts and caps (GET /api/licensed-devices?view=count;
	// VIEW_LICENSE_CAPACITY; not Software Central).
	DeviceCounts(ctx context.Context) (*DeviceCounts, *Response, error)

	// EdgeAssetCounts reads the caller's own org's licensed and unlicensed edge-asset counts and cap (GET /api/licensed-edge-assets?view=count;
	// VIEW_LICENSE_CAPACITY; not Software Central). For another org's count use AllOrgUsage (SaaS) or impersonate.
	EdgeAssetCounts(ctx context.Context) (*EdgeAssetCounts, *Response, error)

	// Fingerprint returns the running instance's fingerprint. Any authenticated
	// user may read it (@RequiresUser); it is the value a license must be bound to.
	Fingerprint(ctx context.Context) (*InstanceFingerprint, *Response, error)

	// InvalidateForOrg invalidates ONE license on a named org, leaving the rest.
	// PLATFORM-scoped (@RequiresForwardSupport), and SaaS deployments only (on-prem answers
	// ErrEndpointNotServed, see ListForOrg). Prefer this over RemoveAllForOrg
	// whenever the intent is to undo a single license: "remove all" is a blunt
	// instrument and its blast radius is the whole org's entitlement.
	InvalidateForOrg(ctx context.Context, orgID string, licenseID string) (*Response, error)

	// LicensedDevices lists the caller's own org's licensed and unlicensed devices (GET /api/licensed-devices; VIEW_LICENSING).
	LicensedDevices(ctx context.Context) (*LicensedDevices, *Response, error)

	// List returns the licenses of the signed-in user's own organization, each
	// with its status now (LicenseController.getLicenses; any signed-in user).
	// Unlike ListForOrg it is served on every deployment, on-prem included. GET
	// /api/licenses. With the SOFTWARE_CENTRAL org property on, invalidated
	// licenses are left out.
	List(ctx context.Context) ([]LicenseDetails, *Response, error)

	// ListForOrg reads an org's licenses. PLATFORM-scoped (@RequiresForwardSupport).
	//
	// SaaS deployments only. ForwardSupportLicenseController, which serves this and the
	// other /api/orgs/{id}/licenses methods below, is @ProfileCriteria(not = ON_PREM): on an
	// on-prem appserver the route is not mapped and Forward answers 404 "No endpoint", as
	// it does for any unmapped path; errors.Is(err, ErrEndpointNotServed) tells that apart
	// from a missing org. The path is right and is served on both pinned builds; use List
	// for your own org, which works everywhere.
	ListForOrg(ctx context.Context, orgID string) ([]LicenseDetails, *Response, error)

	// OrgCloudUsage reads one org's cloud accounting, including the CCH used under each license (GET /api/orgs/{orgId}/platform-usage?view=vpc).
	// Needs a Forward-support login; not for a Software Central org. This is the way to read another org's per-license CCH without
	// impersonating; CCHHistory only reads the credential's own org. Period.EndReason is LICENSE_STARTED, LICENSE_EXPIRED, LICENSE_REPLACED,
	// LICENSE_CONSUMED or GRACE_PERIOD_ELAPSED.
	OrgCloudUsage(ctx context.Context, orgID string) (*CloudUsage, *Response, error)

	// OrgUsage reads one org's device usage and CCH total (GET /api/orgs/{orgId}/platform-usage). Needs a Forward-support login and an org
	// that is not Software Central (else 400). SaaS only: an on-prem appserver answers ErrEndpointNotServed. Count Devices by Licensed and Lite
	// for standard and lite use.
	OrgUsage(ctx context.Context, orgID string) (*PlatformUsage, *Response, error)

	// RemoveAllForOrg deletes every license from ONE NAMED org. PLATFORM-scoped
	// (@RequiresForwardAdmin). SaaS deployments only: an on-prem appserver does not map the
	// route (ErrEndpointNotServed), see ListForOrg.
	//
	// READ THE PATH. This is DELETE /api/orgs/{id}/licenses -- the org's LICENSE
	// COLLECTION. It is not, and cannot become, a call that deletes the org: there
	// is no code path from here to org deletion, the trailing segment is a literal,
	// and orgID is escaped into one path element so a crafted id cannot climb out
	// of it. That is the property to preserve if this method is ever edited.
	RemoveAllForOrg(ctx context.Context, orgID string) (*Response, error)

	// SaaSOrgInfos reads the tier, status and summed caps of every enabled SaaS org the login supports (GET /api/admin/licenses?view=saas;
	// Forward-support login; SaaS only, ErrEndpointNotServed on-prem). Status is Forward's OrgLicenseStatus: ACTIVE, EXEMPT, NOT_ACTIVE_YET, EXPIRED_RECENTLY or EXPIRED.
	SaaSOrgInfos(ctx context.Context) ([]SaaSOrgInfo, *Response, error)

	// TierAndStatus returns the signed-in user's organization's license tier and
	// status. GET /api/licenses?view=status (LicenseController; any signed-in
	// user; served on every deployment).
	TierAndStatus(ctx context.Context) (*LicenseTierAndStatus, *Response, error)

	// Usage reads the CALLER'S OWN org's device usage and CCH total (GET /api/platform-usage; VIEW_LICENSING; not Software Central).
	Usage(ctx context.Context) (*PlatformUsage, *Response, error)

	// UsageHistory reads the caller's own org's device and edge-asset use against caps over a period (GET /api/metrics/license-usage?start=&end=;
	// VIEW_LICENSE_CAPACITY). Both bounds are required.
	UsageHistory(ctx context.Context, start time.Time, end time.Time) ([]UsagePoint, *Response, error)
}

// SoftwareCentral is the SoftwareCentral API family: what Client.SoftwareCentral sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type SoftwareCentral interface {
	// DeploymentArtifactURL resolves the download URL for one artifact version
	// using the `as=url` form (see the service comment for why the 302 form is
	// unusable here). An empty versionID asks the server for its latest version.
	//
	// The URL is presigned and short-lived: resolve it right before the download,
	// not at planning time.
	DeploymentArtifactURL(ctx context.Context, artifactID Identifier, versionID Identifier) (string, *Response, error)

	// DownloadClientPackage is GET /api/software/client?type=: it streams the
	// client package of packageType for the authenticated user into w and
	// reports its name, size and digest. The client's request timeout does not
	// apply -- a headless collector is ~225 MB -- so bound the download with ctx.
	// Nothing is written to w unless Forward answers 2xx. It needs a user login: a collector's own credentials are
	// answered 403 {"message":"Non-collector user required"} (seen live on fwd.app, where a user login got the install4j
	// installer).
	DownloadClientPackage(ctx context.Context, packageType ClientPackageType, w io.Writer) (*ClientPackage, *Response, error)

	// DownloadDeploymentArtifact streams one artifact version to w and returns the
	// bytes written. It resolves a presigned URL (DeploymentArtifactURL) and fetches
	// it without the Forward credential, which would otherwise reach a third party;
	// the client's request timeout does not apply, so cancellation is ctx.
	DownloadDeploymentArtifact(ctx context.Context, artifactID, versionID Identifier, w io.Writer) (int64, error)

	// ListDeploymentArtifacts reads the catalog. deploymentType narrows it to
	// artifacts deployable on that platform; the empty value lists everything.
	ListDeploymentArtifacts(ctx context.Context, deploymentType DeploymentType) ([]DeploymentArtifact, *Response, error)
}

// CVEIndex is the CVEIndex API family: what Client.CVEIndex sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CVEIndex interface {
	// Delete reverts an on-prem appserver to the index bundled in its image.
	Delete(ctx context.Context) (*Response, error)

	// Download fetches the current gzipped CVE index from fwd.app with the
	// Software Central credential the client was built with. The returned bytes
	// are validated as gzip and digested; nothing is decompressed beyond the
	// header check.
	Download(ctx context.Context) (*CVEIndexDownload, *Response, error)

	// Metadata reads what an on-prem appserver is serving.
	Metadata(ctx context.Context) (*CVEIndexMetadata, *Response, error)

	// Put installs a gzipped index on an on-prem appserver. The sha256 is sent as
	// `?sha=` so the server validates the body it received against the digest the
	// caller computed; a mismatch is a 4xx, never a silently corrupt index. A 200
	// says the request was ACCEPTED -- read Metadata back to prove the instance
	// now serves that digest, which is what an installer's verdict should rest on.
	Put(ctx context.Context, index *CVEIndexDownload) (*Response, error)

	// UpdateFromSoftwareCentral asks the on-premises deployment to update its CVE index itself: it downloads from fwd.app with the org's active
	// license id and deployment fingerprint and installs the index if it is newer than the one installed (POST /api/cve-index?action=update;
	// MANAGE_CVE_INDEX; CveIndexUpdateService.updateCveIndex for the caller's org). It contacts an external service and can install an index, so
	// it is a write-class call; it is not destructive (an older installed index is replaced only by a newer one). ON_PREM profile only. Check
	// the result's Error: a failed update is not an HTTP error. Put and Download are the manual path for a deployment that cannot reach fwd.app.
	UpdateFromSoftwareCentral(ctx context.Context) (*CVEIndexUpdate, *Response, error)
}

// FwdAppConnection is the FwdAppConnection API family: what Client.FwdAppConnection sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type FwdAppConnection interface {
	// Proxy reads the organization's fwd.app proxy settings (GET /api/fwd-app-proxy; VIEW_FWD_CONNECTION_CONFIG). Read-only. A deployment that
	// never configured one answers Forward's default (Enabled false).
	Proxy(ctx context.Context) (*FwdAppProxy, *Response, error)

	// Test checks that this deployment can reach Software Central through the organization's configured proxy, with license-based authentication
	// (POST /api/fwd-app-connection?action=test; MANAGE_FWD_CONNECTION_CONFIG). Forward answers by making an outbound HEAD /api/cve-index with
	// the org's license id and deployment id to the org's SOFTWARE_CENTRAL_URL: it contacts an external service, so it is a write-class call (an
	// outbound request), though it changes no stored setting. Status is SUCCESS, AUTH_FAILED (the license or deployment was refused) or
	// CONNECTION_FAILED (the host or proxy could not be reached).
	Test(ctx context.Context) (*FwdAppConnectionTest, *Response, error)
}

// ChangeReports is the ChangeReports API family: what Client.ChangeReports sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type ChangeReports interface {
}

// AccessControl is the AccessControl API family: what Client.AccessControl sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type AccessControl interface {
	// AddLabels adds device access labels to access control groups. PATCH /api/access-control-groups?action=addLabels {groupIds, labelIds}
	// (MANAGE_USER_ACCOUNTS). Preview.
	AddLabels(ctx context.Context, groupIDs []string, labelIDs []string) (*Response, error)

	CreateDeviceAccessLabel(ctx context.Context, input DeviceAccessLabelRequest) (*DeviceAccessLabel, *Response, error)

	CreateGroup(ctx context.Context, input AccessControlGroupRequest) (*AccessControlGroup, *Response, error)

	// DeleteDeviceAccessLabel is idempotent: a 404 is success, because the desired
	// state -- the label does not exist -- already holds.
	DeleteDeviceAccessLabel(ctx context.Context, labelID string) (*Response, error)

	// DeleteGroup is idempotent: a 404 is success.
	DeleteGroup(ctx context.Context, groupID string) (*Response, error)

	// DeviceAccessLabelNames maps each device access label's ID to its name. GET /api/device-access-labels?view=names (VIEW_USER_ACCOUNTS).
	// Preview.
	DeviceAccessLabelNames(ctx context.Context) (map[string]string, *Response, error)

	// GroupNames maps each access control group's ID to its name. Unlike ListGroups it needs no permission beyond being signed in. GET
	// /api/access-control-groups?view=names (AccessController; on primary 15398425a69 and stable 67e89c87124). Preview.
	GroupNames(ctx context.Context) (map[string]string, *Response, error)

	ListDeviceAccessLabels(ctx context.Context) ([]DeviceAccessLabel, *Response, error)

	ListGroups(ctx context.Context) ([]AccessControlGroup, *Response, error)

	// RemoveLabels removes device access labels from access control groups, narrowing which devices their members can see. PATCH
	// /api/access-control-groups?action=removeLabels {groupIds, labelIds} (MANAGE_USER_ACCOUNTS). Preview.
	RemoveLabels(ctx context.Context, groupIDs []string, labelIDs []string) (*Response, error)

	// SetGroupNetworkRole gives an access control group a role on a WORKSPACE network and returns the group. POST
	// /api/access-control-groups/{id}/network-roles/{networkId}?role= (ASSIGN_ROLES on that network). Forward refuses a network that is not
	// a workspace, and a role above the caller's own on it. Preview.
	SetGroupNetworkRole(ctx context.Context, groupID string, networkID string, role NetworkRole) (*AccessControlGroup, *Response, error)

	UpdateDeviceAccessLabel(ctx context.Context, labelID string, input DeviceAccessLabelRequest) (*DeviceAccessLabel, *Response, error)

	// UpdateGroup is a POST to the group's URL, not a PATCH -- that is the route
	// Forward serves. The nil-networkRoles org-admin signal means exactly what it
	// means on create.
	UpdateGroup(ctx context.Context, groupID string, input AccessControlGroupRequest) (*AccessControlGroup, *Response, error)
}

// SAML is the SAML API family: what Client.SAML sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type SAML interface {
	// GetSettings returns nil, nil when the appliance has no SAML configured --
	// Forward answers an empty body or a JSON null, and both mean "none", not an
	// error.
	GetSettings(ctx context.Context) (*SAMLSettings, *Response, error)

	PutSettings(ctx context.Context, input SAMLSettings) (*Response, error)
}

// Configuration is the Configuration API family: what Client.Configuration sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Configuration interface {
	GetDeployment(ctx context.Context, property string) (*DeploymentConfigValue, *Response, error)

	SetDeployment(ctx context.Context, property string, value string) (*DeploymentConfigValue, *Response, error)
}

// Integrations is the Integrations API family: what Client.Integrations sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Integrations interface {
	// AddCheckIncident raises a ServiceNow incident for one failing check and links it (POST /snapshots/{id}/checks/{checkId}
	// ?action=addServiceNowIncident). Forward refuses a check that is disabled or not failing in this snapshot and every later one. It creates
	// a record in the customer's ServiceNow.
	AddCheckIncident(ctx context.Context, snapshotID string, checkID string, input ServiceNowIncidentInput) (*ServiceNowIncidentLink, *Response, error)

	// AddCheckIncidents raises incidents for several checks in one call, keyed by check ID (POST /snapshots/{id}/checks
	// ?action=addServiceNowIncidents). The same conditions as AddCheckIncident apply to every check.
	AddCheckIncidents(ctx context.Context, snapshotID string, inputs map[string]ServiceNowIncidentInput) ([]ServiceNowIncidentLink, *Response, error)

	// AttachToChangeSetChangeRequest uploads a file to a change request (POST .../change-requests/{sysId}/attachments, multipart part "file";
	// 204). Forward refuses more than ServiceNowMaxAttachmentBytes (this SDK refuses it first) and a file type outside png, jpg, jpeg, pdf,
	// xlsx, docx and csv (400). It shortens a file name past ServiceNowMaxAttachmentFileName characters rather than refusing it.
	AttachToChangeSetChangeRequest(ctx context.Context, networkID string, changeSetID string, sysID string, fileName string, content []byte) (*Response, error)

	// CheckIncidentUpdateHistory lists the work-note updates Forward made to one incident of a check (GET /snapshots/{id}/checks/{checkId}
	// ?view=serviceNowIncidentUpdateHistory&incident=NUMBER). An incident that is not linked to the check is a 400.
	CheckIncidentUpdateHistory(ctx context.Context, snapshotID string, checkID string, incidentNumber string) ([]ServiceNowIncidentUpdate, *Response, error)

	// CloseServiceNowIncident closes one incident in ServiceNow (POST /integrations/servicenow/incidents/{number}?action=close). It changes the
	// customer's ServiceNow record. It does not detach the incident from its check: Forward answers 400 "Cannot close incident X. Incident is
	// still active." while the incident still applies to a check (no end instant yet), and UnlinkCheckIncident is what detaches it. An incident
	// already Closed in ServiceNow is not touched there; Forward still marks its own copy closed.
	CloseServiceNowIncident(ctx context.Context, number string) (*Response, error)

	// CloseServiceNowIncidents closes several incidents in ServiceNow (POST /integrations/servicenow/incidents?action=close, 204). It changes
	// the customer's ServiceNow records. Forward checks every incident for the "still active" refusal (see CloseServiceNowIncident) before it
	// closes any, so one active incident means none are closed.
	CloseServiceNowIncidents(ctx context.Context, numbers []string) (*Response, error)

	// CollectVulnerabilityScanner starts a collection of a scanner now, outside its schedule (POST
	// /api/vulnerability-scanners/{name}?action=collect; MANAGE_VULNERABILITY_SCANNERS). Forward answers when it has been queued, with no
	// body: the outcome shows up later in the scanner's lastRetrieved and lastAttempted (ListVulnerabilityScanners). Asynchronous. Forward answers
	// 409 when a collection of the scanner is already in progress; a refused enqueue can also record a failed attempt in the scanner's
	// collection status before the error comes back.
	CollectVulnerabilityScanner(ctx context.Context, name string) (*Response, error)

	// CreateChangeSetChangeRequest raises a ServiceNow change request for a change set (POST .../change-sets/{id}/servicenow/change-requests).
	// Forward checks the change set exists before it creates the ticket and creates the ticket before it links it. A change set's linked requests
	// (number and sys id) are listed by Predict.GetChangeSetSummary, so a lost response is recoverable only if the link was made; keep the answer's
	// SysID anyway.
	CreateChangeSetChangeRequest(ctx context.Context, networkID string, changeSetID string, input ServiceNowChangeRequestInput) (*ServiceNowChangeRequest, *Response, error)

	CreateInfoblox(ctx context.Context, input InfobloxInstanceRequest) (*InfobloxInstance, *Response, error)

	CreateRapid7(ctx context.Context, networkID string, input Rapid7SourceRequest) (*Rapid7Source, *Response, error)

	// CreateTenableSC adds a Tenable Security Center source (POST .../tenable-sources; MANAGE_END_HOST_SCANNERS). Name, BaseURL (http or
	// https) and CredentialID are required; see the type notes for the credential.
	CreateTenableSC(ctx context.Context, networkID string, input TenableSourceRequest) (*TenableSource, *Response, error)

	// CreateVulnerabilityScanner adds a Tenable VM scanner to the organization (POST /api/vulnerability-scanners;
	// MANAGE_VULNERABILITY_SCANNERS, org level). The connection test is a separate call, TestVulnerabilityScanner, and Forward does not require it.
	CreateVulnerabilityScanner(ctx context.Context, input NewVulnerabilityScanner) (*VulnerabilityScanner, *Response, error)

	// DeleteInfoblox removes one Infoblox instance by its numeric id; an instance that is already gone counts as success.
	// DELETE /api/integrations/infoblox/instances/{instanceId} (InfobloxController.deleteInstance, MANAGE_INFOBLOX_INTEGRATION; 204).
	DeleteInfoblox(ctx context.Context, instanceID string) (*Response, error)

	// DeleteRapid7 removes a Rapid7 source by name; a source that is already gone counts as success.
	// DELETE /api/networks/{networkId}/rapid7-sources/{sourceName} (EndHostScannerController.deleteRapid7Source,
	// DELETE_COLLECTION_SOURCES; 204).
	DeleteRapid7(ctx context.Context, networkID string, name string) (*Response, error)

	DeleteServiceNow(ctx context.Context) (*Response, error)

	// DeleteServiceNowCMDBConfiguration deletes the saved configuration, its connection and its mappings (DELETE
	// /integrations/servicenow-cmdb/configuration).
	DeleteServiceNowCMDBConfiguration(ctx context.Context) (*Response, error)

	// DeleteTenableSC removes the named source; a source that is already gone counts as success (DELETE .../tenable-sources/{sourceName};
	// DELETE_COLLECTION_SOURCES; 204). It does not delete the credential.
	DeleteTenableSC(ctx context.Context, networkID string, name string) (*Response, error)

	// DeleteVulnerabilityScanner removes a scanner (DELETE /api/vulnerability-scanners/{name}; MANAGE_VULNERABILITY_SCANNERS, org level). Networks
	// that used it stop receiving its data. Whether snapshots already built keep its data was not checked in source. A plain 404 counts as
	// already removed.
	DeleteVulnerabilityScanner(ctx context.Context, name string) (*Response, error)

	// DisableServiceNowCMDB switches the CMDB sync off without deleting the configuration (POST /integrations/servicenow-cmdb?action=disable,
	// 204).
	DisableServiceNowCMDB(ctx context.Context) (*Response, error)

	// DownloadChangeSetChangeRequestAttachment writes an attachment of a change request to w (GET .../change-requests/{sysId}/attachments/{fileSysId}).
	// Forward audits the download and sends the headers before the bytes, so a failure mid-stream leaves partial bytes in w with the error.
	DownloadChangeSetChangeRequestAttachment(ctx context.Context, networkID string, changeSetID string, sysID string, fileSysID string, w io.Writer) (*Response, error)

	// DownloadServiceNowCMDBPayload writes the payload preview of every configured network as a ZIP to w (GET /integrations/servicenow-cmdb
	// ?view=payload&disposition=attachment). A failure mid-stream leaves partial bytes in w with the error.
	DownloadServiceNowCMDBPayload(ctx context.Context, w io.Writer) (*Response, error)

	// DownloadServiceNowCMDBSyncReport writes the last sync's report as an .xlsx workbook to w (GET
	// /integrations/servicenow-cmdb/synchronization-report).
	DownloadServiceNowCMDBSyncReport(ctx context.Context, w io.Writer) (*Response, error)

	EnableServiceNowCMDB(ctx context.Context) (*Response, error)

	// GetChangeSetChangeRequest reads a change request of a change set by its ServiceNow sys id (GET .../change-requests/{sysId}).
	GetChangeSetChangeRequest(ctx context.Context, networkID string, changeSetID string, sysID string) (*ServiceNowChangeRequest, *Response, error)

	GetServiceNow(ctx context.Context) (*ServiceNowIntegrationSettings, bool, *Response, error)

	// GetServiceNowCMDBConfiguration reads the saved configuration (GET /integrations/servicenow-cmdb/configuration). The second result is
	// false when none is saved (404). The connection's secrets come back as empty strings.
	GetServiceNowCMDBConfiguration(ctx context.Context) (*ServiceNowCMDBConfiguration, bool, *Response, error)

	// GetServiceNowIncident reads an incident from the org's ServiceNow instance by its number (GET /integrations/servicenow/incidents/{number}).
	GetServiceNowIncident(ctx context.Context, number string) (*ServiceNowIncident, *Response, error)

	// InfobloxStatus tests every configured instance and returns each one's status (GET /api/integrations/infoblox/instances?view=status;
	// InfobloxController.getStatus, MANAGE_INFOBLOX_INTEGRATION). It contacts each Infoblox appliance, so it can be slow. Preview: shape from
	// Forward's source (InfobloxInstancesStatus), not seen live.
	InfobloxStatus(ctx context.Context) ([]InfobloxInstanceStatus, *Response, error)

	// LinkCheckIncident links an incident that already exists in ServiceNow to a check (POST /snapshots/{id}/checks/{checkId}
	// ?action=linkServiceNowIncident&incident=NUMBER).
	LinkCheckIncident(ctx context.Context, snapshotID string, checkID string, incidentNumber string) (*ServiceNowIncidentLink, *Response, error)

	// ListCheckIncidents lists the ServiceNow incidents linked to the snapshot's active checks of the given types (GET
	// /snapshots/{id}/checks?view=serviceNowIncidents&type=...). At least one type is required; Forward answers 400 for a name it does not know.
	ListCheckIncidents(ctx context.Context, snapshotID string, checkTypes []string) ([]ServiceNowIncidentLink, *Response, error)

	// ListInfoblox returns the organization's Infoblox instances (GET /api/integrations/infoblox, whose body is {"enabled":..,"instances":[..]};
	// InfobloxController.getInfobloxIntegration, MANAGE_INFOBLOX_INTEGRATION). Before v0.8.38 this read GET /api/integrations/infoblox/instances
	// without a query, a route Forward maps only with ?view=status (it answers the instances' connection status, not the instances): every build
	// answered it 400 `Parameter conditions "view=status" not met`, seen live by forward-skills on 26.10.6. See InfobloxStatus for that route.
	ListInfoblox(ctx context.Context) ([]InfobloxInstance, *Response, error)

	// ListInfobloxLegacy is ListInfoblox on the same route; kept for callers of the older name.
	ListInfobloxLegacy(ctx context.Context) ([]InfobloxInstance, *Response, error)

	// ListNetworkVulnerabilityScanners lists the scanners a network can use, including its end-host scanners (GET
	// /api/networks/{id}/vulnerability-scanners; VIEW_END_HOST_SCANNERS). Filter on Type for TENABLE_VM. A network with none gives an empty list.
	ListNetworkVulnerabilityScanners(ctx context.Context, networkID string) ([]NetworkVulnerabilityScanner, *Response, error)

	ListRapid7(ctx context.Context, networkID string) ([]Rapid7Source, *Response, error)

	// ListTenableSC lists the network's Tenable Security Center sources (GET .../end-host-scanners, which also lists Rapid7 sources; only
	// the rows of type TENABLE are returned). VIEW_END_HOST_SCANNERS. A network with none gives an empty list.
	ListTenableSC(ctx context.Context, networkID string) ([]TenableSource, *Response, error)

	// ListVulnerabilityScanners lists the organization's vulnerability scanners with the status of each one's latest collection (GET
	// /api/vulnerability-scanners; VIEW_VULNERABILITY_SCANNERS, org level).
	ListVulnerabilityScanners(ctx context.Context) ([]VulnerabilityScannerInfo, *Response, error)

	PatchServiceNow(ctx context.Context, input ServiceNowIntegrationRequest) (*Response, error)

	// PushServiceNowCMDB starts a push of the configured networks to ServiceNow (POST /integrations/servicenow-cmdb?action=push, 204). It
	// writes configuration items into the customer's CMDB. The push runs after the answer: read the outcome with ServiceNowCMDBSyncHistory.
	PushServiceNowCMDB(ctx context.Context) (*Response, error)

	// RefreshCheckIncidents re-reads the state of every incident linked to the snapshot's checks from ServiceNow (POST /snapshots/{id}/checks
	// ?action=refreshServiceNowIncidents, 204).
	RefreshCheckIncidents(ctx context.Context, snapshotID string) (*Response, error)

	SaveServiceNowCMDB(ctx context.Context, input ServiceNowCMDBConfiguration) (*Response, error)

	// ServiceNowCMDBConfigurationItems lists the configuration item classes the instance has (POST /integrations/servicenow-cmdb
	// ?view=configuration-items). It calls out to ServiceNow with the given connection.
	ServiceNowCMDBConfigurationItems(ctx context.Context, connection ServiceNowConnectionDetails) ([]string, *Response, error)

	// ServiceNowCMDBDiscoverySources lists the discovery sources the instance offers for the CMDB source name (POST /integrations/servicenow-cmdb
	// ?view=discovery-sources). It calls out to ServiceNow with the given connection.
	ServiceNowCMDBDiscoverySources(ctx context.Context, connection ServiceNowConnectionDetails) ([]string, *Response, error)

	// ServiceNowCMDBPayload previews what a push for one network would send (GET /integrations/servicenow-cmdb?view=payload&networkId=). A
	// problem building it comes back in the result's Error, not as an HTTP error.
	ServiceNowCMDBPayload(ctx context.Context, networkID string) (*ServiceNowCMDBPayloadPreview, *Response, error)

	ServiceNowCMDBSchema(ctx context.Context, input ServiceNowCMDBSchemaRequest) ([]ServiceNowCMDBSchema, *Response, error)

	// ServiceNowCMDBStatusReport checks the saved configuration against the instance (GET /integrations/servicenow-cmdb/status). It calls out
	// to ServiceNow, and answers 404 while no configuration is saved.
	ServiceNowCMDBStatusReport(ctx context.Context) (*ServiceNowCMDBStatus, *Response, error)

	// ServiceNowCMDBSyncHistory lists the recent pushes (GET /integrations/servicenow-cmdb/synchronization-history). It is empty before the
	// first push (Forward answers {"infos":[]}, not 404).
	ServiceNowCMDBSyncHistory(ctx context.Context) ([]ServiceNowCMDBSyncRun, *Response, error)

	// SetNetworkVulnerabilityScannerInclude turns a network's switch for a scanner on or off (PATCH
	// /api/networks/{id}/vulnerability-scanners/{name} with {"include": ...}; MANAGE_END_HOST_SCANNERS, Network Admin). The change applies to the
	// network's next snapshot. Forward answers 204. It refuses (400) a scanner the network may not use, and an include while a scanner of
	// another type is already included in the network. A name matching a legacy Rapid7 or Tenable Security Center source changes that source
	// instead.
	SetNetworkVulnerabilityScannerInclude(ctx context.Context, networkID string, name string, include bool) (*Response, error)

	// TestServiceNowCMDB checks that Forward can reach ServiceNow with the given connection (POST /integrations/servicenow-cmdb/test). A failed
	// connection is a 200 with the failure in the result, not an HTTP error. Forward may take a while to give up on an unreachable instance.
	TestServiceNowCMDB(ctx context.Context, connection ServiceNowConnectionDetails) (*ServiceNowCMDBTestResult, *Response, error)

	// TestTenableSC tests a connection without storing anything (POST .../tenable-sources?action=test; TEST_END_HOST_SCANNERS). Forward runs it
	// on a collector (the named one, else the network's default; 400 if none is online) and answers when it finishes, up to about 60 seconds,
	// so a shorter client timeout is raised to TenableTestTimeout (the ctx deadline still applies). A failed test is a 200 with Error set,
	// not an error return.
	TestTenableSC(ctx context.Context, networkID string, input TenableSourceTest) (*SourceTestResult, *Response, error)

	// TestVulnerabilityScanner tests a Tenable VM connection without storing anything (POST /api/vulnerability-scanner-tests;
	// MANAGE_VULNERABILITY_SCANNERS). Forward runs the test on the named collector and answers when it finishes, up to about 60 seconds, so a
	// shorter client timeout is raised to VulnerabilityScannerTestTimeout; the client's own longer limit and the ctx deadline still apply. A
	// failed test is a 200 with Error set.
	TestVulnerabilityScanner(ctx context.Context, input VulnerabilityScannerTest) (*VulnerabilityScannerTestResult, *Response, error)

	// UnlinkCheckIncident removes the link between a check and an incident (POST /snapshots/{id}/checks/{checkId}
	// ?action=unlinkServiceNowIncident&incident=NUMBER). It leaves the incident in ServiceNow alone.
	UnlinkCheckIncident(ctx context.Context, snapshotID string, checkID string, incidentNumber string) (*Response, error)

	// UpdateInfoblox patches one Infoblox instance by its numeric id.
	// PATCH /api/integrations/infoblox/instances/{instanceId} (InfobloxController.updateInstance, MANAGE_INFOBLOX_INTEGRATION; 204).
	UpdateInfoblox(ctx context.Context, instanceID string, patch InfobloxInstanceUpdate) (*Response, error)

	UpdateRapid7(ctx context.Context, networkID string, name string, input Rapid7SourceRequest) (*Rapid7Source, *Response, error)

	// UpdateServiceNow changes some fields of the ServiceNow connection and leaves the rest (PATCH /integrations/servicenow, the same route
	// as PatchServiceNow). It sends only the fields that are set, so it cannot switch a flag off by leaving it out. The first call for an org
	// has to carry InstanceURL and either a username and password or a token, because Forward builds the connection from it. Token and
	// username/password together are refused here, as Forward refuses them.
	UpdateServiceNow(ctx context.Context, update ServiceNowSettingsUpdate) (*ServiceNowIntegrationSettings, *Response, error)

	// UpdateTenableSC changes the named source (PATCH .../tenable-sources/{sourceName}; MANAGE_END_HOST_SCANNERS) and answers it as stored.
	UpdateTenableSC(ctx context.Context, networkID string, name string, patch TenableSourcePatch) (*TenableSource, *Response, error)

	// UpdateVulnerabilityScanner changes a scanner by name (PATCH /api/vulnerability-scanners/{name}; MANAGE_VULNERABILITY_SCANNERS). Nil fields
	// stay as they are. The stored scanner comes back.
	UpdateVulnerabilityScanner(ctx context.Context, name string, patch VulnerabilityScannerPatch) (*VulnerabilityScanner, *Response, error)
}

// Topology is the Topology API family: what Client.Topology sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Topology interface {
	// BackdateLinkOverrides applies the network's STAGED link overrides from
	// snapshotID onward and INVALIDATES every snapshot from that one on, so they
	// reprocess and their answers are unavailable until they finish; the network
	// has no staged overrides afterwards. POST
	// /api/networks/{id}/link-overrides?action=backdate&snapshotId=
	// (LinkOverridesService.backdateStagedLinkOverridesTo; on primary 15398425a69
	// and stable 67e89c87124). Needs EDIT_TOPOLOGY_LINKS and INVALIDATE_SNAPSHOTS.
	// Forward offers no route that lists the snapshots this would invalidate: it
	// invalidates every non-draft snapshot of the network created at or after
	// snapshotID's creation time, with no end.
	BackdateLinkOverrides(ctx context.Context, networkID string, snapshotID string) (*Response, error)

	// EditNetworkOverrides applies an incremental edit to the network's staged link
	// overrides (same body as the snapshot-scoped EditOverrides). Existing snapshots
	// are untouched; see BackdateLinkOverrides. An addition to one list supersedes
	// the link's override in the other. Forward refuses (400) an edit in which
	// presentAdditions overlaps presentRemovals or absentAdditions, or absentAdditions
	// overlaps absentRemovals; other overlaps are accepted. Both this and
	// SetNetworkOverrides are checked against the latest processed snapshot's ports
	// and answer 400 "Illegal ports in link overrides" for an unknown one.
	// PATCH /api/networks/{id}/link-overrides (TopologyController.patchLinkOverrides;
	// EDIT_TOPOLOGY_LINKS).
	EditNetworkOverrides(ctx context.Context, networkID string, edit TopologyOverridesEdit) (*Response, error)

	// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use EditNetworkOverrides or SetNetworkOverrides, which stage overrides for the next snapshot and leave existing snapshots alone until BackdateLinkOverrides (this route wrote to the snapshot and invalidated it).
	EditOverrides(ctx context.Context, snapshotID string, input TopologyOverridesEdit) (*Response, error)

	List(ctx context.Context, snapshotID string) ([]TopologyLink, *Response, error)

	// NetworkOverrides returns the network's STAGED link overrides: the ones the
	// network's next snapshot will use. Existing snapshots are not affected by
	// changes to them until BackdateLinkOverrides. GET
	// /api/networks/{id}/link-overrides (TopologyController.getLinkOverrides;
	// VIEW_NETWORK_AND_SNAPSHOTS). Forward 26.8's network-level replacement for the
	// snapshot-scoped Overrides, which is deprecated for removal.
	NetworkOverrides(ctx context.Context, networkID string) (*TopologyOverrides, *Response, error)

	// Deprecated: Forward deprecated the snapshot-scoped link override routes in 26.10 for removal in 26.11; use SnapshotOverrides to read one snapshot's overrides (NetworkOverrides reads the staged ones for the next snapshot).
	Overrides(ctx context.Context, snapshotID string) (*TopologyOverrides, *Response, error)

	// SetNetworkOverrides REPLACES all of the network's staged link overrides with
	// overrides. Existing snapshots are untouched; the next snapshot uses them, and
	// BackdateLinkOverrides applies them to an existing one. A nil list is sent as
	// [] (Forward rejects a missing one), and a link in both lists is refused by
	// Forward. PUT /api/networks/{id}/link-overrides
	// (TopologyController.putLinkOverrides; EDIT_TOPOLOGY_LINKS).
	SetNetworkOverrides(ctx context.Context, networkID string, overrides TopologyOverrides) (*Response, error)

	// SnapshotOverrides returns the link overrides in effect for one snapshot, through the network-level route that
	// replaces the deprecated snapshot-scoped Overrides. GET /api/networks/{id}/link-overrides?view=snapshot&snapshotId=
	// (TopologyController.getLinkOverridesAtSnapshot; VIEW_NETWORK_AND_SNAPSHOTS; Forward's 26.8 migration guide maps
	// the old GET to it).
	SnapshotOverrides(ctx context.Context, networkID string, snapshotID string) (*TopologyOverrides, *Response, error)

	// StagedOverrideChanges returns the link-override changes staged since the
	// network's latest processed snapshot, as the edit that turns that snapshot's
	// overrides into the staged ones. It is the whole change only for a backdate to
	// that latest snapshot: backdating to an older one replaces its overrides with the
	// full staged set, which can change more than this edit shows. GET
	// /api/networks/{id}/link-overrides?view=staged
	// (TopologyController.getRecentLinkOverrideChanges; LinkOverridesService
	// .getStagedLinkOverridesEdit). A link that moved between present and absent
	// appears only as an addition.
	StagedOverrideChanges(ctx context.Context, networkID string) (*TopologyOverridesEdit, *Response, error)
}

// Collections is the Collections API family: what Client.Collections sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Collections interface {
	DeviceStatuses(ctx context.Context, networkID string) ([]DeviceCollectionStatus, *Response, error)
}

// JumpServers is the JumpServers API family: what Client.JumpServers sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type JumpServers interface {
	// Create adds a key-authenticated jump server
	// (POST /api/networks/{networkId}/jumpServers).
	//
	// It used to POST /jump-servers with privateKey/certificate fields. Forward
	// has no such route on any build this SDK targets -- NetworkSetupController
	// maps only /jumpServers -- so every call 404'd.
	Create(ctx context.Context, networkID string, input JumpServerRequest) (*JumpServer, *Response, error)

	// CreateLegacy posts a caller-built legacy body to the same route as Create.
	//
	// Deprecated: use Create, which targets the same route with a validated
	// NewJumpServer body.
	CreateLegacy(ctx context.Context, networkID string, input LegacyJumpServerRequest) (*JumpServer, *Response, error)

	// CreateWithPassword adds a password-authenticated jump server.
	CreateWithPassword(ctx context.Context, networkID string, input JumpServerPasswordRequest) (*JumpServer, *Response, error)

	// Delete removes one jump server; one that is already gone counts as success. A 404 that means the route is not
	// served (ErrEndpointNotServed) is still an error.
	// DELETE /api/networks/{networkId}/jumpServers/{jumpServerId} (NetworkSetupController.deleteJumpServer).
	Delete(ctx context.Context, networkID string, jumpServerID string) (*Response, error)

	// List returns the network's jump servers.
	List(ctx context.Context, networkID string) ([]JumpServer, *Response, error)

	// Update patches one jump server. A change makes Forward re-handle the devices that go through it.
	// PATCH /api/networks/{networkId}/jumpServers/{jumpServerId} (NetworkSetupController.editJumpServer).
	Update(ctx context.Context, networkID string, jumpServerID string, patch JumpServerUpdate) (*Response, error)
}

// Compatibility is the Compatibility API family: what Client.Compatibility sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Compatibility interface {
	Checks(ctx context.Context, snapshotID string) (*ForwardHTTPDocument, *Response, error)

	ListSnapshots(ctx context.Context, networkID string) (*ForwardHTTPDocument, *Response, error)

	StartCollectorTask(ctx context.Context, networkID string) (*ForwardHTTPDocument, *Response, error)
}

// TrustedCertificates is the TrustedCertificates API family: what Client.TrustedCertificates sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type TrustedCertificates interface {
	// Add stores a new trusted certificate. Forward rejects a duplicate name AND
	// a certificate whose content already exists under another name (both are
	// validated server-side, TrustedCertificateService#validateUniqueness) with a
	// 400. Callers that reconcile a well-known certificate name (such as a
	// MITM/cloud-proxy CA) should List first and skip Add when an entry with the
	// same name and content is already present, so a repeat reconcile pass never
	// needlessly errors or forces a redundant Apply.
	Add(ctx context.Context, request NewTrustedCertificateRequest) (*TrustedCertificate, *Response, error)

	// Apply dispatches an ApplyCertificatesTask to every collector in the org
	// that supports trusted certificates and does not already have one queued or
	// running, and returns the resulting collector task IDs. It fans out
	// per-collector on the appserver; there is nothing further to scope here.
	//
	// A 409 means an apply task is already in progress for every supported
	// collector -- IsTrustedCertificateApplyInProgress classifies it so callers
	// can treat a repeat Apply as "already underway" rather than a failure.
	// Applying restarts every collector it reaches, so callers must not call this
	// on every reconcile tick; call it only after Add confirms new or changed
	// content (see Add's doc).
	Apply(ctx context.Context) ([]Identifier, *Response, error)

	// Delete removes a trusted certificate by name. It is idempotent: a 404 is
	// success, matching AccessControlService's device-access-label and group
	// deletes, because the desired state (the certificate is absent) already
	// holds.
	Delete(ctx context.Context, name string) (*Response, error)

	// List returns every trusted certificate in the caller's org.
	List(ctx context.Context) ([]TrustedCertificate, *Response, error)
}

// Vulnerabilities is the Vulnerabilities API family: what Client.Vulnerabilities sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Vulnerabilities interface {
	// DetectionLogic reads a CVE's configuration detection logic (GET /api/cves/{cveId}/detection-logic;
	// VulnerabilityAnalysisController.getCveConfigDetectionLogic at release/26.11.0-10 and master). The route is ORG-scoped: it takes no network
	// and no snapshot and is not under /api/networks, so a guard keyed on the network sees only the path.
	//
	// It needs the organization property CVE_DETECTION_LOGIC_EXPOSED (a GUI property, off by default, on-premises configurable, "enabled for
	// select strategic customers"); with it off Forward answers a 403 whose message is "CVE_DETECTION_LOGIC_EXPOSED is off for your
	// organization", which classifies as ErrFeatureGated (ErrorResponse.GateProperty names it). A CVE the index has never heard of is a 404; a
	// CVE the index has whose impact is not configuration-dependent answers 200 with an empty SourceType and Source. cveID must look like
	// CVE-YYYY-NNNN (a four-digit year and at least four digits, as Forward's CveId requires); lower case is accepted and sent upper-cased.
	// Preview, from Forward's source and controller test, not a live call.
	DetectionLogic(ctx context.Context, cveID string) (*CVEDetectionLogic, *Response, error)

	// Get returns one CVE with Forward's verdict for every possibly affected
	// device, and the configuration lines behind each. GET
	// /api/networks/{networkId}/vulnerabilities/{cveId} (getVulnerability). An
	// empty snapshotID uses the latest processed snapshot.
	Get(ctx context.Context, networkID string, cveID string, snapshotID string) (*VulnerabilityWithDevices, *Response, error)

	// List returns the CVEs that may affect the network, each with per-OS device
	// counts by detection result. GET /api/networks/{networkId}/vulnerabilities?v=2
	// (getVulnerabilities). For per-device verdicts on one CVE, use Get.
	List(ctx context.Context, networkID string, options VulnerabilityListOptions) (*VulnerabilityAnalysis, *Response, error)

	// ListByOS returns the full analysis as one row per CVE-OS combination, with
	// a result per possibly affected device. GET
	// /api/networks/{networkId}/vulnerabilities (getOsVulnerabilities, the v1
	// form). The response can be large; page it with Offset and Limit.
	ListByOS(ctx context.Context, networkID string, options VulnerabilityByOSOptions) (*OSVulnerabilityAnalysis, *Response, error)

	// ListDevices returns the network's devices with the CVEs that may affect them,
	// counted by detection result, severity, age and exploit, in one call -- the
	// inverse of List and Get, which are per CVE. A device with no CVE matching the
	// filters is left out. GET /api/networks/{networkId}/device-vulnerabilities
	// (VulnerabilityAnalysisController.getDeviceVulnerabilities, served on primary
	// 15398425a69 and stable 67e89c87124; VIEW_SECURITY_ANALYSIS). An empty
	// SnapshotID uses the latest processed snapshot. Preview: not in the published
	// spec.
	ListDevices(ctx context.Context, networkID string, options DeviceVulnerabilityListOptions) (*DeviceVulnerabilities, *Response, error)
}

// Aliases is the Aliases API family: what Client.Aliases sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Aliases interface {
	// Deactivate ends an alias at a snapshot: it stops being active at that
	// snapshot's creation time (Forward sets its end to one millisecond earlier)
	// and so for every later snapshot too, while snapshots created before still
	// have it. It returns the alias as it was. Deactivating is permanent for that
	// alias: to use the name again Put creates a NEW alias with it. An alias not active at the snapshot
	// counts as success and returns (nil, resp, nil). DELETE
	// /api/snapshots/{snapshotId}/aliases/{name} (deactivateAlias; EDIT_OBJECTS).
	//
	// The collection route (DELETE .../aliases, with repeated ?name=) deactivates
	// several at once, and with no name EVERY alias active at the snapshot; the
	// SDK does not wrap it.
	Deactivate(ctx context.Context, snapshotID string, name string) (*Alias, *Response, error)

	// Get returns one alias active at a snapshot, with its definition in
	// Definition, or (nil, nil) when none is. GET
	// /api/snapshots/{snapshotId}/aliases/{name} (getSingleAlias; VIEW_OBJECTS).
	// Forward adds the alias's resolved value to the definition, so Definition is
	// larger than what Put sent.
	Get(ctx context.Context, snapshotID string, name string) (*Alias, *Response, error)

	// List returns the aliases active at a snapshot. GET
	// /api/snapshots/{snapshotId}/aliases (getAllAliases, published; AliasController
	// on primary 15398425a69 and stable 67e89c87124).
	List(ctx context.Context, snapshotID string) ([]Alias, *Response, error)

	// Put creates an alias, or replaces the definition of the one with the same
	// name, and returns it. It is idempotent: putting the same alias again leaves
	// the same alias. PUT /api/snapshots/{snapshotId}/aliases/{name}
	// (createAlias, published; EDIT_OBJECTS; the snapshot must be past CREATION).
	//
	// WHEN IT TAKES EFFECT: an alias applies to the snapshot you name and every
	// LATER snapshot, including future ones ("Changes to a Snapshot's Aliases
	// propagate forward"); snapshots created before it do not see it. So put it on
	// the latest snapshot, and it carries on to the ones collected afterwards
	// without being put again. Aliases are stored per network, by the snapshot's
	// creation time.
	//
	// Replacing changes only the name and definition fields (Forward's
	// createOrReplace). A snapshot forked for Predict needs the PREDICT_UNSANDBOXED org
	// property.
	Put(ctx context.Context, snapshotID string, alias AliasBuilder) (*Alias, *Response, error)
}

// WanCircuits is the WanCircuits API family: what Client.WanCircuits sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type WanCircuits interface {
	// Backdate applies the network's CURRENT WAN circuits from snapshotID onward
	// and INVALIDATES every snapshot from that one on, so they reprocess and their
	// answers are unavailable until they finish. POST
	// .../wan-circuits?op=backdate&snapshotId= (WanCircuitService.backdateTo:
	// repo.backdateTo, then invalidateAffectedSnapshots from the snapshot's
	// creation instant with no end). Needs MANAGE_COLLECTION_SOURCES and
	// INVALIDATE_SNAPSHOTS. Preview: not in the published spec.
	Backdate(ctx context.Context, networkID string, snapshotID string) (*Response, error)

	// Delete removes one circuit. A 404 is success. DELETE .../wan-circuits/{name}
	// (deleteWanCircuit).
	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// Get returns one circuit, or (nil, nil) when it does not exist. GET
	// .../wan-circuits/{name} (getWanCircuit).
	Get(ctx context.Context, networkID string, name string) (*WanCircuit, *Response, error)

	// List returns the network's WAN circuits. GET .../wan-circuits (getWanCircuits).
	List(ctx context.Context, networkID string) ([]WanCircuit, *Response, error)

	// Patch changes the stated parts of one circuit and returns it as Forward now
	// holds it. PATCH .../wan-circuits/{name} (patchWanCircuit).
	Patch(ctx context.Context, networkID string, name string, patch WanCircuitPatch) (*WanCircuit, *Response, error)

	// Put adds or replaces one circuit. PUT .../wan-circuits/{name} (putWanCircuit).
	// Forward stores the circuit under the body's name, not the path's, so an
	// empty circuit.Name takes name and a different one is refused.
	Put(ctx context.Context, networkID string, name string, circuit WanCircuit) (*Response, error)

	// ReplaceAll replaces EVERY circuit of the network with circuits. PUT
	// .../wan-circuits (putWanCircuits). An empty list deletes them all, so it is
	// refused unless allowEmpty says that is the intent.
	ReplaceAll(ctx context.Context, networkID string, circuits []WanCircuit, allowEmpty bool) (*Response, error)
}

// DataFiles is the DataFiles API family: what Client.DataFiles sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type DataFiles interface {
	// Add uploads a new data file and returns it. It is attached to no network
	// until AddToNetwork. POST /api/data-files, multipart request (JSON) and file
	// (MANAGE_DATA_FILES; 201). Forward refuses files over 50 MB, headers on a
	// non-CSV file, duplicate headers, and a STIG type under any name but the
	// STIG policy file's.
	Add(ctx context.Context, request DataFileCreateRequest, content []byte) (*DataFile, *Response, error)

	// AddToNetwork attaches a data file to a network, so its next snapshots
	// collect it. POST /api/networks/{networkId}/data-files/{name}
	// (MANAGE_DATA_FILES and MANAGE_COLLECTION_SOURCES; 204). The STIG policy
	// file is refused: it is in every network already.
	AddToNetwork(ctx context.Context, networkID string, name string) (*Response, error)

	// AssessReplacement reports whether content could replace a data file without
	// breaking the queries that read it: true when the new content's inferred NQE
	// type is a subtype of the stored content's, so any query that works today
	// still works. It changes nothing. False also covers content that does not
	// parse as the file's type, which is reported as incompatible, not as an
	// error. POST /api/data-files/{name}?action=assess (MANAGE_DATA_FILES; refused
	// for the STIG policy file; 404 if the file does not exist).
	AssessReplacement(ctx context.Context, name string, content []byte, headers []string) (bool, *Response, error)

	// Content writes up to maxBytes of a data file's stored content to dst. GET
	// /api/data-files/{name}?view=content (VIEW_DATA_FILES). Forward refuses an
	// empty file with 400 "File cannot be downloaded as it has no content.".
	Content(ctx context.Context, name string, maxBytes int64, dst io.Writer) (written int64, truncated bool, resp *Response, err error)

	// Delete removes a data file from the organization's library, and so from
	// EVERY network it was attached to; queries reading it then see no
	// network.extensions.<nqeName> field for it. RemoveFromNetwork only detaches
	// one network. A file that does not exist counts as success (Forward answers
	// 404 "No data file with name '...' exists."). DELETE /api/data-files/{name}
	// (MANAGE_DATA_FILES). The content is not kept anywhere else: download it
	// first with Content if it may be needed again.
	Delete(ctx context.Context, name string) (*Response, error)

	// InferSchema previews what Forward would make of content as fileType --
	// format, NQE type and diagnostics -- without storing anything. POST
	// /api/data-files?action=inferSchema, multipart file and fileType
	// (MANAGE_DATA_FILES, although nothing is written). Forward refuses STIG, and
	// files over 50 MB.
	InferSchema(ctx context.Context, fileName string, content []byte, fileType DataFileType) (*DataFileInference, *Response, error)

	// List returns the org's data files. GET /api/data-files (VIEW_DATA_FILES).
	List(ctx context.Context) ([]DataFile, *Response, error)

	// ListForNetwork returns the names of the data files attached to a network.
	// GET /api/networks/{networkId}/data-files (VIEW_COLLECTION_SOURCES).
	ListForNetwork(ctx context.Context, networkID string) ([]string, *Response, error)

	// Patch changes a data file's name, NQE name or description and returns it.
	// The content is untouched. PATCH /api/data-files/{name} (MANAGE_DATA_FILES;
	// 404 if the file does not exist). Renaming or changing NQEName changes what
	// queries must call it, so it is a breaking change for any query reading the
	// old name.
	Patch(ctx context.Context, name string, patch DataFilePatch) (*DataFile, *Response, error)

	// RemoveFromNetwork detaches a data file from a network; snapshots taken after
	// no longer carry it, so queries reading it see status MISSING. DELETE
	// /api/networks/{networkId}/data-files/{name} (DELETE_COLLECTION_SOURCES;
	// 204). The STIG policy file cannot be removed from any network.
	RemoveFromNetwork(ctx context.Context, networkID string, name string) (*Response, error)

	// ReplaceContent replaces the content of an existing data file and returns
	// it. The file keeps its name, type, NQE name and network attachments; headers
	// name the columns of a CSV that has no header row, and are sent like Add's.
	// POST /api/data-files/{name}, multipart file and headerRow (MANAGE_DATA_FILES;
	// 404 if the file does not exist; 50 MB cap).
	//
	// Forward checks that the new content parses as the file's type and infers
	// its NQE type, but does NOT check that queries which read the file still
	// work: incompatible content is accepted. Run AssessReplacement first.
	//
	// To skip a replace that would change nothing, compare DataFile.ContentMD5Hex
	// (List) with the MD5 of the bytes Forward would store: your content as sent,
	// except that a CSV sent with headers is stored with the header line added in
	// front (I did not verify that format), and I did not verify the hash's letter
	// case, so compare case-insensitively.
	ReplaceContent(ctx context.Context, name string, content []byte, headers []string) (*DataFile, *Response, error)

	// Schema returns what Forward infers from a stored data file, in the same
	// form as InferSchema. GET /api/data-files/{name}/schema (VIEW_DATA_FILES;
	// refused for the STIG policy file).
	Schema(ctx context.Context, name string) (*DataFileInference, *Response, error)
}

// DataConnectorsAPI is the DataConnectors API family: what Client.DataConnectors sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type DataConnectorsAPI interface {
	// Add adds a connector and returns it. POST .../data-connectors
	// (addDataConnector; MANAGE_COLLECTION_SOURCES; 201).
	Add(ctx context.Context, networkID string, connector NewDataConnector) (*DataConnector, *Response, error)

	// Delete removes a connector. A 404 is success. DELETE
	// .../data-connectors/{name} (deleteDataConnector; DELETE_COLLECTION_SOURCES).
	Delete(ctx context.Context, networkID string, name string) (*Response, error)

	// Get returns one connector, or (nil, nil) when it does not exist. GET
	// .../data-connectors/{name} (getDataConnector).
	Get(ctx context.Context, networkID string, name string, options DataConnectorReadOptions) (*DataConnector, *Response, error)

	// List returns the network's connectors. GET .../data-connectors
	// (getDataConnectors; VIEW_COLLECTION_SOURCES).
	List(ctx context.Context, networkID string, options DataConnectorReadOptions) (*DataConnectors, *Response, error)

	// Test runs a connectivity test of the stored connector from its Collector
	// and returns the result, which Forward also stores as the connector's
	// TestResult. The call blocks until the test ends, raising a shorter client
	// timeout to DataConnectorTestTimeout (the ctx deadline still applies);
	// a failed test is a result with Error set, not an error. POST
	// .../data-connectors/{name}?action=test (testDataConnector;
	// TEST_COLLECTION_SOURCES).
	Test(ctx context.Context, networkID string, name string) (*DataConnectorTestResult, *Response, error)

	// Update changes the stated parts of a connector and returns it. PATCH
	// .../data-connectors/{name} (updateDataConnector; MANAGE_COLLECTION_SOURCES).
	// An empty patch is refused, as is an empty Endpoints list.
	Update(ctx context.Context, networkID string, name string, patch DataConnectorPatch) (*DataConnector, *Response, error)
}

// AuditLogsAPI is the AuditLogs API family: what Client.AuditLogs sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type AuditLogsAPI interface {
	// List returns audit records, newest first. GET /api/audit-logs
	// (AuditLogController.getAuditLogs; VIEW_AUDIT_LOGS, or a Forward admin).
	List(ctx context.Context, options AuditLogListOptions) (*AuditLogs, *Response, error)
}

// Dashboards is the Dashboards API family: what Client.Dashboards sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Dashboards interface {
	// Defaults returns the predefined dashboards Forward ships, each with an ID
	// and a name only. Forward says this is "not intended for use by the UI; exists
	// for API completeness". GET /api/networks/{networkId}/dashboards?type=DEFAULT
	// (VIEW_NETWORK_AND_SNAPSHOTS). Unpublished.
	Defaults(ctx context.Context, networkID string) ([]Dashboard, *Response, error)

	// DisplaySettings returns a dashboard's full-screen display settings. GET
	// /api/networks/{networkId}/dashboards/{dashboardId}/display-settings
	// (VIEW_NETWORK_AND_SNAPSHOTS). Unpublished.
	DisplaySettings(ctx context.Context, networkID string, dashboardID string) (*DashboardDisplaySettings, *Response, error)

	// Get returns one dashboard, custom or predefined, or (nil, nil) when it does
	// not exist. GET /api/networks/{networkId}/dashboards/{dashboardId}
	// (VIEW_CUSTOM_DASHBOARDS). Unpublished.
	Get(ctx context.Context, networkID string, dashboardID string) (*Dashboard, *Response, error)

	// List returns the network's custom dashboards. GET
	// /api/networks/{networkId}/dashboards (VIEW_CUSTOM_DASHBOARDS). Unpublished.
	List(ctx context.Context, networkID string) ([]Dashboard, *Response, error)
}

// Scorecards is the Scorecards API family: what Client.Scorecards sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type Scorecards interface {
	// Definitions returns the network's scorecard definitions (Forward documents
	// this view as "for debugging purposes"). GET
	// /api/networks/{networkId}/scorecards?view=definitions (VIEW_SCORECARDS).
	// Unpublished.
	Definitions(ctx context.Context, networkID string) ([]Scorecard, *Response, error)

	// ForSnapshot returns every scorecard scored on a snapshot, with per-category
	// results. GET /api/networks/{networkId}/scorecards?snapshotId=
	// (VIEW_SCORECARDS; the snapshot must be past the START stage). The snapshot
	// is required; there is no latest default. Unpublished.
	ForSnapshot(ctx context.Context, networkID string, snapshotID string) ([]SnapshotScorecard, *Response, error)

	// KPICategories returns the network's KPI categories with their checks. GET
	// /api/networks/{networkId}/kpi-categories (VIEW_SCORECARDS). Unpublished.
	KPICategories(ctx context.Context, networkID string) ([]KPICategory, *Response, error)

	// KPICategoryDefinitions returns the KPI category definitions, which carry the
	// check definitions where KPICategories carries the checks. GET
	// /api/networks/{networkId}/kpi-categories?view=definitions (VIEW_SCORECARDS).
	// Unpublished.
	KPICategoryDefinitions(ctx context.Context, networkID string) ([]KPICategory, *Response, error)

	// Trends returns each scorecard's score over time, keyed by scorecard ID, in
	// chronological order: up to maxPoints snapshots between start and end
	// inclusive. start must precede end and maxPoints must be at least 2; zero
	// maxPoints uses Forward's default of 30. GET
	// /api/networks/{networkId}/scorecards?view=trends&start=&end=&maxPoints=
	// (VIEW_SCORECARDS). Times go out as UTC. Unpublished.
	Trends(ctx context.Context, networkID string, start time.Time, end time.Time, maxPoints int) (map[string][]ScorecardPoint, *Response, error)
}

// CollectionSchedules is the CollectionSchedules API family: what Client.CollectionSchedules sends. A method may be added in any release; a type that implements this
// port to fake it should embed the interface.
type CollectionSchedules interface {
	// Create adds a collection schedule to the network and returns it with the ID
	// Forward gave it. POST /api/networks/{id}/collection-schedules
	// (addCollectionSchedule; EDIT_COLLECTION_SCHEDULES). Forward answers 400 for a
	// duplicate of an existing schedule. A network can have several schedules.
	Create(ctx context.Context, networkID string, definition CollectionScheduleDefinition) (*CollectionSchedule, *Response, error)

	// Delete removes a collection schedule; the network then collects only on the
	// schedules it has left, or never if it had one. A schedule that does not
	// exist counts as success. DELETE
	// /api/networks/{id}/collection-schedules/{scheduleId}
	// (deleteCollectionSchedule; EDIT_COLLECTION_SCHEDULES; 204).
	Delete(ctx context.Context, networkID string, scheduleID string) (*Response, error)

	// Get returns one collection schedule, or (nil, nil) when it does not exist.
	// GET /api/networks/{id}/collection-schedules/{scheduleId}
	// (getCollectionSchedule).
	Get(ctx context.Context, networkID string, scheduleID string) (*CollectionSchedule, *Response, error)

	// List returns the network's collection schedules. GET
	// /api/networks/{id}/collection-schedules (getCollectionSchedules).
	List(ctx context.Context, networkID string) ([]CollectionSchedule, *Response, error)

	// Replace overwrites the schedule with schedule.ID by the given definition: it
	// replaces, not merges, so every field is restated. To change one field, read
	// the schedule, edit schedule.Definition() and send it back. PUT
	// /api/networks/{id}/collection-schedules/{scheduleId} (replaceCollectionSchedule;
	// EDIT_COLLECTION_SCHEDULES). Forward requires the ID in the path and in the
	// body to match, which this does by construction, and answers 400 for a
	// duplicate of another schedule.
	Replace(ctx context.Context, networkID string, scheduleID string, definition CollectionScheduleDefinition) (*Response, error)
}
