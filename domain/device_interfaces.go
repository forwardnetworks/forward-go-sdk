package domain

import (
	"encoding/json"
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
