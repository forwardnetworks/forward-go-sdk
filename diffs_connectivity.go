package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
// views below are empty until it has returned.

// SubnetConnectivityDiff is Forward's SubnetConnDiffSummary.
type SubnetConnectivityDiff struct {
	NewlyConnectedSubnetPairs        int64 `json:"newlyConnectedSubnetPairs"`
	NewlyConnectedImpactedLocations  int64 `json:"newlyConnectedImpactedLocations"`
	NewlyConnectedDestinationSubnets int64 `json:"newlyConnectedDestinationSubnets"`
	NewlyIsolatedSubnetPairs         int64 `json:"newlyIsolatedSubnetPairs"`
	NewlyIsolatedImpactedLocations   int64 `json:"newlyIsolatedImpactedLocations"`
	NewlyIsolatedDestinationSubnets  int64 `json:"newlyIsolatedDestinationSubnets"`
	ModifiedSubnetPairs              int64 `json:"modifiedSubnetPairs"`
	ModifiedImpactedLocations        int64 `json:"modifiedImpactedLocations"`
	// EvaluatedSubnetPairs is how many pairs have been compared so far.
	EvaluatedSubnetPairs int64 `json:"evaluatedSubnetPairs"`
	// TotalSubnetPairs separates the two readings of a settled zero: zero
	// pairs means nothing was compared (no locations), not that nothing
	// changed.
	TotalSubnetPairs       int64 `json:"totalSubnetPairs"`
	TotalImpactedLocations int64 `json:"totalImpactedLocations"`
	IsPartialResult        bool  `json:"isPartialResult"`
}

// ConnChangeStats counts connectivity changes in one direction.
type ConnChangeStats struct {
	NewlyConnected int64 `json:"newlyConnected"`
	NewlyIsolated  int64 `json:"newlyIsolated"`
	Modified       int64 `json:"modified"`
}

// Any reports whether any change is counted.
func (s ConnChangeStats) Any() bool {
	return s.NewlyConnected != 0 || s.NewlyIsolated != 0 || s.Modified != 0
}

// LocationConnDiffStat is one location's incoming and outgoing changes
// (Forward's LocationConnDiffStats).
type LocationConnDiffStat struct {
	LocationID Identifier      `json:"locationId"`
	Incoming   ConnChangeStats `json:"incoming"`
	Outgoing   ConnChangeStats `json:"outgoing"`
}

// SubnetPairConnDiffStat is one changed subnet pair between two locations
// (Forward's SubnetConnDiffStats).
type SubnetPairConnDiffStat struct {
	Src   string          `json:"src"`
	Dst   string          `json:"dst"`
	Stats ConnChangeStats `json:"stats"`
}

// ErrConnectivityDiffPartial is matched by the error WaitForSubnetConnectivity
// returns when the comparison was still partial at the deadline.
var ErrConnectivityDiffPartial = errors.New("forward: subnet connectivity comparison still partial")

// ConnectivityDiffPartialError carries the last partial result, so a caller
// can say how far the computation got.
type ConnectivityDiffPartialError struct {
	BeforeSnapshotID string
	AfterSnapshotID  string
	Waited           time.Duration
	Partial          *SubnetConnectivityDiff
}

func (e *ConnectivityDiffPartialError) Error() string {
	if e == nil {
		return ErrConnectivityDiffPartial.Error()
	}
	var evaluated, total int64
	if e.Partial != nil {
		evaluated, total = e.Partial.EvaluatedSubnetPairs, e.Partial.TotalSubnetPairs
	}
	return fmt.Sprintf("%s: %s -> %s after %s (%d of %d pairs evaluated)",
		ErrConnectivityDiffPartial, e.BeforeSnapshotID, e.AfterSnapshotID, e.Waited, evaluated, total)
}

func (e *ConnectivityDiffPartialError) Is(target error) bool {
	return target == ErrConnectivityDiffPartial
}

// ConnectivityWaitOptions bounds WaitForSubnetConnectivity. Zero values mean a
// five-minute timeout and a two-second poll interval.
type ConnectivityWaitOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
}

const (
	defaultConnectivityWaitTimeout  = 5 * time.Minute
	defaultConnectivityPollInterval = 2 * time.Second
)

// SubnetConnectivity returns the connectivity comparison as Forward has it
// right now, which may be partial. Prefer WaitForSubnetConnectivity.
func (s *DiffsService) SubnetConnectivity(ctx context.Context, beforeSnapshotID, afterSnapshotID string) (*SubnetConnectivityDiff, *Response, error) {
	path, err := diffsPath(beforeSnapshotID, afterSnapshotID, "subnet-connectivity")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.SubnetConnectivity")
	out := new(SubnetConnectivityDiff)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// WaitForSubnetConnectivity polls SubnetConnectivity until IsPartialResult is
// false. On timeout it returns a *ConnectivityDiffPartialError (errors.Is
// ErrConnectivityDiffPartial) carrying the last partial result; a caller
// cancellation returns the context's error.
func (s *DiffsService) WaitForSubnetConnectivity(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	options ConnectivityWaitOptions,
) (*SubnetConnectivityDiff, *Response, error) {
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
			return nil, resp, &ConnectivityDiffPartialError{
				BeforeSnapshotID: strings.TrimSpace(beforeSnapshotID),
				AfterSnapshotID:  strings.TrimSpace(afterSnapshotID),
				Waited:           time.Since(started).Round(time.Millisecond),
				Partial:          result,
			}
		}
		if err := waitOperationInterval(ctx, interval); err != nil {
			return nil, resp, err
		}
	}
}

// ConnectivityDiffLocations returns per-location incoming and outgoing change
// counts (view=locations). Empty while the comparison is still partial.
func (s *DiffsService) ConnectivityDiffLocations(ctx context.Context, beforeSnapshotID, afterSnapshotID string) ([]LocationConnDiffStat, *Response, error) {
	path, err := diffsPath(beforeSnapshotID, afterSnapshotID, "subnet-connectivity")
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"view": []string{"locations"}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.ConnectivityDiffLocations")
	var out struct {
		Stats []LocationConnDiffStat `json:"stats"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Stats, resp, nil
}

// ConnectivityDiffLocationPair returns the changed subnet pairs from location
// sourceLocationID to destinationLocationID (view=locationPair&loc1=&loc2=).
func (s *DiffsService) ConnectivityDiffLocationPair(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	sourceLocationID string,
	destinationLocationID string,
) ([]SubnetPairConnDiffStat, *Response, error) {
	path, err := diffsPath(beforeSnapshotID, afterSnapshotID, "subnet-connectivity")
	if err != nil {
		return nil, nil, err
	}
	sourceLocationID = strings.TrimSpace(sourceLocationID)
	destinationLocationID = strings.TrimSpace(destinationLocationID)
	if sourceLocationID == "" || destinationLocationID == "" {
		return nil, nil, errors.New("forward: source and destination location IDs are required")
	}
	query := url.Values{"view": []string{"locationPair"}, "loc1": []string{sourceLocationID}, "loc2": []string{destinationLocationID}}
	path += "?" + query.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.ConnectivityDiffLocationPair")
	var out struct {
		Stats []SubnetPairConnDiffStat `json:"stats"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Stats, resp, nil
}
