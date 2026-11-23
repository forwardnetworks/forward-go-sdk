package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

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
