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

// ThreatExposureService reads and manages threat-exposure analyses: which of a network's critical assets an attacker could reach through
// chosen exposure points (interface aliases such as an internet-facing edge), and how severely. A definition names the exposure points
// with a severity each and optionally narrows the critical assets by location, criticality and subnet; evaluating it against a snapshot
// starts analysis jobs, and the analysis views read their results. Preview and EXPERIMENTAL: the feature is behind the organization
// property THREAT_EXPOSURE (default off, not configurable by an org admin), the routes are appserver ThreatExposureController, and
// they are NOT in the Forward builds this SDK pins, so a deployment without them answers 404 (ErrEndpointNotServed). Reads need
// VIEW_THREAT_EXPOSURE (listing, getting, status and the views are not gated on the property); creating, changing and deleting
// definitions EDIT_THREAT_EXPOSURE; Evaluate EVALUATE_THREAT_EXPOSURE. These operations need the security licence facet.
type ThreatExposureService service

// Exposure levels and criticality levels, as Forward names them.
const (
	ExposureCritical = "CRITICAL"
	ExposureSevere   = "SEVERE"
	ExposureHigh     = "HIGH"
	ExposureModerate = "MODERATE"

	CriticalityBusinessCritical = "BUSINESS_CRITICAL"
	CriticalityCriticalInfra    = "CRITICAL_INFRA"
	CriticalityCrownJewel       = "CROWN_JEWEL"
)

// MaxExposurePoints is the most exposure points one definition can have.
const MaxExposurePoints = 4

// ExposurePointAndLevel is an interface alias acting as an exposure point, with the severity of exposure through it.
type ExposurePointAndLevel struct {
	ExposurePoint string `json:"exposurePoint"`
	Level         string `json:"level"`
}

// ThreatExposureDefinition is a saved definition. An empty Locations, Criticalities or Subnets selects all critical assets.
type ThreatExposureDefinition struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	ExposurePoints     []ExposurePointAndLevel `json:"exposurePoints"`
	Locations          []string                `json:"locations,omitempty"`
	Criticalities      []string                `json:"criticalities,omitempty"`
	Subnets            []string                `json:"subnets,omitempty"`
	MaxDurationSeconds int64                   `json:"maxDurationSeconds"`
	CreatedByID        Identifier              `json:"createdById,omitempty"`
	CreatedAt          string                  `json:"createdAt,omitempty"`
	CreatedBy          string                  `json:"createdBy,omitempty"`
	UpdatedByID        Identifier              `json:"updatedById,omitempty"`
	UpdatedAt          string                  `json:"updatedAt,omitempty"`
	UpdatedBy          string                  `json:"updatedBy,omitempty"`
}

// ThreatExposureDefinitionInfo is a definition with the latest snapshot analyzed against it. Forward does not fill
// LatestAnalyzedSnapshotID in yet (FWD-60477), so expect it empty.
type ThreatExposureDefinitionInfo struct {
	Definition               ThreatExposureDefinition `json:"definition"`
	LatestAnalyzedSnapshotID Identifier               `json:"latestAnalyzedSnapshotId,omitempty"`
}

// NewThreatExposureDefinition is the body that creates (or, to resolve, previews) a definition. ExposurePoints holds 1 to
// MaxExposurePoints unique entries; MaxDurationSeconds is the positive time limit of one analysis and is required.
type NewThreatExposureDefinition struct {
	Name               string                  `json:"name"`
	ExposurePoints     []ExposurePointAndLevel `json:"exposurePoints"`
	Locations          []string                `json:"locations,omitempty"`
	Criticalities      []string                `json:"criticalities,omitempty"`
	Subnets            []string                `json:"subnets,omitempty"`
	MaxDurationSeconds int64                   `json:"maxDurationSeconds"`
}

func validCriticality(level string) bool {
	return level == CriticalityBusinessCritical || level == CriticalityCriticalInfra || level == CriticalityCrownJewel
}

func validateExposurePoints(points []ExposurePointAndLevel) error {
	if len(points) == 0 || len(points) > MaxExposurePoints {
		return fmt.Errorf("forward: a definition needs 1 to %d exposure points", MaxExposurePoints)
	}
	seen := map[string]bool{}
	for _, p := range points {
		if strings.TrimSpace(p.ExposurePoint) == "" {
			return errors.New("forward: an exposure point name must not be blank")
		}
		if seen[p.ExposurePoint] {
			return fmt.Errorf("forward: duplicate exposure point %q", p.ExposurePoint)
		}
		seen[p.ExposurePoint] = true
		switch p.Level {
		case ExposureCritical, ExposureSevere, ExposureHigh, ExposureModerate:
		default:
			return fmt.Errorf("forward: exposure level %q must be CRITICAL, SEVERE, HIGH or MODERATE", p.Level)
		}
	}
	return nil
}

func validateCriticalities(levels []string) error {
	for _, level := range levels {
		if !validCriticality(level) {
			return fmt.Errorf("forward: criticality %q must be BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL", level)
		}
	}
	return nil
}

// Validate refuses a definition Forward would, before any request.
func (d NewThreatExposureDefinition) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("forward: a definition name is required")
	}
	if d.MaxDurationSeconds <= 0 {
		return errors.New("forward: maxDurationSeconds is required and must be positive")
	}
	if err := validateExposurePoints(d.ExposurePoints); err != nil {
		return err
	}
	return validateCriticalities(d.Criticalities)
}

// ThreatExposureDefinitionPatch changes a definition. A nil field is left alone; a non-nil EMPTY Locations, Criticalities or Subnets
// clears that filter (an empty filter selects all critical assets). At least one field is required.
type ThreatExposureDefinitionPatch struct {
	Name               *string
	ExposurePoints     []ExposurePointAndLevel
	Locations          []string
	Criticalities      []string
	Subnets            []string
	MaxDurationSeconds *int64
}

// MarshalJSON sends only the fields that are set, keeping an empty list distinct from an absent one.
func (p ThreatExposureDefinitionPatch) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if p.Name != nil {
		out["name"] = *p.Name
	}
	if p.ExposurePoints != nil {
		out["exposurePoints"] = p.ExposurePoints
	}
	if p.Locations != nil {
		out["locations"] = p.Locations
	}
	if p.Criticalities != nil {
		out["criticalities"] = p.Criticalities
	}
	if p.Subnets != nil {
		out["subnets"] = p.Subnets
	}
	if p.MaxDurationSeconds != nil {
		out["maxDurationSeconds"] = *p.MaxDurationSeconds
	}
	return json.Marshal(out)
}

func (p ThreatExposureDefinitionPatch) validate() error {
	if p.Name == nil && p.ExposurePoints == nil && p.Locations == nil && p.Criticalities == nil && p.Subnets == nil && p.MaxDurationSeconds == nil {
		return errors.New("forward: a definition patch needs at least one field")
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return errors.New("forward: a definition name must not be blank")
	}
	if p.MaxDurationSeconds != nil && *p.MaxDurationSeconds <= 0 {
		return errors.New("forward: maxDurationSeconds must be positive")
	}
	if p.ExposurePoints != nil {
		if err := validateExposurePoints(p.ExposurePoints); err != nil {
			return err
		}
	}
	return validateCriticalities(p.Criticalities)
}

// ThreatExposurePaging pages the analysis tables. Results come in a fixed order and cannot be sorted or filtered through this SDK. Zero values
// send nothing, so Forward applies its defaults (offset 0, limit 1000); the limit is at most 10000.
type ThreatExposurePaging struct {
	Offset int
	Limit  int
}

func (p ThreatExposurePaging) body() (any, error) {
	if p.Offset < 0 || p.Limit < 0 || p.Limit > 10000 {
		return nil, errors.New("forward: offset cannot be negative and limit must be between 0 and 10000")
	}
	if p.Offset == 0 && p.Limit == 0 {
		return nil, nil
	}
	body := map[string]int{}
	if p.Offset > 0 {
		body["offset"] = p.Offset
	}
	if p.Limit > 0 {
		body["limit"] = p.Limit
	}
	return body, nil
}

func (s *ThreatExposureService) definitionsPath(networkID string) (string, error) {
	return s.client.networkSubPath(networkID, "threat-exposure-definitions")
}

func (s *ThreatExposureService) definitionPath(networkID, definitionID string) (string, error) {
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return "", err
	}
	if definitionID = strings.TrimSpace(definitionID); definitionID == "" {
		return "", errors.New("forward: definition ID is required")
	}
	return path + "/" + url.PathEscape(definitionID), nil
}

func (s *ThreatExposureService) analysisPath(networkID, definitionID string) (string, error) {
	if definitionID = strings.TrimSpace(definitionID); definitionID == "" {
		return "", errors.New("forward: definition ID is required")
	}
	return s.client.networkSubPath(networkID, "threat-exposure-analyses", url.PathEscape(definitionID))
}

func (s *ThreatExposureService) do(ctx context.Context, method, path string, body, out any, operation string) (*Response, error) {
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
func (s *ThreatExposureService) ListDefinitions(ctx context.Context, networkID string) ([]ThreatExposureDefinitionInfo, *Response, error) {
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	out := listResponse[ThreatExposureDefinitionInfo]{Keys: []string{"definitions"}}
	resp, err := s.do(ctx, http.MethodGet, path, nil, &out, "ThreatExposure.ListDefinitions")
	if err != nil {
		return nil, resp, err
	}
	return out.Items, resp, nil
}

// GetDefinition returns one definition (GET .../threat-exposure-definitions/{id}; 404 when it does not exist).
func (s *ThreatExposureService) GetDefinition(ctx context.Context, networkID, definitionID string) (*ThreatExposureDefinition, *Response, error) {
	path, err := s.definitionPath(networkID, definitionID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ThreatExposureDefinition)
	resp, err := s.do(ctx, http.MethodGet, path, nil, out, "ThreatExposure.GetDefinition")
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CreateDefinition saves a definition and returns it with its id (POST .../threat-exposure-definitions; 201; EDIT_THREAT_EXPOSURE and
// the organization property THREAT_EXPOSURE). Forward refuses an invalid definition with 400 and the reason.
func (s *ThreatExposureService) CreateDefinition(ctx context.Context, networkID string, definition NewThreatExposureDefinition) (*ThreatExposureDefinition, *Response, error) {
	if err := definition.Validate(); err != nil {
		return nil, nil, err
	}
	path, err := s.definitionsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	out := new(ThreatExposureDefinition)
	resp, err := s.do(ctx, http.MethodPost, path, definition, out, "ThreatExposure.CreateDefinition")
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// UpdateDefinition changes a definition (PATCH .../threat-exposure-definitions/{id}; 204; EDIT_THREAT_EXPOSURE and THREAT_EXPOSURE).
func (s *ThreatExposureService) UpdateDefinition(ctx context.Context, networkID, definitionID string, patch ThreatExposureDefinitionPatch) (*Response, error) {
	if err := patch.validate(); err != nil {
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
func (s *ThreatExposureService) DeleteDefinition(ctx context.Context, networkID, definitionID string) (*Response, error) {
	path, err := s.definitionPath(networkID, definitionID)
	if err != nil {
		return nil, err
	}
	return s.do(ctx, http.MethodDelete, path, nil, nil, "ThreatExposure.DeleteDefinition")
}

// ResolveDefinition previews what a definition (saved or not) selects on a snapshot: the exposure points and critical assets it
// resolves to, as Forward sent it (POST .../threat-exposure-definitions?action=resolve&view=summary&snapshotId=; VIEW_THREAT_EXPOSURE and
// THREAT_EXPOSURE; the snapshot must be processed to the reachability stage). The body is not modelled.
func (s *ThreatExposureService) ResolveDefinition(ctx context.Context, networkID, snapshotID string, definition NewThreatExposureDefinition) (json.RawMessage, *Response, error) {
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
func (s *ThreatExposureService) Evaluate(ctx context.Context, networkID, definitionID, snapshotID string) (*Response, error) {
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
func (s *ThreatExposureService) Status(ctx context.Context, networkID, definitionID, snapshotID string) (json.RawMessage, *Response, error) {
	return s.view(ctx, http.MethodGet, networkID, definitionID, snapshotID, "status", "", nil, "ThreatExposure.Status")
}

// CriticalAssetsSummary returns the critical assets the analysis found exposed, one page at a time, as Forward sent it (POST
// .../threat-exposure-analyses/{id}?view=criticalAssetsExposureSummary&snapshotId=; VIEW_THREAT_EXPOSURE). Rows come in a fixed order.
func (s *ThreatExposureService) CriticalAssetsSummary(ctx context.Context, networkID, definitionID, snapshotID string, paging ThreatExposurePaging) (json.RawMessage, *Response, error) {
	body, err := paging.body()
	if err != nil {
		return nil, nil, err
	}
	return s.view(ctx, http.MethodPost, networkID, definitionID, snapshotID, "criticalAssetsExposureSummary", "", body, "ThreatExposure.CriticalAssetsSummary")
}

// DirectExposureDetails returns how one critical asset is directly exposed, as Forward sent it (GET
// .../threat-exposure-analyses/{id}?view=criticalAssetDirectExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE). assetId is the global host id
// the summary gives the asset.
func (s *ThreatExposureService) DirectExposureDetails(ctx context.Context, networkID, definitionID, snapshotID string, assetID int64) (json.RawMessage, *Response, error) {
	if assetID < 0 {
		return nil, nil, errors.New("forward: asset ID must not be negative")
	}
	return s.view(ctx, http.MethodGet, networkID, definitionID, snapshotID, "criticalAssetDirectExposureDetails", fmt.Sprint(assetID), nil, "ThreatExposure.DirectExposureDetails")
}

// TransitiveExposureDetails returns how one critical asset is exposed through other hosts, one page at a time, as Forward sent it (POST
// .../threat-exposure-analyses/{id}?view=criticalAssetTransitiveExposureDetails&assetId=&snapshotId=; VIEW_THREAT_EXPOSURE).
func (s *ThreatExposureService) TransitiveExposureDetails(ctx context.Context, networkID, definitionID, snapshotID string, assetID int64, paging ThreatExposurePaging) (json.RawMessage, *Response, error) {
	body, err := paging.body()
	if err != nil {
		return nil, nil, err
	}
	if assetID < 0 {
		return nil, nil, errors.New("forward: asset ID must not be negative")
	}
	return s.view(ctx, http.MethodPost, networkID, definitionID, snapshotID, "criticalAssetTransitiveExposureDetails", fmt.Sprint(assetID), body, "ThreatExposure.TransitiveExposureDetails")
}

func (s *ThreatExposureService) view(ctx context.Context, method, networkID, definitionID, snapshotID, view, assetID string, body any, operation string) (json.RawMessage, *Response, error) {
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
