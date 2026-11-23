package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
	"time"
)

// Subnet-connectivity diff: how site-to-site reachability changed between two
// snapshots (DiffController, GET /api/diffs/{a}/{b}/subnet-connectivity and its
// view= variants). Gated on the LOCATION_CONNECTIVITY_DIFFS org property and
// the snapshots' REACHABILITY processing stage.
//
// Forward computes the comparison asynchronously on first ask. A read that
// lands before the computation has started returns every count as zero with
// IsPartialResult true: those zeros mean "not finished", not "nothing
// changed". WaitForSubnetConnectivity polls past them, and the per-location
// views below are empty until it has returned. Once it has returned, an empty result is a real answer: Forward compares only the
// subnet pairs a route, ACL or NAT difference could affect, so no pairs means no forwarding change (see TotalSubnetPairs).

const (
	defaultConnectivityWaitTimeout  = 5 * time.Minute
	defaultConnectivityPollInterval = 2 * time.Second
)

// WaitForSubnetConnectivity polls SubnetConnectivity until IsPartialResult is
// false. On timeout it returns a *ConnectivityDiffPartialError (errors.Is
// ErrConnectivityDiffPartial) carrying the last partial result; a caller
// cancellation returns the context's error.
func (s *Diffs) WaitForSubnetConnectivity(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	options domain.ConnectivityWaitOptions,
) (*domain.SubnetConnectivityDiff, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultConnectivityWaitTimeout
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultConnectivityPollInterval
	}
	started := time.Now()
	for {
		result, resp, err := s.SubnetConnectivity(ctx, beforeSnapshotID, afterSnapshotID)
		if err != nil {
			return nil, resp, err
		}
		if !result.IsPartialResult {
			return result, resp, nil
		}
		if time.Since(started)+interval > timeout {
			return nil, resp, &domain.ConnectivityDiffPartialError{
				BeforeSnapshotID: strings.TrimSpace(beforeSnapshotID),
				AfterSnapshotID:  strings.TrimSpace(afterSnapshotID),
				Waited:           time.Since(started).Round(time.Millisecond),
				Partial:          result,
			}
		}
		if err := domain.WaitInterval(ctx, interval); err != nil {
			return nil, resp, err
		}
	}
}

// WaitConnectivityDiffLocationPairs waits for the connectivity comparison to finish (WaitForSubnetConnectivity, with the same options and
// the same ErrConnectivityDiffPartial on timeout) and then reads ConnectivityDiffLocationPairs, so an empty answer means no change.
func (s *Diffs) WaitConnectivityDiffLocationPairs(ctx context.Context, beforeSnapshotID, afterSnapshotID, diffType, locationID string, options domain.ConnectivityWaitOptions) ([]domain.LocationPairChangeCount, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if _, resp, err := s.WaitForSubnetConnectivity(ctx, beforeSnapshotID, afterSnapshotID, options); err != nil {
		return nil, resp, err
	}
	return s.ConnectivityDiffLocationPairs(ctx, beforeSnapshotID, afterSnapshotID, diffType, locationID)
}
