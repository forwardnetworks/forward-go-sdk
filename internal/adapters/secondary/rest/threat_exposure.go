package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// ThreatExposureService reads and manages threat-exposure analyses: which of a network's critical assets an attacker could reach through
// chosen exposure points (interface aliases such as an internet-facing edge), and how severely. A definition names the exposure points
// with a severity each and optionally narrows the critical assets by location, criticality and subnet; evaluating it against a snapshot
// starts analysis jobs, and the analysis views read their results. Preview and EXPERIMENTAL: the feature is behind the organization
// property THREAT_EXPOSURE (default off, not configurable by an org admin), the routes are appserver ThreatExposureController, and
// they are NOT in the Forward builds this SDK pins, so a deployment without them answers 404 (ErrEndpointNotServed). Reads need
// VIEW_THREAT_EXPOSURE (listing, getting, status and the views are not gated on the property); creating, changing and deleting
// definitions EDIT_THREAT_EXPOSURE; Evaluate EVALUATE_THREAT_EXPOSURE. These operations need the security licence facet.
type ThreatExposure service

func validCriticality(level string) bool {
	return level == ports.CriticalityBusinessCritical || level == ports.CriticalityCriticalInfra || level == ports.CriticalityCrownJewel
}

func (s *ThreatExposure) definitionsPath(networkID string) (string, error) {
	return s.client.networkSubPath(networkID, "threat-exposure-definitions")
}

func (s *ThreatExposure) definitionPath(networkID, definitionID string) (string, error) {
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return "", err
	}
	if definitionID = strings.TrimSpace(definitionID); definitionID == "" {
		return "", errors.New("forward: definition ID is required")
	}
	return path + "/" + url.PathEscape(definitionID), nil
}

func (s *ThreatExposure) analysisPath(networkID, definitionID string) (string, error) {
	if definitionID = strings.TrimSpace(definitionID); definitionID == "" {
		return "", errors.New("forward: definition ID is required")
	}
	return s.client.networkSubPath(networkID, "threat-exposure-analyses", url.PathEscape(definitionID))
}

func (s *ThreatExposure) do(ctx context.Context, method, path string, body, out any, operation string) (*ports.Response, error) {
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

func requireSnapshot(snapshotID string) (string, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required (this SDK always sends one: Forward refuses a missing one on evaluate and status, and defaults it to the latest snapshot, which may not have been analyzed, elsewhere)")
	}
	return snapshotID, nil
}

// ListDefinitions returns the network's definitions (GET .../threat-exposure-definitions; VIEW_THREAT_EXPOSURE).
func (s *ThreatExposure) ListDefinitions(ctx context.Context, networkID string) ([]ports.ThreatExposureDefinitionInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ports.ThreatExposureDefinitionInfo]{Keys: []string{"definitions"}}
	resp, err := s.do(ctx, http.MethodGet, path, nil, &out, "ThreatExposure.ListDefinitions")
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

// GetDefinition returns one definition (GET .../threat-exposure-definitions/{id}; 404 when it does not exist).
func (s *ThreatExposure) GetDefinition(ctx context.Context, networkID, definitionID string) (*ports.ThreatExposureDefinition, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.definitionPath(networkID, definitionID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ThreatExposureDefinition)
	resp, err := s.do(ctx, http.MethodGet, path, nil, out, "ThreatExposure.GetDefinition")
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CreateDefinition saves a definition and returns it with its id (POST .../threat-exposure-definitions; 201; EDIT_THREAT_EXPOSURE and
// the organization property THREAT_EXPOSURE). Forward refuses an invalid definition with 400 and the reason.
func (s *ThreatExposure) CreateDefinition(ctx context.Context, networkID string, definition ports.NewThreatExposureDefinition) (*ports.ThreatExposureDefinition, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := definition.Validate(); err != nil {
		return nil, nil, err
	}
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.ThreatExposureDefinition)
	resp, err := s.do(ctx, http.MethodPost, path, definition, out, "ThreatExposure.CreateDefinition")
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// UpdateDefinition changes a definition (PATCH .../threat-exposure-definitions/{id}; 204; EDIT_THREAT_EXPOSURE and THREAT_EXPOSURE).
func (s *ThreatExposure) UpdateDefinition(ctx context.Context, networkID, definitionID string, patch ports.ThreatExposureDefinitionPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	path, err := s.definitionPath(networkID, definitionID)
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodPatch, path, patch, nil, "ThreatExposure.UpdateDefinition")
}

// DeleteDefinition removes a definition (DELETE .../threat-exposure-definitions/{id}; 204; 404 when it does not exist; EDIT_THREAT_EXPOSURE;
// not gated on the organization property). It cannot be undone.
func (s *ThreatExposure) DeleteDefinition(ctx context.Context, networkID, definitionID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.definitionPath(networkID, definitionID)
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodDelete, path, nil, nil, "ThreatExposure.DeleteDefinition")
}

// ResolveDefinition previews what a definition (saved or not) selects on a snapshot: the exposure points and critical assets it
// resolves to, as Forward sent it (POST .../threat-exposure-definitions?action=resolve&view=summary&snapshotId=; VIEW_THREAT_EXPOSURE and
// THREAT_EXPOSURE; the snapshot must be processed to the reachability stage). The body is not modelled.
func (s *ThreatExposure) ResolveDefinition(ctx context.Context, networkID, snapshotID string, definition ports.NewThreatExposureDefinition) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := definition.Validate(); err != nil {
		return nil, nil, err
	}
	snapshotID, err := requireSnapshot(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"action": []string{"resolve"}, "view": []string{"summary"}, "snapshotId": []string{snapshotID}}.Encode()
	var out json.RawMessage
	resp, err := s.do(ctx, http.MethodPost, path, definition, &out, "ThreatExposure.ResolveDefinition")
	return out, resp, err
}

// Evaluate starts the analysis of a definition on a snapshot (POST .../threat-exposure-analyses/{id}?action=evaluate&snapshotId=; 202,
// no body; EVALUATE_THREAT_EXPOSURE and THREAT_EXPOSURE; the snapshot must be processed to the reachability stage). It starts jobs and
// returns at once (409 when the definition is already being analyzed against that snapshot): poll Status until it reports completion, then read the views. The snapshot is required.
func (s *ThreatExposure) Evaluate(ctx context.Context, networkID, definitionID, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	snapshotID, err := requireSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	path, err := s.analysisPath(networkID, definitionID)
	if err != nil {
		return nil, err
	}
	path += "?" + url.Values{"action": []string{"evaluate"}, "snapshotId": []string{snapshotID}}.Encode()
	return s.do(ctx, http.MethodPost, path, nil, nil, "ThreatExposure.Evaluate")
}

// Status returns the progress and state of the analysis of a definition on a snapshot, as Forward sent it (GET
// .../threat-exposure-analyses/{id}?view=status&snapshotId=; VIEW_THREAT_EXPOSURE). The snapshot is required: the latest may not have been
// analyzed.
func (s *ThreatExposure) Status(ctx context.Context, networkID, definitionID, snapshotID string) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.view(ctx, http.MethodGet, networkID, definitionID, snapshotID, "status", "", nil, "ThreatExposure.Status")
}

// CriticalAssetsSummary returns the critical assets the analysis found exposed, one page at a time, as Forward sent it (POST
// .../threat-exposure-analyses/{id}?view=criticalAssetsExposureSummary&snapshotId=; VIEW_THREAT_EXPOSURE). Rows come in a fixed order.
func (s *ThreatExposure) CriticalAssetsSummary(ctx context.Context, networkID, definitionID, snapshotID string, paging ports.ThreatExposurePaging) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	body, err := paging.Body()
	if err != nil {
		return nil, nil, err
	}
	return s.view(ctx, http.MethodPost, networkID, definitionID, snapshotID, "criticalAssetsExposureSummary", "", body, "ThreatExposure.CriticalAssetsSummary")
}

// DirectExposureDetails returns how one critical asset is directly exposed, as Forward sent it (GET
// .../threat-exposure-analyses/{id}?view=criticalAssetDirectExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE). assetId is the global host id
// the summary gives the asset.
func (s *ThreatExposure) DirectExposureDetails(ctx context.Context, networkID, definitionID, snapshotID string, assetID int64) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if assetID < 0 {
		return nil, nil, errors.New("forward: asset ID must not be negative")
	}
	return s.view(ctx, http.MethodGet, networkID, definitionID, snapshotID, "criticalAssetDirectExposureDetails", fmt.Sprint(assetID), nil, "ThreatExposure.DirectExposureDetails")
}

// TransitiveExposureDetails returns how one critical asset is exposed through other hosts, one page at a time, as Forward sent it (POST
// .../threat-exposure-analyses/{id}?view=criticalAssetTransitiveExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE).
func (s *ThreatExposure) TransitiveExposureDetails(ctx context.Context, networkID, definitionID, snapshotID string, assetID int64, paging ports.ThreatExposurePaging) (json.RawMessage, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	body, err := paging.Body()
	if err != nil {
		return nil, nil, err
	}
	if assetID < 0 {
		return nil, nil, errors.New("forward: asset ID must not be negative")
	}
	return s.view(ctx, http.MethodPost, networkID, definitionID, snapshotID, "criticalAssetTransitiveExposureDetails", fmt.Sprint(assetID), body, "ThreatExposure.TransitiveExposureDetails")
}

func (s *ThreatExposure) view(ctx context.Context, method, networkID, definitionID, snapshotID, view, assetID string, body any, operation string) (json.RawMessage, *ports.Response, error) {
	snapshotID, err := requireSnapshot(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	path, err := s.analysisPath(networkID, definitionID)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": []string{view}, "snapshotId": []string{snapshotID}}
	if assetID != "" {
		query.Set("assetId", assetID)
	}
	var out json.RawMessage
	resp, err := s.do(ctx, method, path+"?"+query.Encode(), body, &out, operation)
	return out, resp, err
}
