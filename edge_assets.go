package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// EdgeAssetsService reads and manages a network's edge assets: hosts a user added that are not collected from, whose identifiers
// (addresses, MAC, cloud host id) are matched to the hosts of later snapshots, and the criticality that marks assets (edge assets and
// endpoints) as business critical. Threat-exposure analyses pick their critical assets from these. Preview and EXPERIMENTAL: appserver
// EdgeAssetController, not the published OpenAPI, and NOT in the Forward builds this SDK pins, so a deployment without it answers 404
// (ErrEndpointNotServed). This service does not create edge assets: Forward adds them from inferred hosts (POST .../inferred-hosts?
// action=addAsEdgeAsset, MANAGE_EDGE_ASSETS and EDGE_ASSETS_IN_SOURCES), which this SDK does not wrap. The operations here need the
// security licence facet.
type EdgeAssetsService service

// EdgeAsset is an edge asset with its network-level criticality and its state in the network's latest snapshot.
type EdgeAsset struct {
	Name string `json:"name"`
	// Include says the asset is matched to hosts in later snapshots.
	Include     bool     `json:"include"`
	IPs         []string `json:"ips,omitempty"`
	MACAddress  string   `json:"macAddress,omitempty"`
	CloudHostID string   `json:"cloudHostId,omitempty"`
	// Criticality is BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL; empty when none is set.
	Criticality string `json:"criticality,omitempty"`
	// EnrichmentSources name where extra information about the asset comes from (RAPID7, CLOUD, TENABLE_SC, TENABLE_VM).
	EnrichmentSources        []string   `json:"enrichmentSources,omitempty"`
	IncludedInLatestSnapshot bool       `json:"includedInLatestSnapshot"`
	CreatedByID              Identifier `json:"createdById,omitempty"`
	CreatedAt                string     `json:"createdAt,omitempty"`
	CreatedBy                string     `json:"createdBy,omitempty"`
	UpdatedByID              Identifier `json:"updatedById,omitempty"`
	UpdatedAt                string     `json:"updatedAt,omitempty"`
	UpdatedBy                string     `json:"updatedBy,omitempty"`
}

// EdgeAssetPatch changes an edge asset; a nil or empty field is left alone, and at least one is required.
type EdgeAssetPatch struct {
	// Include turns matching to later snapshots on or off.
	Include *bool `json:"include,omitempty"`
	// Name renames the asset.
	Name string `json:"name,omitempty"`
}

func (s *EdgeAssetsService) path(networkID string, rest ...string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/" + strings.Join(rest, "/"), nil
}

func (s *EdgeAssetsService) do(ctx context.Context, method, path string, body, out any, operation string) (*Response, error) {
	var req *http.Request
	var err error
	if body == nil {
		req, err = s.client.NewRequest(ctx, method, path, nil)
	} else {
		req, err = s.client.newJSONRequest(ctx, method, path, body)
	}
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	if out == nil {
		return s.client.Do(req, nil)
	}
	return s.client.doRequired(req, out)
}

func (s *EdgeAssetsService) assetPath(networkID, assetName string) (string, error) {
	if assetName = strings.TrimSpace(assetName); assetName == "" {
		return "", errors.New("forward: edge asset name is required")
	}
	return s.path(networkID, "edge-assets", url.PathEscape(assetName))
}

// List returns the network's edge assets (GET .../edge-assets; VIEW_COLLECTION_SOURCES). It is not gated on the organization property
// EDGE_ASSETS_IN_SOURCES, so assets added while it was on stay visible after it is turned off.
func (s *EdgeAssetsService) List(ctx context.Context, networkID string) ([]EdgeAsset, *Response, error) {
	path, err := s.path(networkID, "edge-assets")
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[EdgeAsset]{Keys: []string{"edgeAssets"}}
	resp, err := s.do(ctx, http.MethodGet, path, nil, &out, "EdgeAssets.List")
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

// Update changes an edge asset (PATCH .../edge-assets/{name}; 204; MANAGE_EDGE_ASSETS and the organization property
// EDGE_ASSETS_IN_SOURCES).
func (s *EdgeAssetsService) Update(ctx context.Context, networkID, assetName string, patch EdgeAssetPatch) (*Response, error) {
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
func (s *EdgeAssetsService) Delete(ctx context.Context, networkID, assetName string) (*Response, error) {
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

func (s *EdgeAssetsService) assignCriticality(ctx context.Context, networkID string, names []string, level *string, operation string) (*Response, error) {
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
func (s *EdgeAssetsService) SetCriticality(ctx context.Context, networkID string, names []string, level string) (*Response, error) {
	if !validCriticality(level) {
		return nil, fmt.Errorf("forward: criticality %q must be BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL", level)
	}
	return s.assignCriticality(ctx, networkID, names, &level, "EdgeAssets.SetCriticality")
}

// RemoveCriticality removes the criticality of the named assets (the same route with a null criticality; EDIT_ASSET_CRITICALITIES and
// EDGE_ASSETS_IN_SOURCES), which takes them out of the critical assets threat-exposure analyses select.
func (s *EdgeAssetsService) RemoveCriticality(ctx context.Context, networkID string, names []string) (*Response, error) {
	return s.assignCriticality(ctx, networkID, names, nil, "EdgeAssets.RemoveCriticality")
}
