package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// DiffSource runs one query (inline source or a library QueryID, with its Parameters) on both snapshots with RunAll and diffs the
// rows by options.KeyColumn. Forward's own POST /api/nqe-diffs/{before}/{after} takes only a library queryId (NqeDiffRequest has no
// source field), so a query written in source, or one that needs parameters Forward's diff does not take, is diffed here instead; this
// reads every row of both snapshots (one request per page) and compares in memory, so size options.MaxRows to what you can hold. Both
// reads keep RunAll's consistency checks, and a read that cannot be made consistent fails rather than diffing a partial set.
func (s *NQE) DiffSource(ctx context.Context, networkID, beforeSnapshotID, afterSnapshotID string, query domain.NQEQueryRequest, options domain.NQEPagedOptions) ([]domain.NQEDiffEntry, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	before, resp, err := s.RunAll(ctx, networkID, beforeSnapshotID, query, options)
	if err != nil {
		return nil, resp, err
	}
	after, resp, err := s.RunAll(ctx, networkID, afterSnapshotID, query, options)
	if err != nil {
		return nil, resp, err
	}
	diff, err := domain.DiffNQERows(before, after, options.KeyColumn)
	return diff, resp, err
}
