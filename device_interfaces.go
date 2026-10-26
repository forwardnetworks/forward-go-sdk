package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DeviceInterface is one interface of a modeled device, as Forward 26.10 publishes it (experimental). Name, Type and
// the common scalar and list properties are typed (not fileLines or anything type-specific); Raw is the interface exactly as Forward sent it, because the rest
// depends on Type (VLANs on ACCESS and TRUNK, member ports on aggregates, tunnel properties, ...) and an experimental
// contract may add more. Type is the enum value ("ACCESS", "L3", ...), not the display name the unpublished route used.
type DeviceInterface struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Aliases      []string `json:"aliases"`
	IsDown       bool     `json:"isDown"`
	IsLoopback   bool     `json:"isLoopback"`
	MTU          *int32   `json:"mtu,omitempty"`
	MACAddress   string   `json:"macAddress,omitempty"`
	InACLNames   []string `json:"inAclNames"`
	OutACLNames  []string `json:"outAclNames"`
	SecurityZone string   `json:"securityZone,omitempty"`
	// Neighbors is present only when requested with WithNeighbors.
	Neighbors []DeviceInterfaceNeighbor `json:"neighbors,omitempty"`
	Raw       json.RawMessage           `json:"-"`
}

// DeviceInterfaceNeighbor is a device and port on the far end of an interface's outgoing link.
type DeviceInterfaceNeighbor struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}

// UnmarshalJSON keeps the raw interface next to the typed fields.
func (i *DeviceInterface) UnmarshalJSON(data []byte) error {
	type plain DeviceInterface
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*i = DeviceInterface(p)
	i.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DeviceInterfaceOptions narrows a device interface read. An empty SnapshotID lets Forward use the latest processed
// snapshot. WithNeighbors adds each interface's neighbors (?with=neighbors); without it none are returned.
type DeviceInterfaceOptions struct {
	SnapshotID    string
	WithNeighbors bool
}

func deviceInterfacesPath(networkID, deviceName string) (string, error) {
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	base, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return base + "/devices/" + url.PathEscape(deviceName) + "/interfaces", nil
}

func (o DeviceInterfaceOptions) query() string {
	q := url.Values{}
	setString(q, "snapshotId", o.SnapshotID)
	if o.WithNeighbors {
		q.Add("with", "neighbors")
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// Interfaces lists a device's interfaces, sorted by name. GET /api/networks/{id}/devices/{device}/interfaces
// (DeviceController, published in 26.10 as experimental; 409 while Forward processes the snapshot, see ErrSnapshotNotProcessed; the same path served an unpublished route with a different
// shape: type was a display name, VLANs were strings, neighbors were "device port" strings and came without a request).
func (s *DevicesService) Interfaces(ctx context.Context, networkID, deviceName string, options DeviceInterfaceOptions) ([]DeviceInterface, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := deviceInterfacesPath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+options.query(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Interfaces")
	var out struct {
		Interfaces []DeviceInterface `json:"interfaces"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.Interfaces, resp, err
}

// Interface returns one of a device's interfaces by name or alias (case-sensitive; a "/" in it is percent-encoded).
// GET /api/networks/{id}/devices/{device}/interfaces/{interface}; experimental, see Interfaces.
func (s *DevicesService) Interface(ctx context.Context, networkID, deviceName, interfaceName string, options DeviceInterfaceOptions) (*DeviceInterface, *Response, error) {
	if interfaceName = strings.TrimSpace(interfaceName); interfaceName == "" {
		return nil, nil, errors.New("forward: interface name is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := deviceInterfacesPath(networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/"+url.PathEscape(interfaceName)+options.query(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Interface")
	out := new(DeviceInterface)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// DeviceDiffEntry is one device added, removed or modified between two snapshots. Before is absent for ADDED and After
// for REMOVED. ChangedProperties names the Device properties that differ and is set only for MODIFIED.
type DeviceDiffEntry struct {
	DeviceName        string   `json:"deviceName"`
	ChangeType        string   `json:"changeType"` // ADDED, REMOVED or MODIFIED
	ChangedProperties []string `json:"changedProperties,omitempty"`
	Before            *Device  `json:"before,omitempty"`
	After             *Device  `json:"after,omitempty"`
}

// DeviceDiffs is the device-level difference between two snapshots. TotalDiffs counts every changed device matching
// the filters; Diffs is cut to Limit.
type DeviceDiffs struct {
	BeforeSnapshotCreatedAt string            `json:"beforeSnapshotCreatedAt"`
	AfterSnapshotCreatedAt  string            `json:"afterSnapshotCreatedAt"`
	Diffs                   []DeviceDiffEntry `json:"diffs"`
	TotalDiffs              int               `json:"totalDiffs"`
}

// DeviceDiffOptions filters a device diff. Name is a case-insensitive glob on the device name; ChangeTypes limits to
// ADDED, REMOVED and/or MODIFIED; With adds "tags" and/or "locationId" to before and after (and to the comparison, so
// it can add MODIFIED entries); Limit caps the entries returned, with TotalDiffs still counting all.
type DeviceDiffOptions struct {
	Name        string
	ChangeTypes []string
	With        []string
	Limit       *int32
}

// Diff reports each device added, removed or modified between the before and after snapshots of a network; unchanged
// devices are omitted and devices match by name ignoring case. GET /api/networks/{id}/device-diffs/{before}/{after}
// (DeviceController, published in 26.10 as experimental). Answers 409 (ErrSnapshotNotProcessed) while Forward is processing either snapshot.
func (s *DevicesService) Diff(ctx context.Context, networkID, beforeSnapshotID, afterSnapshotID string, options DeviceDiffOptions) (*DeviceDiffs, *Response, error) {
	beforeSnapshotID, afterSnapshotID = strings.TrimSpace(beforeSnapshotID), strings.TrimSpace(afterSnapshotID)
	if beforeSnapshotID == "" || afterSnapshotID == "" {
		return nil, nil, errors.New("forward: before and after snapshot IDs are required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	base, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	setString(q, "name", options.Name)
	if len(options.ChangeTypes) > 0 {
		q.Set("changeType", strings.Join(options.ChangeTypes, ","))
	}
	if len(options.With) > 0 {
		q.Set("with", strings.Join(options.With, ","))
	}
	if options.Limit != nil {
		q.Set("limit", strconv.FormatInt(int64(*options.Limit), 10))
	}
	path := base + "/device-diffs/" + url.PathEscape(beforeSnapshotID) + "/" + url.PathEscape(afterSnapshotID)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Devices.Diff")
	out := new(DeviceDiffs)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}
