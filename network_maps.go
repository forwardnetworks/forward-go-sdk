package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// NetworkMapsService reads a network's saved network maps: the named sets of devices, with positions and display settings, that the
// Network Maps page draws. READ-ONLY: this service does not create, change or delete a map or a directory. Preview: appserver
// NetworkMapController, not the published OpenAPI. Every read needs the network permission VIEW_NETWORK_MAPS; a caller without it gets
// the typed 403 (ErrPermissionDenied), and the directory listing is also switched by the organization property
// NETWORK_MAP_DIRECTORIES (default on; off answers 403 with ErrFeatureGated, checked before the permission). A licence that lacks the operation answers ErrUnlicensedOperation instead of ErrPermissionDenied.
//
// A network with no maps is not an error: the listing is an empty (non-nil) slice, and the directory listing still holds the root
// directory "/" with no map ids.
type NetworkMapsService service

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

func (s *NetworkMapsService) mapPath(networkID, mapID string, rest ...string) (string, error) {
	if mapID = strings.TrimSpace(mapID); mapID == "" {
		return "", errors.New("forward: network map ID is required")
	}
	return s.client.networkSubPath(networkID, append([]string{"maps", url.PathEscape(mapID)}, rest...)...)
}

func (s *NetworkMapsService) get(ctx context.Context, path string, query url.Values, operation string, out any) (*Response, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// snapshotQuery builds the query of the snapshot-scoped reads. An empty snapshot ID sends no snapshotId, so the appserver applies its
// own default (the network's latest processed snapshot), as it does for the other snapshot-scoped reads.
func snapshotQuery(snapshotID string, extra ...string) url.Values {
	query := url.Values{}
	for i := 0; i+1 < len(extra); i += 2 {
		query.Set(extra[i], extra[i+1])
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	return query
}

// List returns the network's maps, summary rows only (GET .../maps; VIEW_NETWORK_MAPS). A network without maps gives an empty slice.
func (s *NetworkMapsService) List(ctx context.Context, networkID string) ([]NetworkMapInfo, *Response, error) {
	path, err := s.client.networkSubPath(networkID, "maps")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[NetworkMapInfo]{Keys: []string{"maps"}}
	resp, err := s.get(ctx, path, nil, "NetworkMaps.List", &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []NetworkMapInfo{}
	}
	return out.Items, resp, nil
}

// Get returns one stored map (GET .../maps/{mapId}; VIEW_NETWORK_MAPS). An unknown id is a 404.
func (s *NetworkMapsService) Get(ctx context.Context, networkID, mapID string) (*NetworkMap, *Response, error) {
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(NetworkMap)
	resp, err := s.get(ctx, path, nil, "NetworkMaps.Get", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// GetWithDirectory is Get with the directory path filled in (GET .../maps/{mapId}?with=directory). A map in no directory reads "/".
func (s *NetworkMapsService) GetWithDirectory(ctx context.Context, networkID, mapID string) (*NetworkMap, *Response, error) {
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(NetworkMap)
	resp, err := s.get(ctx, path, url.Values{"with": []string{"directory"}}, "NetworkMaps.GetWithDirectory", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Snapshot returns the map as it is in one snapshot, with the map's devices that snapshot has (GET .../maps/{mapId}?view=snapshot;
// VIEW_NETWORK_MAPS; the snapshot must have passed the CREATION stage). An empty snapshot ID leaves the choice to the appserver, which
// uses the network's latest snapshot that has passed CREATION (not a draft or fork; it may still be processing later stages), or
// answers 409 NO_QUALIFYING_SNAPSHOT when there is none. The Network-Snapshot response header names the one it picked.
func (s *NetworkMapsService) Snapshot(ctx context.Context, networkID, mapID, snapshotID string) (*NetworkMapSnapshot, *Response, error) {
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(NetworkMapSnapshot)
	resp, err := s.get(ctx, path, snapshotQuery(snapshotID, "view", "snapshot"), "NetworkMaps.Snapshot", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Structures returns the structure hierarchy of the map's devices in a snapshot (GET .../maps/{mapId}/structures; VIEW_NETWORK_MAPS;
// CREATION stage). Map devices the snapshot lacks are left out. An empty snapshot ID leaves the choice to the appserver, which picks as
// Snapshot describes.
func (s *NetworkMapsService) Structures(ctx context.Context, networkID, mapID, snapshotID string) ([]NetworkMapStructure, *Response, error) {
	path, err := s.mapPath(networkID, mapID, "structures")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[NetworkMapStructure]{Keys: []string{"structures"}}
	resp, err := s.get(ctx, path, snapshotQuery(snapshotID), "NetworkMaps.Structures", &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []NetworkMapStructure{}
	}
	return out.Items, resp, nil
}

// Directories returns the network's map directories (GET .../map-directories; VIEW_NETWORK_MAPS and the organization property
// NETWORK_MAP_DIRECTORIES, default on; off is ErrFeatureGated). The root directory "/" is always first, so a network without maps still has one directory.
func (s *NetworkMapsService) Directories(ctx context.Context, networkID string) (*NetworkMapDirectories, *Response, error) {
	path, err := s.client.networkSubPath(networkID, "map-directories")
	if err != nil {
		return nil, nil, err
	}
	out := new(NetworkMapDirectories)
	resp, err := s.get(ctx, path, nil, "NetworkMaps.Directories", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
