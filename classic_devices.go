package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ClassicDevicesService manages device collection sources.
type ClassicDevicesService service

// ClassicDeviceRequest contains the common published source fields. Fields
// carries additional version-specific properties; typed fields win on conflict.
type ClassicDeviceRequest struct {
	Name             string
	Host             string
	Type             string
	Port             *int32
	CLICredentialID  string
	CLICredential2ID string
	CLICredential3ID string
	HTTPCredentialID string
	SNMPCredentialID string
	JumpServerID     string
	Collect          *bool
	Note             string
	// CollectorID pins the device to one collector (wire field collectorId, a
	// DeviceSetupMeta property). Empty sends nothing, so the network's default
	// collector applies. Preview: the appserver hides it from the published spec.
	CollectorID string
	// Collection options (NewClassicDevice). Pointers and empty strings send
	// nothing, so Forward's own default applies.
	//   BGPTableType: BOTH, ADJ_RIB_IN or ADJ_RIB_OUT (honoured only when
	//   CollectBGPAdvertisements is true; default BOTH).
	//   BGPPeerType: BOTH, EBGP or IBGP (same condition; default BOTH).
	//   BGPAndBMPFailureHandling: FAIL_DEVICE (default), SWITCH_TO_CLI,
	//   ALWAYS_USE_CLI, ALWAYS_USE_BMP or CONTINUE_WITHOUT_BGP_DATA.
	EnableSNMPCollection     *bool
	CollectBGPAdvertisements *bool
	BGPTableType             string
	BGPPeerType              string
	BGPAndBMPFailureHandling string
	DisableIPv6Collection    *bool
	FullCollectionLog        *bool
	Fields                   map[string]any
}

// MarshalJSON preserves new appliance fields without weakening the common
// typed contract.
func (r ClassicDeviceRequest) MarshalJSON() ([]byte, error) {
	fields := make(map[string]any, len(r.Fields)+12)
	for key, value := range r.Fields {
		fields[key] = value
	}
	fields["name"] = r.Name
	fields["host"] = r.Host
	if r.Type != "" {
		fields["type"] = r.Type
	}
	if r.Port != nil {
		fields["port"] = *r.Port
	}
	for key, value := range map[string]string{
		"cliCredentialId": r.CLICredentialID, "cliCredential2Id": r.CLICredential2ID,
		"cliCredential3Id": r.CLICredential3ID, "httpCredentialId": r.HTTPCredentialID,
		"snmpCredentialId": r.SNMPCredentialID, "jumpServerId": r.JumpServerID, "note": r.Note,
	} {
		if value != "" {
			fields[key] = value
		}
	}
	if r.Collect != nil {
		fields["collect"] = *r.Collect
	}
	if r.CollectorID != "" {
		fields["collectorId"] = r.CollectorID
	}
	for key, value := range map[string]string{
		"bgpTableType": r.BGPTableType, "bgpPeerType": r.BGPPeerType, "bgpAndBmpFailureHandling": r.BGPAndBMPFailureHandling,
	} {
		if value != "" {
			fields[key] = value
		}
	}
	for key, value := range map[string]*bool{
		"enableSnmpCollection": r.EnableSNMPCollection, "collectBgpAdvertisements": r.CollectBGPAdvertisements,
		"disableIpv6Collection": r.DisableIPv6Collection, "fullCollectionLog": r.FullCollectionLog,
	} {
		if value != nil {
			fields[key] = *value
		}
	}
	return json.Marshal(fields)
}

// ClassicDevice is a collection source. Raw retains the complete wire object
// so callers can inspect fields introduced by newer Forward versions.
type ClassicDevice struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	Type             string `json:"type,omitempty"`
	Port             *int32 `json:"port,omitempty"`
	CLICredentialID  string `json:"cliCredentialId,omitempty"`
	HTTPCredentialID string `json:"httpCredentialId,omitempty"`
	Collect          *bool  `json:"collect,omitempty"`
	Note             string `json:"note,omitempty"`
	// CollectorID is the collector this device is pinned to (collectorId in
	// DeviceSetupMeta, unwrapped into the device object); empty when the read
	// did not state one, meaning the network's default collector. The appserver
	// serialises it as "C<n>" (CollectorId's @JsonValue) and accepts "C<n>" or "<n>".
	CollectorID Identifier `json:"collectorId,omitempty"`
	// EnableSNMPCollection and SNMPCredentialID say whether Forward polls the device over SNMP, and with which credential -- whether
	// performance data can exist for it at all. EnableSNMPCollection is nil when the read did not state it.
	EnableSNMPCollection *bool  `json:"enableSnmpCollection,omitempty"`
	SNMPCredentialID     string `json:"snmpCredentialId,omitempty"`
	// CLICredential2ID is the PRIVILEGED_MODE credential (Cisco-like, Checkpoint)
	// or SHELL credential (Avi); CLICredential3ID is the EXPERT_MODE one
	// (Checkpoint). Empty when none is set.
	CLICredential2ID string `json:"cliCredential2Id,omitempty"`
	CLICredential3ID string `json:"cliCredential3Id,omitempty"`
	JumpServerID     string `json:"jumpServerId,omitempty"`
	// Per-device collection options, nil/empty when the read did not state them.
	// Forward leaves bgpTableType and bgpPeerType out of the response when they
	// are BOTH, so empty with CollectBGPAdvertisements true means BOTH.
	// BGPTableType: BOTH, ADJ_RIB_IN, ADJ_RIB_OUT. BGPPeerType: BOTH, EBGP, IBGP.
	// BGPAndBMPFailureHandling: FAIL_DEVICE, SWITCH_TO_CLI, ALWAYS_USE_CLI,
	// ALWAYS_USE_BMP, CONTINUE_WITHOUT_BGP_DATA.
	CollectBGPAdvertisements *bool  `json:"collectBgpAdvertisements,omitempty"`
	BGPTableType             string `json:"bgpTableType,omitempty"`
	BGPPeerType              string `json:"bgpPeerType,omitempty"`
	BGPAndBMPFailureHandling string `json:"bgpAndBmpFailureHandling,omitempty"`
	DisableIPv6Collection    *bool  `json:"disableIpv6Collection,omitempty"`
	FullCollectionLog        *bool  `json:"fullCollectionLog,omitempty"`
	// Who created and last changed the device, and when (Forward's Attribution, unwrapped into the device; the read fills the
	// usernames, which are empty for a deleted user). Empty when Forward states none, as for a device created before it
	// recorded them. Times are ISO-8601 strings.
	CreatedBy   string     `json:"createdBy,omitempty"`
	CreatedByID Identifier `json:"createdById,omitempty"`
	CreatedAt   string     `json:"createdAt,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
	UpdatedByID Identifier `json:"updatedById,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
}

// ClassicDeviceBatchItem is the putBatch wire shape.
type ClassicDeviceBatchItem struct {
	Name                     string `json:"name"`
	Type                     string `json:"type,omitempty"`
	Host                     string `json:"host"`
	Port                     int    `json:"port,omitempty"`
	CLICredentialID          string `json:"cliCredentialId,omitempty"`
	SNMPCredentialID         string `json:"snmpCredentialId,omitempty"`
	JumpServerID             string `json:"jumpServerId,omitempty"`
	CollectBGPAdvertisements bool   `json:"collectBgpAdvertisements"`
	BGPTableType             string `json:"bgpTableType"`
	BGPPeerType              string `json:"bgpPeerType"`
	EnableSNMPCollection     bool   `json:"enableSnmpCollection"`
	// Collect is Forward's per-device collection toggle -- the switch in the
	// Sources table. A POINTER, and deliberately so: this batch is a PUT, and
	// the three states have to stay distinguishable. Absent sends nothing, so
	// Forward applies its default (collect on): putBatch builds the stored
	// device from the request alone (ClassicDeviceService.saveDevices ->
	// NewClassicDevice.toStored) and replaces the existing one, so absent does
	// NOT keep the device's current toggle. A non-nil false is an explicit
	// request to onboard the device and NOT collect it.
	//
	// That third state is the point. A device can be present in the inventory,
	// carrying its credentials, its jump server and its type, with collection
	// off -- which is how you onboard something an operator may want to switch
	// on later without collecting it today, and how a synthetic-node member
	// stays visible beside the cloud that represents it.
	//
	// Same field name and semantics as ClassicDeviceRequest.Collect on the
	// single-device path, which has carried it since the client was written;
	// this only closes the gap on the batch path Skyforge actually uses.
	Collect *bool `json:"collect,omitempty"`
	// CollectorID pins the device to one collector; empty sends nothing, so the
	// network's default collector applies. putBatch replaces the stored device
	// with one built from this item alone, so empty does NOT keep a collector
	// the device already had; state it again on every PUT.
	CollectorID string `json:"collectorId,omitempty"`
	// CLICredential2ID (PRIVILEGED_MODE, or SHELL for Avi) and CLICredential3ID
	// (EXPERT_MODE, Checkpoint) are omitted when empty. HTTPCredentialID is not
	// modelled on this type. The four BGP/SNMP fields above are always sent, as
	// before. Because putBatch replaces the device, every field left empty or
	// nil here (the new ones included) resets to Forward's default; state them
	// all on every PUT, or use Patch for a partial change. BGPAndBMPFailureHandling
	// is FAIL_DEVICE, SWITCH_TO_CLI, ALWAYS_USE_CLI, ALWAYS_USE_BMP or
	// CONTINUE_WITHOUT_BGP_DATA; empty and nil send nothing.
	CLICredential2ID         string `json:"cliCredential2Id,omitempty"`
	CLICredential3ID         string `json:"cliCredential3Id,omitempty"`
	BGPAndBMPFailureHandling string `json:"bgpAndBmpFailureHandling,omitempty"`
	DisableIPv6Collection    *bool  `json:"disableIpv6Collection,omitempty"`
	FullCollectionLog        *bool  `json:"fullCollectionLog,omitempty"`
}

func (s *ClassicDevicesService) PutBatch(ctx context.Context, networkID string, devices []ClassicDeviceBatchItem) (*Response, error) {
	if len(devices) == 0 {
		return nil, nil
	}
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=putBatch", devices)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ClassicDevices.PutBatch")
	return s.client.Do(req, nil)
}

func (s *ClassicDevicesService) ListTestStatuses(ctx context.Context, networkID string) ([]SourceTestStatus, *Response, error) {
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[SourceTestStatus]{Keys: []string{"devices", "classicDevices", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?with=testResult&with=snmpCollectionStatus", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ListTestStatuses")
	response, err := s.client.doRequired(req, &result)
	for i := range result.Items {
		result.Items[i].SourceKind = "classic"
	}
	return result.Items, response, err
}

// ResolveGlobs says which of the network's classic devices of the given connection types each glob matches (POST
// ...classic-devices?action=resolveGlobs, ClassicDeviceController.resolveGlobs; VIEW_COLLECTION_SOURCES; a read, nothing changes).
// A custom command group's Devices are such globs, so this answers "which devices would this group's commands run on". The
// result maps each glob, as sent, to the matching device names; a glob that matches nothing may be missing from the map (Forward
// sends {} when no classic device of those types exists), so read it with a lookup. Globs match ignoring case. Globs and connTypes must be non-empty,
// and no glob blank.
func (s *ClassicDevicesService) ResolveGlobs(ctx context.Context, networkID string, globs, connTypes []string) (map[string][]string, *Response, error) {
	if len(globs) == 0 || len(connTypes) == 0 {
		return nil, nil, errors.New("forward: globs and connection types are required")
	}
	for _, glob := range globs {
		if strings.TrimSpace(glob) == "" {
			return nil, nil, errors.New("forward: a glob is blank")
		}
	}
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=resolveGlobs", map[string]any{"globs": globs, "connTypes": connTypes})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ResolveGlobs")
	var out struct {
		GlobToResolvedDevices map[string][]string `json:"globToResolvedDevices"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.GlobToResolvedDevices, resp, err
}

func (d *ClassicDevice) UnmarshalJSON(data []byte) error {
	type plain ClassicDevice
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = ClassicDevice(value)
	d.Raw = raw
	return nil
}

// List reads every classic device of the network in one request: GET
// /api/networks/{networkId}/classic-devices takes no offset or limit
// (ClassicDeviceController.getClassicDevices), so there is no paging to be
// unstable. For a subset by name use GetMany.
func (s *ClassicDevicesService) List(ctx context.Context, networkID string, with ...string) ([]ClassicDevice, *Response, error) {
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(with) != 0 {
		path += "?" + url.Values{"with": with}.Encode()
	}
	// TOLERATE EVERY ENVELOPE FORWARD HAS ACTUALLY RETURNED, not just the one
	// this route returned when it was written.
	//
	// A reader that knows one shape does not fail on another -- it reports an
	// EMPTY list. Skyforge prunes classic devices by diffing this list against
	// the topology, so an empty answer reads as "nothing to prune" and the
	// drift it is meant to remove survives silently. Measured on cs-lab
	// (network 3150) 2026-09-05: a renamed device left a stale record, and the
	// SNMP contract then failed every sync with "12/13 devices observed"
	// against a lab that was healthy.
	//
	// So this accepts a bare array, a single object, and the wrapped keys
	// Forward uses across its list routes -- the same set Endpoints.List
	// accepts, for the same reason.
	result := listResponse[ClassicDevice]{
		Keys:        []string{"devices", "classicDevices", "items", "data", "results"},
		AllowSingle: true,
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.List")
	resp, err := s.client.Do(req, &result)
	return result.Items, resp, err
}

func (s *ClassicDevicesService) Create(ctx context.Context, networkID string, input ClassicDeviceRequest) (*ClassicDevice, *Response, error) {
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	if err := validateClassicDevice(input); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	device := new(ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevicesService) Get(ctx context.Context, networkID, deviceName string, with ...string) (*ClassicDevice, *Response, error) {
	path, err := classicDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	if len(with) != 0 {
		path += "?" + url.Values{"with": with}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	device := new(ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevicesService) Put(ctx context.Context, networkID, deviceName string, input ClassicDeviceRequest) (*ClassicDevice, *Response, error) {
	path, err := classicDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	if err := validateClassicDevice(input); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, input)
	if err != nil {
		return nil, nil, err
	}
	device := new(ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

// ClassicDevicePatch changes part of a device. Every field is a pointer so that
// "not stated" and "stated as empty" stay distinguishable -- clearing a note and
// leaving it alone are different requests, and a plain string cannot say which
// was meant.
type ClassicDevicePatch struct {
	Host             *string `json:"host,omitempty"`
	Type             *string `json:"type,omitempty"`
	Port             *int32  `json:"port,omitempty"`
	CLICredentialID  *string `json:"cliCredentialId,omitempty"`
	HTTPCredentialID *string `json:"httpCredentialId,omitempty"`
	Collect          *bool   `json:"collect,omitempty"`
	Note             *string `json:"note,omitempty"`
	// CollectorID moves the device to another collector (ClassicDevicePatch binds
	// collectorId; hidden from the published spec, so Preview). nil leaves it alone.
	CollectorID *string `json:"collectorId,omitempty"`

	// CLICredential2ID is the PRIVILEGED_MODE (or Avi SHELL) credential and
	// CLICredential3ID the EXPERT_MODE one (Checkpoint).
	CLICredential2ID *string `json:"cliCredential2Id,omitempty"`
	CLICredential3ID *string `json:"cliCredential3Id,omitempty"`
	SNMPCredentialID *string `json:"snmpCredentialId,omitempty"`
	JumpServerID     *string `json:"jumpServerId,omitempty"`
	// Collection options. BGPTableType and BGPPeerType take effect only when
	// collection of BGP advertisements is on: set CollectBGPAdvertisements=true
	// in the same patch, or Forward drops them. BGPTableType: BOTH, ADJ_RIB_IN,
	// ADJ_RIB_OUT. BGPPeerType: BOTH, EBGP, IBGP. BGPAndBMPFailureHandling: FAIL_DEVICE,
	// SWITCH_TO_CLI, ALWAYS_USE_CLI, ALWAYS_USE_BMP, CONTINUE_WITHOUT_BGP_DATA.
	EnableSNMPCollection     *bool   `json:"enableSnmpCollection,omitempty"`
	CollectBGPAdvertisements *bool   `json:"collectBgpAdvertisements,omitempty"`
	BGPTableType             *string `json:"bgpTableType,omitempty"`
	BGPPeerType              *string `json:"bgpPeerType,omitempty"`
	BGPAndBMPFailureHandling *string `json:"bgpAndBmpFailureHandling,omitempty"`
	DisableIPv6Collection    *bool   `json:"disableIpv6Collection,omitempty"`
	FullCollectionLog        *bool   `json:"fullCollectionLog,omitempty"`

	// Clear names wire properties to send as an explicit JSON null, which
	// Forward reads as "clear it back to unset" (the collector falls back to the
	// network's default, an auto-associated credential is chosen again, an
	// option returns to its default). A pointer field can only omit, so this is
	// the only way to clear. Allowed names: cliCredentialId, cliCredential2Id,
	// cliCredential3Id, httpCredentialId, snmpCredentialId, jumpServerId,
	// collectorId, note, port, enableSnmpCollection, collectBgpAdvertisements,
	// bgpTableType, bgpPeerType, bgpAndBmpFailureHandling, disableIpv6Collection,
	// fullCollectionLog. Naming a property that the patch also sets is an error.
	Clear []string `json:"-"`
}

var classicDevicePatchClearable = map[string]bool{
	"cliCredentialId": true, "cliCredential2Id": true, "cliCredential3Id": true, "httpCredentialId": true,
	"snmpCredentialId": true, "jumpServerId": true, "collectorId": true, "note": true, "port": true,
	"enableSnmpCollection": true, "collectBgpAdvertisements": true, "bgpTableType": true, "bgpPeerType": true,
	"bgpAndBmpFailureHandling": true, "disableIpv6Collection": true, "fullCollectionLog": true,
}

// MarshalJSON writes the set properties and a JSON null for each name in Clear.
func (p ClassicDevicePatch) MarshalJSON() ([]byte, error) {
	type plain ClassicDevicePatch
	raw, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}
	if len(p.Clear) == 0 {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	cleared := map[string]bool{}
	for _, name := range p.Clear {
		if !classicDevicePatchClearable[name] {
			return nil, fmt.Errorf("forward: classic device property %q cannot be cleared with null", name)
		}
		if _, set := fields[name]; set && !cleared[name] {
			return nil, fmt.Errorf("forward: classic device patch both sets and clears %q", name)
		}
		cleared[name] = true
		fields[name] = json.RawMessage("null")
	}
	return json.Marshal(fields)
}

// Patch updates only the fields the patch states.
func (s *ClassicDevicesService) Patch(ctx context.Context, networkID, deviceName string, patch ClassicDevicePatch) (*ClassicDevice, *Response, error) {
	path, err := classicDevicePath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	device := new(ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevicesService) Delete(ctx context.Context, networkID, deviceName string) (*Response, error) {
	path, err := classicDevicePath(networkID, deviceName)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func classicDevicesPath(networkID string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/classic-devices", nil
}

func classicDevicePath(networkID, deviceName string) (string, error) {
	path, err := classicDevicesPath(networkID)
	if err != nil {
		return "", err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return path + "/" + url.PathEscape(deviceName), nil
}

func validateClassicDevice(input ClassicDeviceRequest) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Host) == "" {
		return errors.New("forward: classic device name and host are required")
	}
	return nil
}
