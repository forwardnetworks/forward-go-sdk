package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ScorecardsService reads a network's scorecards and the KPI categories they
// are built from (ScorecardController). Forward computes them for the
// organization's license tier, so what an org sees can depend on its licence.
//
// UNPUBLISHED: not in Forward's OpenAPI set, so shapes may change between
// builds. Identifying fields are typed; the rest stays in Raw. Served on
// primary 15398425a69 and stable 67e89c87124. Reads only; definitions cannot
// be created or changed through this service.
type Scorecards service

func (s *Scorecards) networkPath(networkID, segment string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + segment, nil
}

func getJSONList[T any](ctx context.Context, c *Client, path, operation string) ([]T, *ports.Response, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out []T
	resp, err := c.doRequired(req, &out)
	return out, resp, err
}

// ForSnapshot returns every scorecard scored on a snapshot, with per-category
// results. GET /api/networks/{networkId}/scorecards?snapshotId=
// (VIEW_SCORECARDS; the snapshot must be past the START stage). The snapshot
// is required; there is no latest default. Unpublished.
func (s *Scorecards) ForSnapshot(ctx context.Context, networkID, snapshotID string) ([]ports.SnapshotScorecard, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path, err := s.networkPath(networkID, "scorecards")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[ports.SnapshotScorecard](ctx, s.client, path+"?"+url.Values{"snapshotId": []string{snapshotID}}.Encode(), "Scorecards.ForSnapshot")
}

// Definitions returns the network's scorecard definitions (Forward documents
// this view as "for debugging purposes"). GET
// /api/networks/{networkId}/scorecards?view=definitions (VIEW_SCORECARDS).
// Unpublished.
func (s *Scorecards) Definitions(ctx context.Context, networkID string) ([]ports.Scorecard, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.networkPath(networkID, "scorecards")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[ports.Scorecard](ctx, s.client, path+"?view=definitions", "Scorecards.Definitions")
}

// Trends returns each scorecard's score over time, keyed by scorecard ID, in
// chronological order: up to maxPoints snapshots between start and end
// inclusive. start must precede end and maxPoints must be at least 2; zero
// maxPoints uses Forward's default of 30. GET
// /api/networks/{networkId}/scorecards?view=trends&start=&end=&maxPoints=
// (VIEW_SCORECARDS). Times go out as UTC. Unpublished.
func (s *Scorecards) Trends(ctx context.Context, networkID string, start, end time.Time, maxPoints int) (map[string][]ports.ScorecardPoint, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, nil, errors.New("forward: scorecard trends need a start before an end")
	}
	if maxPoints != 0 && maxPoints < 2 {
		return nil, nil, errors.New("forward: maxPoints must be at least 2")
	}
	path, err := s.networkPath(networkID, "scorecards")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": []string{"trends"}, "start": []string{start.UTC().Format(time.RFC3339Nano)}, "end": []string{end.UTC().Format(time.RFC3339Nano)}}
	if maxPoints != 0 {
		query.Set("maxPoints", strconv.Itoa(maxPoints))
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Scorecards.Trends")
	out := map[string][]ports.ScorecardPoint{}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// KPICategories returns the network's KPI categories with their checks. GET
// /api/networks/{networkId}/kpi-categories (VIEW_SCORECARDS). Unpublished.
func (s *Scorecards) KPICategories(ctx context.Context, networkID string) ([]ports.KPICategory, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.networkPath(networkID, "kpi-categories")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[ports.KPICategory](ctx, s.client, path, "Scorecards.KPICategories")
}

// KPICategoryDefinitions returns the KPI category definitions, which carry the
// check definitions where KPICategories carries the checks. GET
// /api/networks/{networkId}/kpi-categories?view=definitions (VIEW_SCORECARDS).
// Unpublished.
func (s *Scorecards) KPICategoryDefinitions(ctx context.Context, networkID string) ([]ports.KPICategory, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.networkPath(networkID, "kpi-categories")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[ports.KPICategory](ctx, s.client, path+"?view=definitions", "Scorecards.KPICategoryDefinitions")
}
