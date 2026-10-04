package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
type ScorecardsService service

// Scorecard is a scorecard definition: a named weighting of KPI categories.
// CategoryWeights maps a KPI category ID to its weight.
type Scorecard struct {
	ID              Identifier      `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	CategoryWeights map[string]int  `json:"categoryWeights,omitempty"`
	Raw             json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the whole object in Raw.
func (c *Scorecard) UnmarshalJSON(data []byte) error {
	type plain Scorecard
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*c = Scorecard(out)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// SnapshotScorecard is a scorecard scored on one snapshot. Score is Forward's
// overall score when it sends one. Categories carry each KPI category's own
// score and weight; their checks are left raw.
type SnapshotScorecard struct {
	ID          Identifier         `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Score       *float64           `json:"score,omitempty"`
	Categories  []KPICategoryScore `json:"categories,omitempty"`
	Raw         json.RawMessage    `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the whole object in Raw.
func (c *SnapshotScorecard) UnmarshalJSON(data []byte) error {
	type plain SnapshotScorecard
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*c = SnapshotScorecard(out)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// KPICategoryScore is one KPI category inside a scored scorecard: its weight in
// the scorecard, whether Forward manages it (SystemManaged), and its checks with
// their scores, left raw.
type KPICategoryScore struct {
	ID            Identifier      `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	SystemManaged bool            `json:"systemManaged,omitempty"`
	Weight        int             `json:"weight"`
	Checks        json.RawMessage `json:"checks,omitempty"`
}

// ScorecardPoint is a scorecard's score on one snapshot, for trends. Score is
// nil when Forward could not compute one for that snapshot.
type ScorecardPoint struct {
	SnapshotID Identifier `json:"snapshotId"`
	Time       time.Time  `json:"-"`
	Score      *float64   `json:"score,omitempty"`
}

// UnmarshalJSON reads instant as epoch milliseconds (Forward's choice for this
// type) or an ISO-8601 instant.
func (p *ScorecardPoint) UnmarshalJSON(data []byte) error {
	type plain ScorecardPoint
	wire := struct {
		plain
		Instant json.RawMessage `json:"instant"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis, err := decodeEpochMillisOrInstant(wire.Instant)
	if err != nil {
		return fmt.Errorf("forward: scorecard point instant: %w", err)
	}
	*p = ScorecardPoint(wire.plain)
	if millis != 0 {
		p.Time = time.UnixMilli(millis).UTC()
	}
	return nil
}

// KPICategory is a KPI category of the network. Checks is left raw.
type KPICategory struct {
	ID            Identifier      `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	SystemManaged bool            `json:"systemManaged,omitempty"`
	Checks        json.RawMessage `json:"checks,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the whole object in Raw.
func (c *KPICategory) UnmarshalJSON(data []byte) error {
	type plain KPICategory
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*c = KPICategory(out)
	c.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (s *ScorecardsService) networkPath(networkID, segment string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + segment, nil
}

func getJSONList[T any](ctx context.Context, c *Client, path, operation string) ([]T, *Response, error) {
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
func (s *ScorecardsService) ForSnapshot(ctx context.Context, networkID, snapshotID string) ([]SnapshotScorecard, *Response, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	path, err := s.networkPath(networkID, "scorecards")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[SnapshotScorecard](ctx, s.client, path+"?"+url.Values{"snapshotId": []string{snapshotID}}.Encode(), "Scorecards.ForSnapshot")
}

// Definitions returns the network's scorecard definitions (Forward documents
// this view as "for debugging purposes"). GET
// /api/networks/{networkId}/scorecards?view=definitions (VIEW_SCORECARDS).
// Unpublished.
func (s *ScorecardsService) Definitions(ctx context.Context, networkID string) ([]Scorecard, *Response, error) {
	path, err := s.networkPath(networkID, "scorecards")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[Scorecard](ctx, s.client, path+"?view=definitions", "Scorecards.Definitions")
}

// Trends returns each scorecard's score over time, keyed by scorecard ID, in
// chronological order: up to maxPoints snapshots between start and end
// inclusive. start must precede end and maxPoints must be at least 2; zero
// maxPoints uses Forward's default of 30. GET
// /api/networks/{networkId}/scorecards?view=trends&start=&end=&maxPoints=
// (VIEW_SCORECARDS). Times go out as UTC. Unpublished.
func (s *ScorecardsService) Trends(ctx context.Context, networkID string, start, end time.Time, maxPoints int) (map[string][]ScorecardPoint, *Response, error) {
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
	out := map[string][]ScorecardPoint{}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// KPICategories returns the network's KPI categories with their checks. GET
// /api/networks/{networkId}/kpi-categories (VIEW_SCORECARDS). Unpublished.
func (s *ScorecardsService) KPICategories(ctx context.Context, networkID string) ([]KPICategory, *Response, error) {
	path, err := s.networkPath(networkID, "kpi-categories")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[KPICategory](ctx, s.client, path, "Scorecards.KPICategories")
}

// KPICategoryDefinitions returns the KPI category definitions, which carry the
// check definitions where KPICategories carries the checks. GET
// /api/networks/{networkId}/kpi-categories?view=definitions (VIEW_SCORECARDS).
// Unpublished.
func (s *ScorecardsService) KPICategoryDefinitions(ctx context.Context, networkID string) ([]KPICategory, *Response, error) {
	path, err := s.networkPath(networkID, "kpi-categories")
	if err != nil {
		return nil, nil, err
	}
	return getJSONList[KPICategory](ctx, s.client, path+"?view=definitions", "Scorecards.KPICategoryDefinitions")
}
