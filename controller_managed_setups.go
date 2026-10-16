package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ControllerManagedSetupsService manages SD-WAN controller-managed setups:
// the record that tells Forward a controller (vManage, vBond, vSmart) owns a
// set of devices, rather than each device being collected classically.
//
// Published in Forward's OpenAPI (controller-managed-setups.yaml) but tagged Experimental: the contract may change without
// deprecation.
type ControllerManagedSetupsService service

// ControllerManagedSetup is a setup as Forward returns it.
type ControllerManagedSetup struct {
	Name        string             `json:"name"`
	Controllers []ControllerDevice `json:"controllers"`

	// ManagedDevices is absent for controller-only setups and for a setup that
	// has never had guests declared. Forward DISCOVERS guests during the SETUP
	// phase of a connectivity test -- the vSmart reports them -- but discovery
	// does not register them: a setup whose test just found four guests still
	// reads back managedDevices null until someone writes them.
	ManagedDevices []ManagedDevice `json:"managedDevices,omitempty"`

	// CollectorID is the setup's preferred collector (StoredControllerManagedSetup.collectorId), one for the whole setup:
	// its controllers and guests are collected through it ("C<n>"). Empty means the network's default collector.
	CollectorID Identifier `json:"collectorId,omitempty"`
}

// ControllerDevice is one controller within a setup.
//
// BGPAdvertisementCollectionOptions and SNMPCollectionConfig carry the same
// settings the classic batch writes as collectBgpAdvertisements/bgpTableType/
// bgpPeerType and enableSnmpCollection. Forward unwraps the meta record into
// the controller entry, which is why they sit at this level rather than in a
// nested object of their own.
type ControllerDevice struct {
	Name                              string                `json:"name"`
	Type                              string                `json:"type,omitempty"`
	Host                              string                `json:"host,omitempty"`
	CLICredentialID                   string                `json:"cliCredentialId,omitempty"`
	SNMPCredentialID                  string                `json:"snmpCredentialId,omitempty"`
	JumpServerID                      string                `json:"jumpServerId,omitempty"`
	BGPAdvertisementCollectionOptions *BGPCollectionOptions `json:"bgpAdvertisementCollectionOptions,omitempty"`
	SNMPCollectionConfig              *SNMPCollectionConfig `json:"snmpCollectionConfig,omitempty"`

	// TestResult is the device's last connectivity test; only ListWithTestResults fills it, and it stays nil for a device
	// that has never been tested.
	TestResult *DeviceTestResult `json:"testResult,omitempty"`
}

// ManagedDevice is one guest device collected THROUGH a setup's controller
// rather than as a classic source of its own.
//
// Forward calls this a "controller-managed device". On the wire it is the same
// flattened shape as ControllerDevice -- Forward's ControllerManagedDeviceWithMeta
// @JsonUnwrap's a DeviceSetup (name/type/host/credentials) and a DeviceSetupMeta
// (collect/note/snmp) into one object -- so the fields below mirror the DeviceSetup
// field names exactly.
//
// Type matters and is easy to get wrong: it is the guest's OWN connection type,
// not the controller's and not a vendor-family alias. A Cisco C8000V running
// IOS-XE in SD-WAN controller mode is CISCO_IOS_XE_SSH (cisco_ios_xe_ssh) even
// though its controller is a vSmart -- Forward's DeviceConnType lists
// VIPTELA_SMART_SSH.relatedTypes as {VIPTELA_EDGE_SSH, CISCO_IOS_XE_SSH,
// CISCO_IOS_XE_TELNET}, and StoredControllerManagedSetup rejects anything else.
// Declaring such a device cisco_sdwan_ssh makes Forward's type discovery answer
// DEVICE_TYPE_MISMATCH with discoveredType cisco_ios_xe_ssh (measured on cs-lab
// network 3150, 2026-09-07).
//
// CLICredentialID is per-guest and required in practice: nothing is inherited
// from the controller. StoredControllerManagedSetup.buildDeviceConfigs emits one
// DeviceConfig per managed device from that device's own DeviceSetup, so a guest
// with no credential is a guest Forward cannot log into.
type ManagedDevice struct {
	Name                              string                `json:"name"`
	Type                              string                `json:"type,omitempty"`
	Host                              string                `json:"host,omitempty"`
	CLICredentialID                   string                `json:"cliCredentialId,omitempty"`
	SNMPCredentialID                  string                `json:"snmpCredentialId,omitempty"`
	JumpServerID                      string                `json:"jumpServerId,omitempty"`
	BGPAdvertisementCollectionOptions *BGPCollectionOptions `json:"bgpAdvertisementCollectionOptions,omitempty"`
	SNMPCollectionConfig              *SNMPCollectionConfig `json:"snmpCollectionConfig,omitempty"`

	// Collect is DeviceSetupMeta.collect. Absent means true (Forward's
	// isCollectionEnabled treats null as enabled), so leave it nil to collect.
	Collect *bool `json:"collect,omitempty"`

	// TestResult is the device's last connectivity test; only ListWithTestResults fills it.
	TestResult *DeviceTestResult `json:"testResult,omitempty"`
}

// DeviceTestResult is the part of a device's connectivity test result (Forward's SourceConnectivityResult) the SDK models.
// Error is the normalised error code (AUTHENTICATION_FAILED, PING_FAILED, ...; empty when the test passed) and ErrorPhase
// the phase it failed in; it decodes exactly as SourceTestStatus does.
//
// Forward returns the LAST SAVED result, so right after Test the previous one is still here until the new test finishes:
// compare EndTime with when you queued the test. Forward writes the times as ISO-8601 strings (an epoch number is kept as its digits);
// parse them before comparing, because ISO strings with different fractional digits do not sort as text.
// An object with no known key decodes to an all-empty value, which reads as "no error", so check EndTime before treating
// empty as a pass.
type DeviceTestResult struct {
	Error           string           `json:"error,omitempty"`
	ErrorPhase      string           `json:"errorPhase,omitempty"`
	DiscoveredType  string           `json:"discoveredType,omitempty"`
	ControllerInfos []ControllerInfo `json:"controllerInfos,omitempty"`
	SavedAt         string           `json:"savedAt,omitempty"`
	EndTime         string           `json:"endTime,omitempty"`
	StartTime       string           `json:"startTime,omitempty"`

	// ManagedDevices is, for a controller, the devices its QUERY phase reported managing (name and address; Forward's hidden
	// managedDevices, "only used for controller-managed setups"). It is not the guest list HOST_NOT_REPORTING_ANY_GUEST is
	// raised from: that comes from the SETUP phase (SetupPhaseResult.getDependentsFromMaster), and Forward does not serialize
	// it, the per-phase results, the device's host/guest role, or the request's operation id and connection params. No phase
	// result records the commands that were run; only UnauthorizedCommands is on the wire. An empty ManagedDevices on a controller whose
	// error is HOST_NOT_REPORTING_ANY_GUEST (a SETUP-phase error) is therefore consistent, and a non-empty one is not proof
	// the setup phase found guests.
	ManagedDevices []ControllerInfo `json:"managedDevices,omitempty"`
	// DependentNameAndAliases is, for a guest, the names and aliases it reported (hidden; used to match it to its controller).
	DependentNameAndAliases []string `json:"dependentNameAndAliases,omitempty"`
	// NameAliases are the hostnames and aliases the QUERY phase found for the device (hidden).
	NameAliases []string `json:"nameAliases,omitempty"`
	// HostIPs are the addresses the connection phase reached; UnauthorizedCommands the commands the authorization phase found
	// the account may not run.
	HostIPs              []string `json:"hostIps,omitempty"`
	UnauthorizedCommands []string `json:"unauthorizedCommands,omitempty"`
	// The credentials the test found working (a controller or guest left to auto-association shows them here).
	DiscoveredCLICredentialID  string `json:"discoveredCliCredentialId,omitempty"`
	DiscoveredHTTPCredentialID string `json:"discoveredHttpCredentialId,omitempty"`
	DiscoveredSNMPCredentialID string `json:"discoveredSnmpCredentialId,omitempty"`
}

func (r *DeviceTestResult) UnmarshalJSON(data []byte) error {
	var status SourceTestStatus
	if err := status.UnmarshalJSON([]byte(`{"testResult":` + string(data) + `}`)); err != nil {
		return err
	}
	*r = DeviceTestResult{Error: status.ConnectivityError, ErrorPhase: status.ErrorPhase, DiscoveredType: status.DiscoveredType, ControllerInfos: status.ControllerInfos}
	var fields map[string]any
	if json.Unmarshal(data, &fields) == nil {
		r.SavedAt, r.EndTime, r.StartTime = firstAnyString(fields, "savedAt"), firstAnyString(fields, "endTime"), firstAnyString(fields, "startTime")
		r.DiscoveredCLICredentialID = firstAnyString(fields, "discoveredCliCredentialId")
		r.DiscoveredHTTPCredentialID = firstAnyString(fields, "discoveredHttpCredentialId")
		r.DiscoveredSNMPCredentialID = firstAnyString(fields, "discoveredSnmpCredentialId")
		r.ManagedDevices = controllerInfoList(fields["managedDevices"])
		r.DependentNameAndAliases = stringList(fields["dependentNameAndAliases"])
		r.NameAliases = stringList(fields["nameAliases"])
		r.HostIPs = stringList(fields["hostIps"])
		r.UnauthorizedCommands = stringList(fields["unauthorizedCommands"])
	}
	return nil
}

type BGPCollectionOptions struct {
	CollectBGPAdvertisements bool   `json:"collectBgpAdvertisements"`
	TableType                string `json:"tableType,omitempty"`
	PeerType                 string `json:"peerType,omitempty"`
}

type SNMPCollectionConfig struct {
	EnableSNMPCollection bool `json:"enableSnmpCollection"`
}

// NewControllerManagedSetup creates a setup.
//
// The name is LOWERCASED by Forward on the way in
// (NewControllerManagedSetup lowercases it in the constructor), so a caller
// that compares a returned name against the one it sent must fold case or it
// will see a difference that is not there.
type NewControllerManagedSetup struct {
	Name           string             `json:"name"`
	Controllers    []ControllerDevice `json:"controllers"`
	ManagedDevices []ManagedDevice    `json:"managedDevices,omitempty"`
	// CollectorID pins the whole setup to one collector. Empty sends nothing, so the network's default collector applies.
	// A setup's devices have no collector pin that takes effect (Forward stores a hidden collectorId on a device if one is
	// written, but only the setup's is used).
	CollectorID Identifier `json:"collectorId,omitempty"`
}

func (s *ControllerManagedSetupsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/controller-managed-setups", nil
}

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
func (s *ControllerManagedSetupsService) List(ctx context.Context, networkID string) ([]ControllerManagedSetup, *Response, error) {
	return s.list(ctx, networkID, "", "ControllerManagedSetups.List")
}

// ListWithTestResults is List with ?with=testResult: each controller and guest carries its last connectivity test
// (DeviceTestResult), which is where the outcome of Test shows up. The same read is available per device on classic devices
// through ClassicDevices.ListTestStatuses.
func (s *ControllerManagedSetupsService) ListWithTestResults(ctx context.Context, networkID string) ([]ControllerManagedSetup, *Response, error) {
	return s.list(ctx, networkID, "?with=testResult", "ControllerManagedSetups.ListWithTestResults")
}

func (s *ControllerManagedSetupsService) list(ctx context.Context, networkID, query, operation string) ([]ControllerManagedSetup, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	// AllowSingle is deliberately OFF. This list drives a delete-and-recreate
	// reconciliation, so a misread is not a cosmetic problem: with a single
	// object accepted, an ERROR body like {"error":"forbidden"} would decode
	// into one setup with an empty name, match nothing, and let the drifted
	// setup survive exactly as if the list had been empty. An object carrying
	// none of the known keys must therefore be an ERROR.
	result := listResponse[ControllerManagedSetup]{
		Keys: []string{"setups", "data", "items", "results"},
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+query, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Create adds a controller-managed setup.
//
// Names are unique across every collection source in the network, so a controller or guest that a classic device
// already holds under the same name is refused with a 400; CreateMigratingClassic is the one atomic way to take such
// devices over.
func (s *ControllerManagedSetupsService) Create(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error) {
	return s.create(ctx, networkID, input, "", "ControllerManagedSetups.Create")
}

// CreateMigratingClassic adds a controller-managed setup and, in the same transaction, DELETES every classic device
// whose name matches one of the setup's controllers or managed devices (POST ...controller-managed-setups?action=migrate,
// addControllerManagedSetupWithMigration, published as Experimental). It is how the UI moves devices that were added
// as classic devices into a setup: if the setup is refused, nothing is deleted; if it is accepted, the classic devices are
// gone, with their per-device settings (credentials, collection options), their connectivity test results and their device tags.
// Only the setup's controllers and managed devices are matched, not the setup's own name (a classic device called that is
// still a 400). A name held by a
// non-classic source (another setup, an endpoint) is still a 400 and rolls the whole thing back. Names that match no
// classic device are simply added. Read the classic devices you are about to replace first.
func (s *ControllerManagedSetupsService) CreateMigratingClassic(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error) {
	return s.create(ctx, networkID, input, "?action=migrate", "ControllerManagedSetups.CreateMigratingClassic")
}

func (s *ControllerManagedSetupsService) create(ctx context.Context, networkID string, input NewControllerManagedSetup, query, operation string) (*ControllerManagedSetup, *Response, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, errors.New("forward: controller-managed setup name is required")
	}
	if len(input.Controllers) == 0 {
		return nil, nil, errors.New("forward: a controller-managed setup requires at least one controller")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+query, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

// ControllerAndManagedDeviceSelection picks the devices a setup test runs on: by device name, or by setup name (every
// controller and managed device of those setups). Exactly one of the two must be non-empty (Forward's own rule).
type ControllerAndManagedDeviceSelection struct {
	Names      []string `json:"names,omitempty"`
	SetupNames []string `json:"setupNames,omitempty"`
}

// Test queues a connectivity test of the selected controllers and managed devices (POST
// ...controller-managed-setups?action=test, testControllerManagedSetups, published as Experimental; the UI's "Test" button). Forward answers
// 204 as soon as the tests are queued, not when they finish: read each device's testResult afterwards (ListWithTestResults, or
// ClassicDevices.ListTestStatuses for a device still held as a classic one). SetupNames are matched case-sensitively against the stored (lowercase) names, so Test folds
// them to lowercase; a name that matches no setup tests nothing and still answers 204. collectorID, when not empty, runs them through that collector instead of the setup's own.
func (s *ControllerManagedSetupsService) Test(ctx context.Context, networkID, collectorID string, selection ControllerAndManagedDeviceSelection) (*Response, error) {
	if (len(selection.Names) == 0) == (len(selection.SetupNames) == 0) {
		return nil, errors.New("forward: exactly one of names or setupNames must be given")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	if len(selection.SetupNames) != 0 {
		folded := make([]string, len(selection.SetupNames))
		for i, name := range selection.SetupNames {
			folded[i] = strings.ToLower(strings.TrimSpace(name))
		}
		selection.SetupNames = folded
	}
	query := url.Values{"action": []string{"test"}}
	if collectorID = strings.TrimSpace(collectorID); collectorID != "" {
		query.Set("collectorId", collectorID)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), selection)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Test")
	return s.client.Do(req, nil)
}

// Delete removes a setup by name. A 404 is success: the desired state, that
// the setup is absent, already holds.
func (s *ControllerManagedSetupsService) Delete(ctx context.Context, networkID, setupName string) (*Response, error) {
	setupName = strings.TrimSpace(setupName)
	if setupName == "" {
		return nil, errors.New("forward: controller-managed setup name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/"+url.PathEscape(setupName), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

// ControllerManagedSetupPatch is a partial update of a setup.
//
// Every field is a pointer because Forward's ControllerManagedSetupPatch uses
// JsonProp<T>, which distinguishes three states, not two: ABSENT leaves the
// existing value alone, present-and-null clears it, and present-and-set
// replaces it. A plain slice cannot say "leave the guests as they are" and
// "this setup has no guests" differently, and confusing those two would
// silently delete every guest on a patch that meant to touch nothing else.
type ControllerManagedSetupPatch struct {
	// ManagedDevices replaces the setup's whole guest list when present.
	// Point it at an empty slice to remove every guest; leave it nil to keep
	// the stored list untouched.
	ManagedDevices *[]ManagedDevice `json:"managedDevices,omitempty"`
}

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
func (s *ControllerManagedSetupsService) Patch(
	ctx context.Context,
	networkID, setupName string,
	patch ControllerManagedSetupPatch,
) (*ControllerManagedSetup, *Response, error) {
	setupName = strings.TrimSpace(setupName)
	if setupName == "" {
		return nil, nil, errors.New("forward: controller-managed setup name is required")
	}
	base, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(
		ctx,
		http.MethodPatch,
		base+"/"+url.PathEscape(strings.ToLower(setupName)),
		patch,
	)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Patch")
	out := new(ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
}

func stringList(raw any) []string {
	items, _ := raw.([]any)
	var out []string
	for _, item := range items {
		if value := anyString(item); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func controllerInfoList(raw any) []ControllerInfo {
	items, _ := raw.([]any)
	var out []ControllerInfo
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			out = append(out, ControllerInfo{Name: firstAnyString(object, "name"), IPAddress: firstAnyString(object, "ipAddress")})
		}
	}
	return out
}
