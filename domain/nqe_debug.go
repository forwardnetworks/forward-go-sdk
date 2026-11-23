package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NQERunMetricsOptions filters RunMetrics. The zero value asks for Forward's
// defaults: the last 7 days, runs of at least 10 seconds, at most 100 of them.
type NQERunMetricsOptions struct {
	From, To time.Time
	// MinExecutionSeconds and Limit are pointers so that an explicit value
	// differs from "use Forward's default" (10 seconds, 100 runs). A
	// MinExecutionSeconds of 0 means every run; a Limit of 0 returns nothing,
	// and a negative Limit is refused.
	MinExecutionSeconds *int
	Limit               *int
}

// NQERunMetric is one slow NQE run Forward recorded. QueryID and CommitID are
// set only for a saved library query (empty for a run of ad-hoc text).
// TrackingID ties the run to the request that started it.
type NQERunMetric struct {
	TaskID                  string
	NetworkID               string
	NetworkName             string
	SnapshotID              string
	SnapshotCreationInstant time.Time
	ExecutedAt              time.Time
	RunDuration             time.Duration
	// SourceSetID is Forward's identifier for the query's source set, a string
	// such as "NqeSourceSetId_<hex>" (the key GET /api/nqe-sources-json/{id} takes).
	SourceSetID string
	QueryID     string
	CommitID    string
	TrackingID  string
}

// UnmarshalJSON reads Forward's shape: executedAt and snapshotCreationInstant
// are epoch milliseconds, runDuration is milliseconds.
func (m *NQERunMetric) UnmarshalJSON(data []byte) error {
	var wire struct {
		TaskID                  json.RawMessage `json:"taskId"`
		NetworkID               json.RawMessage `json:"networkId"`
		NetworkName             string          `json:"networkName"`
		SnapshotID              json.RawMessage `json:"snapshotId"`
		SnapshotCreationInstant json.RawMessage `json:"snapshotCreationInstant"`
		ExecutedAt              json.RawMessage `json:"executedAt"`
		RunDuration             json.RawMessage `json:"runDuration"`
		SourceSetID             json.RawMessage `json:"sourceSetId"`
		QueryID                 string          `json:"queryId"`
		CommitID                string          `json:"commitId"`
		TrackingID              string          `json:"trackingId"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var err error
	*m = NQERunMetric{
		TaskID: scalarText(wire.TaskID), NetworkID: scalarText(wire.NetworkID), NetworkName: wire.NetworkName,
		SnapshotID: scalarText(wire.SnapshotID), SourceSetID: scalarText(wire.SourceSetID), QueryID: wire.QueryID,
		CommitID: wire.CommitID, TrackingID: wire.TrackingID,
	}
	if m.ExecutedAt, err = instantFromJSON(wire.ExecutedAt); err != nil {
		return fmt.Errorf("executedAt: %w", err)
	}
	if m.SnapshotCreationInstant, err = instantFromJSON(wire.SnapshotCreationInstant); err != nil {
		return fmt.Errorf("snapshotCreationInstant: %w", err)
	}
	if text := strings.TrimSpace(string(wire.RunDuration)); text != "" && text != "null" {
		millis, perr := strconv.ParseFloat(text, 64)
		if perr != nil {
			return fmt.Errorf("runDuration: want milliseconds, got %s", text)
		}
		m.RunDuration = time.Duration(millis * float64(time.Millisecond))
	}
	return nil
}

// scalarText reads a JSON string or number as text, and "" for null or absent.
func scalarText(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return text
}

// instantFromJSON reads epoch milliseconds (Forward's @JsonFormat NUMBER) or an
// ISO-8601 string, and the zero time for null or absent.
func instantFromJSON(raw json.RawMessage) (time.Time, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return time.Time{}, nil
	}
	if text[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.RFC3339Nano, s)
	}
	millis, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("want epoch milliseconds, got %s", text)
	}
	return time.UnixMilli(int64(millis)).UTC(), nil
}
