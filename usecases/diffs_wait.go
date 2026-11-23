package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// WaitChecks reads the check diff of one type and, while any row is still PROCESSING, reads it again on the poll schedule, returning
// the rows once none is. Only the NQE type answers early with PROCESSING rows; the other types block in Forward until computed, so
// they return on the first read. Use PollOptions.Interval/MaxInterval for the pacing and Budget to bound the wait: running out
// returns the last rows with an error satisfying errors.Is(err, ErrPollBudget). Preview: the PROCESSING row shape is from Forward's
// DiffController.getImmediateDiffEntry and was seen live by Skyforge's regression sweep.
func (s *Diffs) WaitChecks(ctx context.Context, snapshotA, snapshotB, checkType string, options ports.PollOptions[[]domain.CheckDiffEntry]) ([]domain.CheckDiffEntry, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	rows, resp, err := s.Checks(ctx, snapshotA, snapshotB, checkType)
	if err != nil {
		return nil, resp, err
	}
	poller, err := NewPoller(&rows, func(ctx context.Context) (*[]domain.CheckDiffEntry, *ports.Response, error) {
		next, resp, err := s.Checks(ctx, snapshotA, snapshotB, checkType)
		if err != nil {
			return nil, resp, err
		}
		return &next, resp, nil
	}, func(rows *[]domain.CheckDiffEntry) (bool, error) { return !checkDiffProcessing(*rows), nil })
	if err != nil {
		return nil, resp, err
	}
	out, waitResp, err := poller.Wait(ctx, options)
	if waitResp != nil {
		resp = waitResp
	}
	if out == nil {
		return nil, resp, err
	}
	return *out, resp, err
}

func checkDiffProcessing(rows []domain.CheckDiffEntry) bool {
	for _, row := range rows {
		if strings.EqualFold(row.DiffType, domain.CheckDiffProcessing) {
			return true
		}
		for _, check := range []*domain.Check{row.A, row.B} {
			if check != nil && strings.EqualFold(check.Status, domain.CheckDiffProcessing) {
				return true
			}
		}
	}
	return false
}
