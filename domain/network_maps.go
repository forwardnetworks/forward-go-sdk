package domain

import (
	"encoding/json"
)

// NetworkMapInfo is a map's summary row (NetworkMapInfo). The attribution names are filled by the appserver when the user still exists.
type NetworkMapInfo struct {
	ID   Identifier `json:"id"`
	Name string     `json:"name"`
	// Pinned is nil when it was never set.
	Pinned      *bool      `json:"pinned,omitempty"`
	CreatedByID Identifier `json:"createdById,omitempty"`
	CreatedAt   string     `json:"createdAt,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	UpdatedByID Identifier `json:"updatedById,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
}

// NetworkMapDevice is a device placed on a map: its lower-case NAME and its position. Whether the device exists in a snapshot, and what it
// is there, is Snapshot's answer.
type NetworkMapDevice struct {
	Name string   `json:"name"`
	X    *float64 `json:"x,omitempty"`
	Y    *float64 `json:"y,omitempty"`
	// LoopbackDirection is the angle in (-180, 180] that loopback links leave the device at.
	LoopbackDirection *float64 `json:"loopbackDirection,omitempty"`
}

// NetworkMap is a stored map. The display settings are left as the raw JSON Forward sent (annotations, defaults, per-device and
// per-interface settings, BGP settings, link appearance rules, hidden links): they are drawing detail whose shapes are Forward's
// TopologyAnnotation, NetworkMapDeviceSettings and the like, and a field this SDK does not model must not be lost.
type NetworkMap struct {
	NetworkMapInfo
	// Devices is the map's coverage: the devices placed on it, sorted by name, whether or not a given snapshot has them.
	Devices             []NetworkMapDevice `json:"devices,omitempty"`
	Annotations         json.RawMessage    `json:"annotations,omitempty"`
	DeviceDefaults      json.RawMessage    `json:"deviceDefaults,omitempty"`
	InterfaceDefaults   json.RawMessage    `json:"interfaceDefaults,omitempty"`
	DeviceSettings      json.RawMessage    `json:"deviceSettings,omitempty"`
	InterfaceSettings   json.RawMessage    `json:"interfaceSettings,omitempty"`
	BGPSettings         json.RawMessage    `json:"bgpSettings,omitempty"`
	LinkAppearanceRules json.RawMessage    `json:"linkAppearanceRules,omitempty"`
	HiddenLinks         json.RawMessage    `json:"hiddenLinks,omitempty"`
	// Directory is the directory path holding the map, filled only by GetWithDirectory ("/" for the root).
	Directory string `json:"directory,omitempty"`
}

// NetworkMapSnapshotDevice is a map device as it is in one snapshot: the snapshot's own device record (name case as the snapshot has
// it) with its map position.
type NetworkMapSnapshotDevice struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName,omitempty"`
	SourceName      string   `json:"sourceName,omitempty"`
	Type            string   `json:"type,omitempty"`
	Vendor          string   `json:"vendor,omitempty"`
	Model           string   `json:"model,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	OSVersion       string   `json:"osVersion,omitempty"`
	ManagementIPs   []string `json:"managementIps,omitempty"`
	CollectionError string   `json:"collectionError,omitempty"`
	ProcessingError string   `json:"processingError,omitempty"`
	DuplicateOf     string   `json:"duplicateOf,omitempty"`
	CollectedAt     string   `json:"collectedAt,omitempty"`
	CollectionIP    string   `json:"collectionIp,omitempty"`
	// Tags and LocationID are the device's tags and location id in the snapshot; empty when it has none.
	Tags       []string `json:"tags,omitempty"`
	LocationID string   `json:"locationId,omitempty"`
	X          *float64 `json:"x,omitempty"`
	Y          *float64 `json:"y,omitempty"`
}

// NetworkMapSnapshot is a map rendered for one snapshot (NetworkMapSnapshot). Devices holds only the map's devices that the snapshot
// has, so comparing it with NetworkMap.Devices shows which placed devices the snapshot lacks. Interfaces, Links, BGPLinks and the
// settings are left as raw JSON.
type NetworkMapSnapshot struct {
	NetworkMapInfo
	Devices             []NetworkMapSnapshotDevice `json:"devices,omitempty"`
	Interfaces          json.RawMessage            `json:"interfaces,omitempty"`
	Links               json.RawMessage            `json:"links,omitempty"`
	Annotations         json.RawMessage            `json:"annotations,omitempty"`
	DeviceDefaults      json.RawMessage            `json:"deviceDefaults,omitempty"`
	InterfaceDefaults   json.RawMessage            `json:"interfaceDefaults,omitempty"`
	DeviceSettings      json.RawMessage            `json:"deviceSettings,omitempty"`
	InterfaceSettings   json.RawMessage            `json:"interfaceSettings,omitempty"`
	BGPSettings         json.RawMessage            `json:"bgpSettings,omitempty"`
	LinkAppearanceRules json.RawMessage            `json:"linkAppearanceRules,omitempty"`
	HiddenLinks         json.RawMessage            `json:"hiddenLinks,omitempty"`
	// BGPLinks is empty when the organization property NETWORK_MAPS_BGP_LAYER is off.
	BGPLinks json.RawMessage `json:"bgpLinks,omitempty"`
}

// DeviceNames lists the names of the map's devices that the snapshot has.
func (m *NetworkMapSnapshot) DeviceNames() []string {
	if m == nil {
		return nil
	}
	names := make([]string, 0, len(m.Devices))
	for _, device := range m.Devices {
		names = append(names, device.Name)
	}
	return names
}

// NetworkMapStructure is a node of the structure hierarchy Forward infers for a map's devices in a snapshot (StructureNode): a row of
// devices, a spine-leaf group, a single-point-of-failure group and so on. Type is Forward's name for the kind.
type NetworkMapStructure struct {
	Type       string                `json:"type,omitempty"`
	Devices    []string              `json:"devices,omitempty"`
	Structures []NetworkMapStructure `json:"structures,omitempty"`
}

// NetworkMapDirectory is one directory of maps. The root directory "/" lists every map not placed in another one and has no attribution.
type NetworkMapDirectory struct {
	Path        string       `json:"path"`
	MapIDs      []Identifier `json:"mapIds"`
	CreatedByID Identifier   `json:"createdById,omitempty"`
	CreatedAt   string       `json:"createdAt,omitempty"`
	CreatedBy   string       `json:"createdBy,omitempty"`
	UpdatedByID Identifier   `json:"updatedById,omitempty"`
	UpdatedAt   string       `json:"updatedAt,omitempty"`
	UpdatedBy   string       `json:"updatedBy,omitempty"`
}

// NetworkMapDirectories is the flattened directory tree, root first and the rest depth-first.
type NetworkMapDirectories struct {
	Directories []NetworkMapDirectory `json:"directories"`
}
