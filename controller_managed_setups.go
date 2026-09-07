package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ControllerManagedSetupsService manages SD-WAN controller-managed setups:
// the record that tells Forward a controller (vManage, vBond, vSmart) owns a
// set of devices, rather than each device being collected classically.
//
// Preview: these routes are not in the published OpenAPI set.
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
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.List")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Create adds a controller-managed setup.
func (s *ControllerManagedSetupsService) Create(ctx context.Context, networkID string, input NewControllerManagedSetup) (*ControllerManagedSetup, *Response, error) {
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
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ControllerManagedSetups.Create")
	out := new(ControllerManagedSetup)
	resp, err := s.client.Do(req, out)
	return out, resp, err
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
	if isStatus(err, http.StatusNotFound) {
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
