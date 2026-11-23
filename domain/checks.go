package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Check is one evaluated check on a particular snapshot.
type Check struct {
	ID                    Identifier `json:"id,omitempty"`
	Name                  string     `json:"name"`
	Status                string     `json:"status"`
	Description           string     `json:"description,omitempty"`
	Note                  string     `json:"note,omitempty"`
	Priority              string     `json:"priority,omitempty"`
	Tags                  []string   `json:"tags,omitempty"`
	Enabled               *bool      `json:"enabled,omitempty"`
	PerfMonitoringEnabled *bool      `json:"perfMonitoringEnabled,omitempty"`
	Creator               string     `json:"creator,omitempty"`
	CreatorID             Identifier `json:"creatorId,omitempty"`
	Editor                string     `json:"editor,omitempty"`
	EditorID              Identifier `json:"editorId,omitempty"`
	CreatedAt             string     `json:"createdAt,omitempty"`
	DefinedAt             string     `json:"definedAt,omitempty"`
	EditedAt              string     `json:"editedAt,omitempty"`
	ExecutedAt            string     `json:"executedAt,omitempty"`
	ExecutionDurationMS   *int64     `json:"executionDurationMillis,omitempty"`
	// Outdated marks a result computed against a definition that has since
	// changed, so the status describes a check that no longer exists as
	// stated.
	Outdated *bool `json:"outdated,omitempty"`
	// NumViolations is only sent for a failing check, so nil means the check
	// did not fail rather than "failed zero times".
	NumViolations *int64 `json:"numViolations,omitempty"`
	// Definition stays raw for the reason NewCheck.Definition is open: flow,
	// isolation, NQE, and predefined checks have different schemas.
	Definition json.RawMessage `json:"definition,omitempty"`
	// NQE checks carry the keys that locate their query results.
	NQEResultKey   string `json:"nqeResultKey,omitempty"`
	NQESourceSetID string `json:"nqeSourceSetId,omitempty"`
}

// CheckDetail is a single check read back with its diagnosis. The diagnosis is
// only computed for a lookup by id, which is why listing cannot produce one.
type CheckDetail struct {
	Check
	Diagnosis *CheckDiagnosis `json:"diagnosis,omitempty"`
}

// CheckDiagnosis explains a check's result. For a FAIL it lists the violations; for an ERROR or TIMEOUT, Summary carries the reason when
// Forward recorded one (CheckResult.error/timeout store their message there; some error paths record none). Only Checks.Get returns it.
type CheckDiagnosis struct {
	Summary string            `json:"summary,omitempty"`
	Details []DiagnosisDetail `json:"details,omitempty"`
	// DetailsIncomplete reports that Forward stopped short of enumerating
	// every violation, so Details is a sample rather than the whole set.
	DetailsIncomplete *bool `json:"detailsIncomplete,omitempty"`
}

// DiagnosisDetail is one finding, and the query that produced it.
//
// Forward's spec types query as a string, and for most checks it is one, but a
// loop violation (a predefined NO_LOOP check) sends an OBJECT such as
// {"flowTypes":["LOOP"]}, with no flow and no devices. So Query holds the
// string form only (empty when Forward sent an object), QueryRaw always holds
// whatever was sent, and FlowTypes reads the object form's flowTypes. Decoding
// never fails a whole check read over this field.
type DiagnosisDetail struct {
	Query      string               `json:"-"`
	QueryRaw   json.RawMessage      `json:"-"`
	References []DiagnosisReference `json:"references,omitempty"`
}

// UnmarshalJSON accepts query as a string or as any other JSON value.
func (d *DiagnosisDetail) UnmarshalJSON(data []byte) error {
	var wire struct {
		Query      json.RawMessage      `json:"query"`
		References []DiagnosisReference `json:"references"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*d = DiagnosisDetail{References: wire.References}
	if len(wire.Query) == 0 || string(wire.Query) == "null" {
		return nil
	}
	d.QueryRaw = append(json.RawMessage(nil), wire.Query...)
	var text string
	if json.Unmarshal(wire.Query, &text) == nil {
		d.Query = text
	}
	return nil
}

// MarshalJSON writes query back as it was received, so a read-modify-write
// round trip keeps the object form.
func (d DiagnosisDetail) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	switch {
	case len(d.QueryRaw) != 0:
		out["query"] = d.QueryRaw
	case d.Query != "":
		out["query"] = d.Query
	}
	if len(d.References) != 0 {
		out["references"] = d.References
	}
	return json.Marshal(out)
}

// FlowTypes returns the flowTypes of an object-form query (["LOOP"] for a
// loop violation), or nil when query was a string or had none.
func (d DiagnosisDetail) FlowTypes() []string {
	var object struct {
		FlowTypes []string `json:"flowTypes"`
	}
	if len(d.QueryRaw) == 0 || json.Unmarshal(d.QueryRaw, &object) != nil {
		return nil
	}
	return object.FlowTypes
}

// DiagnosisReference points a finding at the configuration that caused it,
// down to the lines of the device files involved.
type DiagnosisReference struct {
	Key   string                 `json:"key,omitempty"`
	Value string                 `json:"value,omitempty"`
	Files map[string][]LineRange `json:"files,omitempty"`
}

// LineRange is a span within a device file.
type LineRange struct {
	Start *int32 `json:"start,omitempty"`
	End   *int32 `json:"end,omitempty"`
}

// CheckListOptions filters a listing server-side. Each field contributes one
// repeated query parameter, and an empty field means "no filter" rather than
// "match nothing".
type CheckListOptions struct {
	Types      []string
	Statuses   []string
	Priorities []string
}

// NewCheck is Forward's NewNetworkCheck payload. Definition stays structurally
// open because flow, isolation, NQE, and intent checks have different schemas.
type NewCheck struct {
	Definition map[string]any `json:"definition"`
	// Name is omitted when empty. An NQE check takes its name from the query
	// and Forward rejects the field outright -- including an empty one -- so
	// sending it unconditionally makes every NQE check fail to create. A
	// PREDEFINED check is the same: a live Forward answered 400 when a name was
	// sent with one (reported from use; not checked against Forward's source),
	// so leave Name empty for a definition that names a predefinedCheckType.
	Name                  string   `json:"name,omitempty"`
	Note                  string   `json:"note,omitempty"`
	Tags                  []string `json:"tags,omitempty"`
	Enabled               *bool    `json:"enabled,omitempty"`
	Priority              string   `json:"priority,omitempty"`
	PerfMonitoringEnabled *bool    `json:"perfMonitoringEnabled,omitempty"`
}

var (
	ErrNoChecks              = errors.New("forward: snapshot carries no checks")
	ErrCheckCorpusIncomplete = errors.New("forward: snapshot check corpus is incomplete")
)

// CheckRequirements describes the corpus a scoring operation expects. Every
// exact name and every regular-expression pattern must match at least one row.
// MinChecks defaults to one.
type CheckRequirements struct {
	MinChecks        int
	RequiredNames    []string
	RequiredPatterns []string
}

// NoChecksError prevents an empty check list from being interpreted as a
// successful 0/0 score. This is especially important for new workspaces: checks
// persist forward from the snapshot where they were promoted, not sideways into
// an independently-created workspace network.
type NoChecksError struct {
	SnapshotID string
}

func (e *NoChecksError) Error() string {
	if e == nil || e.SnapshotID == "" {
		return ErrNoChecks.Error()
	}
	return fmt.Sprintf("%s: %s", ErrNoChecks, e.SnapshotID)
}

func (e *NoChecksError) Is(target error) bool { return target == ErrNoChecks }

// IncompleteCheckCorpusError reports which scoring requirements were absent.
type IncompleteCheckCorpusError struct {
	SnapshotID string
	Missing    []string
}

func (e *IncompleteCheckCorpusError) Error() string {
	if e == nil {
		return ErrCheckCorpusIncomplete.Error()
	}
	return fmt.Sprintf("%s on snapshot %s: missing %s", ErrCheckCorpusIncomplete, e.SnapshotID, strings.Join(e.Missing, ", "))
}

func (e *IncompleteCheckCorpusError) Is(target error) bool { return target == ErrCheckCorpusIncomplete }

// CheckSet is a non-empty, snapshot-bound set returned by ForScoring. Its rows
// are private so scoring code cannot construct a vacuous set accidentally.
type CheckSet struct {
	snapshotID string
	checks     []Check
}

// NewCheckSet builds a CheckSet over a copy of checks. Checks.ForScoring returns one only when the corpus is complete; a set built
// here claims completeness on its builder's word.
func NewCheckSet(snapshotID string, checks []Check) CheckSet {
	return CheckSet{snapshotID: snapshotID, checks: append([]Check(nil), checks...)}
}

func (s CheckSet) SnapshotID() string { return s.snapshotID }

func (s CheckSet) Len() int { return len(s.checks) }

func (s CheckSet) Checks() []Check { return append([]Check(nil), s.checks...) }

// PredefinedCheck is a built-in check Forward offers (AvailablePredefinedCheck):
// PredefinedCheckType is what a check definition names to use it.
type PredefinedCheck struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	PredefinedCheckType string `json:"predefinedCheckType"`
}
