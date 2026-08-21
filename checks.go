package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ChecksService manages checks attached to snapshots. Checks are not created on
// a network resource: they attach to one real snapshot. When created with
// CreatePersistent, Forward carries them onto later real and predicted snapshots
// of that network.
type ChecksService service

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

// CheckDiagnosis explains why a check failed.
type CheckDiagnosis struct {
	Summary string            `json:"summary,omitempty"`
	Details []DiagnosisDetail `json:"details,omitempty"`
	// DetailsIncomplete reports that Forward stopped short of enumerating
	// every violation, so Details is a sample rather than the whole set.
	DetailsIncomplete *bool `json:"detailsIncomplete,omitempty"`
}

// DiagnosisDetail is one finding, and the query that produced it.
type DiagnosisDetail struct {
	Query      string               `json:"query,omitempty"`
	References []DiagnosisReference `json:"references,omitempty"`
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

func (o CheckListOptions) query() url.Values {
	query := url.Values{}
	for key, values := range map[string][]string{
		"type": o.Types, "status": o.Statuses, "priority": o.Priorities,
	} {
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				query.Add(key, value)
			}
		}
	}
	return query
}

// NewCheck is Forward's NewNetworkCheck payload. Definition stays structurally
// open because flow, isolation, NQE, and intent checks have different schemas.
type NewCheck struct {
	Definition            map[string]any `json:"definition"`
	Name                  string         `json:"name"`
	Note                  string         `json:"note,omitempty"`
	Tags                  []string       `json:"tags,omitempty"`
	Enabled               *bool          `json:"enabled,omitempty"`
	Priority              string         `json:"priority,omitempty"`
	PerfMonitoringEnabled *bool          `json:"perfMonitoringEnabled,omitempty"`
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

func (s CheckSet) SnapshotID() string { return s.snapshotID }
func (s CheckSet) Len() int           { return len(s.checks) }
func (s CheckSet) Checks() []Check    { return append([]Check(nil), s.checks...) }

const checkPrimeAttempts = 4

// List returns checks evaluated on snapshotID, optionally filtered. The first GET primes Forward's
// evaluation cache and can outlive an http.Client deadline while computation
// continues server-side, so client-side timeout failures are re-issued up to
// four times. Cancellation of the caller's context is never retried.
func (s *ChecksService) List(
	ctx context.Context,
	snapshotID string,
	options ...CheckListOptions,
) ([]Check, *Response, error) {
	if len(options) > 1 {
		return nil, nil, errors.New("forward: at most one check list option set is allowed")
	}
	if err := s.client.requireCapability(CapabilityPersistentSnapshotChecks); err != nil {
		return nil, nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if len(options) == 1 {
		if query := options[0].query(); len(query) != 0 {
			path += "?" + query.Encode()
		}
	}
	var lastResponse *Response
	for attempt := 1; attempt <= checkPrimeAttempts; attempt++ {
		req, requestErr := s.client.NewRequest(ctx, http.MethodGet, path, nil)
		if requestErr != nil {
			return nil, nil, requestErr
		}
		result := listResponse[Check]{Keys: []string{"checks", "items"}}
		response, requestErr := s.client.Do(req, &result)
		lastResponse = response
		if requestErr == nil {
			s.client.observeCapability(CapabilityPersistentSnapshotChecks)
			return result.Items, response, nil
		}
		if ctx.Err() != nil || attempt == checkPrimeAttempts || !isClientTimeout(requestErr) {
			return nil, response, requestErr
		}
	}
	return nil, lastResponse, errors.New("forward: unreachable check retry state")
}

// ForScoring returns a non-empty CheckSet or ErrNoChecks. Consumers should use
// this method—not List—before computing pass/fail totals, so a workspace that
// never received the persistent corpus cannot produce a vacuous pass.
func (s *ChecksService) ForScoring(
	ctx context.Context,
	snapshotID string,
	requirements ...CheckRequirements,
) (CheckSet, *Response, error) {
	if len(requirements) > 1 {
		return CheckSet{}, nil, errors.New("forward: at most one check requirement set is allowed")
	}
	checks, response, err := s.List(ctx, snapshotID)
	if err != nil {
		return CheckSet{}, response, err
	}
	if len(checks) == 0 {
		return CheckSet{}, response, &NoChecksError{SnapshotID: strings.TrimSpace(snapshotID)}
	}
	required := CheckRequirements{MinChecks: 1}
	if len(requirements) == 1 {
		required = requirements[0]
		if required.MinChecks <= 0 {
			required.MinChecks = 1
		}
	}
	missing, err := missingCheckRequirements(checks, required)
	if err != nil {
		return CheckSet{}, response, err
	}
	if len(missing) != 0 {
		return CheckSet{}, response, &IncompleteCheckCorpusError{
			SnapshotID: strings.TrimSpace(snapshotID),
			Missing:    missing,
		}
	}
	return CheckSet{snapshotID: strings.TrimSpace(snapshotID), checks: append([]Check(nil), checks...)}, response, nil
}

// CreatePersistent creates a check on snapshotID with persistent=true. The
// snapshot must be a real snapshot; a successful check is inherited and
// evaluated by later snapshots of that same network, including Predict output.
func (s *ChecksService) CreatePersistent(ctx context.Context, snapshotID string, check NewCheck) (Identifier, *Response, error) {
	if err := s.client.requireCapability(CapabilityPersistentSnapshotChecks); err != nil {
		return "", nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(check.Name) == "" || check.Definition == nil {
		return "", nil, errors.New("forward: check name and definition are required")
	}
	path += "?persistent=true"
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, check)
	if err != nil {
		return "", nil, err
	}
	var created struct {
		ID Identifier `json:"id"`
	}
	response, err := s.client.Do(req, &created)
	if err != nil {
		return "", response, err
	}
	if created.ID == "" {
		return "", response, errors.New("forward: persistent check create returned no ID")
	}
	s.client.observeCapability(CapabilityPersistentSnapshotChecks)
	return created.ID, response, nil
}

// ExistingNames returns active check names for idempotent corpus promotion.
func (s *ChecksService) ExistingNames(ctx context.Context, snapshotID string) (map[string]bool, *Response, error) {
	checks, response, err := s.List(ctx, snapshotID)
	if err != nil {
		return nil, response, err
	}
	names := make(map[string]bool, len(checks))
	for _, check := range checks {
		if name := strings.TrimSpace(check.Name); name != "" {
			names[name] = true
		}
	}
	return names, response, nil
}

func checksPath(snapshotID string) (string, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return "", errors.New("forward: snapshot ID is required")
	}
	return "/api/snapshots/" + url.PathEscape(snapshotID) + "/checks", nil
}

func isClientTimeout(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func missingCheckRequirements(checks []Check, required CheckRequirements) ([]string, error) {
	var missing []string
	if len(checks) < required.MinChecks {
		missing = append(missing, fmt.Sprintf("at least %d checks (got %d)", required.MinChecks, len(checks)))
	}
	names := make(map[string]bool, len(checks))
	for _, check := range checks {
		names[check.Name] = true
	}
	for _, name := range required.RequiredNames {
		if name = strings.TrimSpace(name); name != "" && !names[name] {
			missing = append(missing, "name="+name)
		}
	}
	for _, pattern := range required.RequiredPatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("forward: invalid required check pattern %q: %w", pattern, err)
		}
		matched := false
		for name := range names {
			if expression.MatchString(name) {
				matched = true
				break
			}
		}
		if !matched {
			missing = append(missing, "pattern="+pattern)
		}
	}
	return missing, nil
}

// Get returns one check with its diagnosis.
func (s *ChecksService) Get(ctx context.Context, snapshotID, checkID string) (*CheckDetail, *Response, error) {
	path, err := checkPath(snapshotID, checkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	detail := new(CheckDetail)
	resp, err := s.client.Do(req, detail)
	return detail, resp, err
}

// Create adds a check to snapshotID and returns it as evaluated.
//
// A persistent check is inherited and re-evaluated by later snapshots of the
// same network, including Predict output; a non-persistent one belongs to this
// snapshot alone. persistent is a pointer so that leaving it unstated defers
// to whatever the appserver defaults to, rather than asserting a default here.
func (s *ChecksService) Create(
	ctx context.Context,
	snapshotID string,
	check NewCheck,
	persistent *bool,
) (*CheckDetail, *Response, error) {
	if err := s.client.requireCapability(CapabilityPersistentSnapshotChecks); err != nil {
		return nil, nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if check.Definition == nil {
		return nil, nil, errors.New("forward: check definition is required")
	}
	if persistent != nil {
		path += "?persistent=" + strconv.FormatBool(*persistent)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, check)
	if err != nil {
		return nil, nil, err
	}
	created := new(CheckDetail)
	resp, err := s.client.Do(req, created)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityPersistentSnapshotChecks)
	return created, resp, nil
}

// Deactivate disables one check on a snapshot.
//
// Forward deactivates rather than deletes: the check stops evaluating but its
// history stays readable, so a later snapshot can still be explained.
func (s *ChecksService) Deactivate(ctx context.Context, snapshotID, checkID string) (*Response, error) {
	path, err := checkPath(snapshotID, checkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// DeactivateAll disables every check on a snapshot.
func (s *ChecksService) DeactivateAll(ctx context.Context, snapshotID string) (*Response, error) {
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func checkPath(snapshotID, checkID string) (string, error) {
	base, err := checksPath(snapshotID)
	if err != nil {
		return "", err
	}
	if checkID = strings.TrimSpace(checkID); checkID == "" {
		return "", errors.New("forward: check ID is required")
	}
	return base + "/" + url.PathEscape(checkID), nil
}
