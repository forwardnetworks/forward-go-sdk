package domain

import (
	"encoding/json"
)

// Dashboard is a dashboard definition. Layout is the list of widgets, left raw
// because its shape is large and version-dependent. The predefined dashboards
// Forward lists with type=DEFAULT have an ID and a Name only. CreatedBy and the
// other attribution fields are empty for those.
type Dashboard struct {
	ID          Identifier      `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Layout      json.RawMessage `json:"layout,omitempty"`
	CreatedAt   string          `json:"createdAt,omitempty"`
	CreatedBy   string          `json:"createdBy,omitempty"`
	CreatedByID Identifier      `json:"createdById,omitempty"`
	UpdatedAt   string          `json:"updatedAt,omitempty"`
	UpdatedBy   string          `json:"updatedBy,omitempty"`
	UpdatedByID Identifier      `json:"updatedById,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the whole object in Raw.
func (d *Dashboard) UnmarshalJSON(data []byte) error {
	type plain Dashboard
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*d = Dashboard(out)
	d.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DashboardDisplaySettings is how a dashboard scrolls when shown full screen.
// AutoScrollMode and AutoScrollSpeed are Forward's enum names, which are
// version-dependent, so they stay strings. Unset fields are nil or empty.
type DashboardDisplaySettings struct {
	AutoScrollEnabled     *bool           `json:"autoScrollEnabled,omitempty"`
	AutoScrollMode        string          `json:"autoScrollMode,omitempty"`
	AutoScrollSpeed       string          `json:"autoScrollSpeed,omitempty"`
	AutoScrollIntervalSec *int            `json:"autoScrollIntervalSec,omitempty"`
	Raw                   json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the whole object in Raw.
func (d *DashboardDisplaySettings) UnmarshalJSON(data []byte) error {
	type plain DashboardDisplaySettings
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*d = DashboardDisplaySettings(out)
	d.Raw = append(json.RawMessage(nil), data...)
	return nil
}
