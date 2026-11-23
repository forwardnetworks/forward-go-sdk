package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"regexp"
	"strings"
)

// ForScoring returns a non-empty CheckSet or ErrNoChecks. Consumers should use
// this method—not List—before computing pass/fail totals, so a workspace that
// never received the persistent corpus cannot produce a vacuous pass.
func (s *Checks) ForScoring(
	ctx context.Context,
	snapshotID string,
	requirements ...domain.CheckRequirements,
) (domain.CheckSet, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return domain.CheckSet{}, nil, err
	}
	if len(requirements) > 1 {
		return domain.CheckSet{}, nil, errors.New("forward: at most one check requirement set is allowed")
	}
	checks, response, err := s.List(ctx, snapshotID)
	if err != nil {
		return domain.CheckSet{}, response, err
	}
	if len(checks) == 0 {
		return domain.CheckSet{}, response, &domain.NoChecksError{SnapshotID: strings.TrimSpace(snapshotID)}
	}
	required := domain.CheckRequirements{MinChecks: 1}
	if len(requirements) == 1 {
		required = requirements[0]
		if required.MinChecks <= 0 {
			required.MinChecks = 1
		}
	}
	missing, err := missingCheckRequirements(checks, required)
	if err != nil {
		return domain.CheckSet{}, response, err
	}
	if len(missing) != 0 {
		return domain.CheckSet{}, response, &domain.IncompleteCheckCorpusError{
			SnapshotID: strings.TrimSpace(snapshotID),
			Missing:    missing,
		}
	}
	return domain.NewCheckSet(strings.TrimSpace(snapshotID), checks), response, nil
}

// ExistingNames returns active check names for idempotent corpus promotion.
func (s *Checks) ExistingNames(ctx context.Context, snapshotID string) (map[string]bool, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
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

func missingCheckRequirements(checks []domain.Check, required domain.CheckRequirements) ([]string, error) {
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
