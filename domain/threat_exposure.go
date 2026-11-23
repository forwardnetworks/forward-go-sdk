package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

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

func (p ThreatExposureDefinitionPatch) Validate() error {
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

func (p ThreatExposurePaging) Body() (any, error) {
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
