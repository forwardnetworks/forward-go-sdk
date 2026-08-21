package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ChecksService manages checks attached to snapshots. Checks are not created on
// a network resource: they attach to one real snapshot. When created with
// CreatePersistent, Forward carries them onto later real and predicted snapshots
// of that network.
type ChecksService service

// Check is one evaluated check on a particular snapshot.
//
// Enabled is a pointer because a check that never stated it and one that
// stated it false are different: a caller reconciling declared configuration
// has to leave the first alone. The same holds for NewCheck.Enabled, where
// omitting the field defers to the appserver's default rather than asserting
// one here.
type Check struct {
	ID            Identifier `json:"id,omitempty"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	NumViolations int        `json:"numViolations"`
	Enabled       *bool      `json:"enabled,omitempty"`
	Priority      string     `json:"priority,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
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

// List returns checks evaluated on snapshotID. The first GET primes Forward's
// evaluation cache and can outlive an http.Client deadline while computation
// continues server-side, so client-side timeout failures are re-issued up to
// four times. Cancellation of the caller's context is never retried.
func (s *ChecksService) List(ctx context.Context, snapshotID string) ([]Check, *Response, error) {
	if err := s.client.requireCapability(CapabilityPersistentSnapshotChecks); err != nil {
		return nil, nil, err
	}
	path, err := checksPath(snapshotID)
	if err != nil {
		return nil, nil, err
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
