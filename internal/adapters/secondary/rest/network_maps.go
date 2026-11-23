package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type NetworkMaps service

func (s *NetworkMaps) mapPath(networkID, mapID string, rest ...string) (string, error) {
	if mapID = strings.TrimSpace(mapID); mapID == "" {
		return "", errors.New("forward: network map ID is required")
	}
	return s.client.networkSubPath(networkID, append([]string{"maps", url.PathEscape(mapID)}, rest...)...)
}

func (s *NetworkMaps) get(ctx context.Context, path string, query url.Values, operation string, out any) (*ports.Response, error) {
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
func (s *NetworkMaps) List(ctx context.Context, networkID string) ([]ports.NetworkMapInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.client.networkSubPath(networkID, "maps")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.NetworkMapInfo]{Keys: []string{"maps"}}
	resp, err := s.get(ctx, path, nil, "NetworkMaps.List", &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []ports.NetworkMapInfo{}
	}
	return out.Items, resp, nil
}

// Get returns one stored map (GET .../maps/{mapId}; VIEW_NETWORK_MAPS). An unknown id is a 404.
func (s *NetworkMaps) Get(ctx context.Context, networkID, mapID string) (*ports.NetworkMap, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.NetworkMap)
	resp, err := s.get(ctx, path, nil, "NetworkMaps.Get", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// GetWithDirectory is Get with the directory path filled in (GET .../maps/{mapId}?with=directory). A map in no directory reads "/".
func (s *NetworkMaps) GetWithDirectory(ctx context.Context, networkID, mapID string) (*ports.NetworkMap, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.NetworkMap)
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
func (s *NetworkMaps) Snapshot(ctx context.Context, networkID, mapID, snapshotID string) (*ports.NetworkMapSnapshot, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.mapPath(networkID, mapID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.NetworkMapSnapshot)
	resp, err := s.get(ctx, path, snapshotQuery(snapshotID, "view", "snapshot"), "NetworkMaps.Snapshot", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Structures returns the structure hierarchy of the map's devices in a snapshot (GET .../maps/{mapId}/structures; VIEW_NETWORK_MAPS;
// CREATION stage). Map devices the snapshot lacks are left out. An empty snapshot ID leaves the choice to the appserver, which picks as
// Snapshot describes.
func (s *NetworkMaps) Structures(ctx context.Context, networkID, mapID, snapshotID string) ([]ports.NetworkMapStructure, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.mapPath(networkID, mapID, "structures")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.NetworkMapStructure]{Keys: []string{"structures"}}
	resp, err := s.get(ctx, path, snapshotQuery(snapshotID), "NetworkMaps.Structures", &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Items == nil {
		out.Items = []ports.NetworkMapStructure{}
	}
	return out.Items, resp, nil
}

// Directories returns the network's map directories (GET .../map-directories; VIEW_NETWORK_MAPS and the organization property
// NETWORK_MAP_DIRECTORIES, default on; off is ErrFeatureGated). The root directory "/" is always first, so a network without maps still has one directory.
func (s *NetworkMaps) Directories(ctx context.Context, networkID string) (*ports.NetworkMapDirectories, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.client.networkSubPath(networkID, "map-directories")
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.NetworkMapDirectories)
	resp, err := s.get(ctx, path, nil, "NetworkMaps.Directories", out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
