package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// EdgeAssets reads and manages a network's edge assets: hosts a user added that are not collected from, whose identifiers
// (addresses, MAC, cloud host id) are matched to the hosts of later snapshots, and the criticality that marks assets (edge assets and
// endpoints) as business critical. Threat-exposure analyses pick their critical assets from these. Preview and EXPERIMENTAL: appserver
// EdgeAssetController and DeviceController, not the published OpenAPI; a deployment without them answers 404 (ErrEndpointNotServed).
// Forward adds edge assets from a snapshot's inferred hosts (AddInferredHosts). The operations here need the security licence facet.
type EdgeAssets service

func (s *EdgeAssets) path(networkID string, rest ...string) (string, error) {
	return s.client.networkSubPath(networkID, rest...)
}

func (s *EdgeAssets) do(ctx context.Context, method, path string, body, out any, operation string) (*ports.Response, error) {
	req, err := s.client.bodyRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	if out == nil {
		return s.client.Do(req, nil)
	}
	return s.client.doRequired(req, out)
}

// maxInferredHostsPerCall is Forward's MAX_INFERRED_HOSTS_LIMIT, the most ids one addAsEdgeAsset call may carry.
const maxInferredHostsPerCall = 1000

// AddInferredHosts adds the snapshot's inferred hosts with the given global host IDs as edge assets; see the port for the
// permissions, the failure cases and why it is not idempotent.
func (s *EdgeAssets) AddInferredHosts(ctx context.Context, networkID, snapshotID string, globalHostIDs []int64) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: a snapshot ID is required to add inferred hosts as edge assets")
	}
	if len(globalHostIDs) == 0 {
		return nil, errors.New("forward: at least one global host ID is required to add inferred hosts as edge assets")
	}
	if len(globalHostIDs) > maxInferredHostsPerCall {
		return nil, fmt.Errorf("forward: %d global host IDs is more than the %d one call may add", len(globalHostIDs), maxInferredHostsPerCall)
	}
	path, err := s.path(networkID, "inferred-hosts?action=addAsEdgeAsset&snapshotId="+url.QueryEscape(snapshotID))
	if err != nil {
		return nil, err
	}
	body := struct {
		GlobalHostIDs []int64 `json:"globalHostIds"`
	}{GlobalHostIDs: globalHostIDs}
	return s.do(ctx, http.MethodPost, path, body, nil, "EdgeAssets.AddInferredHosts")
}

func (s *EdgeAssets) assetPath(networkID, assetName string) (string, error) {
	if assetName = strings.TrimSpace(assetName); assetName == "" {
		return "", errors.New("forward: edge asset name is required")
	}
	return s.path(networkID, "edge-assets", url.PathEscape(assetName))
}

// List returns the network's edge assets (GET .../edge-assets; VIEW_COLLECTION_SOURCES). It is not gated on the organization property
// EDGE_ASSETS_IN_SOURCES, so assets added while it was on stay visible after it is turned off.
func (s *EdgeAssets) List(ctx context.Context, networkID string) ([]ports.EdgeAsset, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.path(networkID, "edge-assets")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.EdgeAsset]{Keys: []string{"edgeAssets"}}
	resp, err := s.do(ctx, http.MethodGet, path, nil, &out, "EdgeAssets.List")
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

// Update changes an edge asset (PATCH .../edge-assets/{name}; 204; MANAGE_EDGE_ASSETS and the organization property
// EDGE_ASSETS_IN_SOURCES).
func (s *EdgeAssets) Update(ctx context.Context, networkID, assetName string, patch ports.EdgeAssetPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if patch.Include == nil && strings.TrimSpace(patch.Name) == "" {
		return nil, errors.New("forward: an edge asset patch needs include or name")
	}
	patch.Name = strings.TrimSpace(patch.Name)
	path, err := s.assetPath(networkID, assetName)
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodPatch, path, patch, nil, "EdgeAssets.Update")
}

// Delete removes an edge asset AND its criticality (DELETE .../edge-assets/{name}; 204; MANAGE_EDGE_ASSETS; not gated on the
// organization property, so it works after the feature is turned off). It cannot be undone.
func (s *EdgeAssets) Delete(ctx context.Context, networkID, assetName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.assetPath(networkID, assetName)
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodDelete, path, nil, nil, "EdgeAssets.Delete")
}

type criticalityAssignment struct {
	Names       []string `json:"names"`
	Criticality *string  `json:"criticality"`
}

func (s *EdgeAssets) assignCriticality(ctx context.Context, networkID string, names []string, level *string, operation string) (*ports.Response, error) {
	var cleaned []string
	for _, name := range names {
		if name = strings.TrimSpace(name); name != "" {
			cleaned = append(cleaned, name)
		}
	}
	if len(cleaned) == 0 {
		return nil, errors.New("forward: at least one asset name is required")
	}
	path, err := s.path(networkID, "assets?action=modifyCriticality")
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodPatch, path, criticalityAssignment{Names: cleaned, Criticality: level}, nil, operation)
}

// SetCriticality assigns one criticality to the named assets, edge assets and endpoints alike (PATCH .../assets?action=modifyCriticality;
// 204; EDIT_ASSET_CRITICALITIES and EDGE_ASSETS_IN_SOURCES). level is BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL.
func (s *EdgeAssets) SetCriticality(ctx context.Context, networkID string, names []string, level string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if !validCriticality(level) {
		return nil, fmt.Errorf("forward: criticality %q must be BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL", level)
	}
	return s.assignCriticality(ctx, networkID, names, &level, "EdgeAssets.SetCriticality")
}

// RemoveCriticality removes the criticality of the named assets (the same route with a null criticality; EDIT_ASSET_CRITICALITIES and
// EDGE_ASSETS_IN_SOURCES), which takes them out of the critical assets threat-exposure analyses select.
func (s *EdgeAssets) RemoveCriticality(ctx context.Context, networkID string, names []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.assignCriticality(ctx, networkID, names, nil, "EdgeAssets.RemoveCriticality")
}
