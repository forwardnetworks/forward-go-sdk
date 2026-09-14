package forward

import (
	"context"
	"encoding/json"
	"errors"
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
	Fields           map[string]any
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
	// the three states have to stay distinguishable. Absent means "leave the
	// device's current toggle alone", which is what every existing caller
	// wants and gets for free by not setting the field. A non-nil false is an
	// explicit request to onboard the device and NOT collect it.
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
