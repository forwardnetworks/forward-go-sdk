package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type Dashboards service

func (s *Dashboards) path(networkID, dashboardID, tail string) (string, error) {
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
func (s *Dashboards) List(ctx context.Context, networkID string) ([]ports.Dashboard, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
func (s *Dashboards) Defaults(ctx context.Context, networkID string) ([]ports.Dashboard, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.path(networkID, "", "")
	if err != nil {
		return nil, nil, err
	}
	return s.dashboards(ctx, path+"?"+url.Values{"type": []string{"DEFAULT"}}.Encode(), "Dashboards.Defaults")
}

func (s *Dashboards) dashboards(ctx context.Context, path, operation string) ([]ports.Dashboard, *ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out []ports.Dashboard
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Get returns one dashboard, custom or predefined, or (nil, nil) when it does
// not exist. GET /api/networks/{networkId}/dashboards/{dashboardId}
// (VIEW_CUSTOM_DASHBOARDS). Unpublished.
func (s *Dashboards) Get(ctx context.Context, networkID, dashboardID string) (*ports.Dashboard, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.path(networkID, dashboardID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Dashboards.Get")
	out := new(ports.Dashboard)
	resp, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DisplaySettings returns a dashboard's full-screen display settings. GET
// /api/networks/{networkId}/dashboards/{dashboardId}/display-settings
// (VIEW_NETWORK_AND_SNAPSHOTS). Unpublished.
func (s *Dashboards) DisplaySettings(ctx context.Context, networkID, dashboardID string) (*ports.DashboardDisplaySettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.path(networkID, dashboardID, "/display-settings")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Dashboards.DisplaySettings")
	out := new(ports.DashboardDisplaySettings)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
