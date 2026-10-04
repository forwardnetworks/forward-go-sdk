package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// DashboardsService reads a network's dashboards (DashboardController).
//
// UNPUBLISHED: none of the dashboard, scorecard and KPI routes is in Forward's
// OpenAPI set, so their shapes may change between builds. The identifying
// fields are typed; everything else stays in Raw so a change does not break a
// read. They are served, with the routes below, on primary 15398425a69 and
// stable 67e89c87124. Reads only; dashboard and scorecard writes are not wrapped.
type DashboardsService service

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

func (s *DashboardsService) path(networkID, dashboardID, tail string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	path += "/dashboards"
	if dashboardID == "" {
		return path, nil
	}
	if dashboardID = strings.TrimSpace(dashboardID); dashboardID == "" {
		return "", errors.New("forward: dashboard ID is required")
	}
	return path + "/" + url.PathEscape(dashboardID) + tail, nil
}

// List returns the network's custom dashboards. GET
// /api/networks/{networkId}/dashboards (VIEW_CUSTOM_DASHBOARDS). Unpublished.
func (s *DashboardsService) List(ctx context.Context, networkID string) ([]Dashboard, *Response, error) {
	path, err := s.path(networkID, "", "")
	if err != nil {
		return nil, nil, err
	}
	return s.dashboards(ctx, path, "Dashboards.List")
}

// Defaults returns the predefined dashboards Forward ships, each with an ID
// and a name only. Forward says this is "not intended for use by the UI; exists
// for API completeness". GET /api/networks/{networkId}/dashboards?type=DEFAULT
// (VIEW_NETWORK_AND_SNAPSHOTS). Unpublished.
func (s *DashboardsService) Defaults(ctx context.Context, networkID string) ([]Dashboard, *Response, error) {
	path, err := s.path(networkID, "", "")
	if err != nil {
		return nil, nil, err
	}
	return s.dashboards(ctx, path+"?"+url.Values{"type": []string{"DEFAULT"}}.Encode(), "Dashboards.Defaults")
}

func (s *DashboardsService) dashboards(ctx context.Context, path, operation string) ([]Dashboard, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out []Dashboard
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Get returns one dashboard, custom or predefined, or (nil, nil) when it does
// not exist. GET /api/networks/{networkId}/dashboards/{dashboardId}
// (VIEW_CUSTOM_DASHBOARDS). Unpublished.
func (s *DashboardsService) Get(ctx context.Context, networkID, dashboardID string) (*Dashboard, *Response, error) {
	path, err := s.path(networkID, dashboardID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Dashboards.Get")
	out := new(Dashboard)
	resp, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
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

// DisplaySettings returns a dashboard's full-screen display settings. GET
// /api/networks/{networkId}/dashboards/{dashboardId}/display-settings
// (VIEW_NETWORK_AND_SNAPSHOTS). Unpublished.
func (s *DashboardsService) DisplaySettings(ctx context.Context, networkID, dashboardID string) (*DashboardDisplaySettings, *Response, error) {
	path, err := s.path(networkID, dashboardID, "/display-settings")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Dashboards.DisplaySettings")
	out := new(DashboardDisplaySettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
