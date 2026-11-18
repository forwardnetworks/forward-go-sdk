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
// views below are empty until it has returned. Once it has returned, an empty result is a real answer: Forward compares only the
// subnet pairs a route, ACL or NAT difference could affect, so no pairs means no forwarding change (see TotalSubnetPairs).

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
	// TotalSubnetPairs is how many pairs Forward compares, and a settled zero does NOT mean "nothing was compared because there are no
	// locations". Forward builds only the pairs a route, ACL or NAT difference between the two snapshots could affect
	// (LocalDiffComputer.generateDeviceSubnetPairs: no pairs when there are no device subnets or no impacted subnets), so zero pairs once
	// IsPartialResult is false means the comparison found no forwarding change; it was also zero locations only in the degenerate case of no
	// device subnets at all. The AutoCon6 workshop saw it live on 26.10.6: an empty route diff gave 0 pairs and six route changes gave 42
	// (reported by forward-python-sdk). An all-zero summary with IsPartialResult true is still "not finished".
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

// Diff types the location-pair counts take (Forward's DiffType, restricted by DiffController to these three; ALL is a 400 "No enum constant
// ...DiffType.ALL", seen live by the AutoCon6 workshop).
const (
	ConnectivityDiffAdded    = "ADDED"
	ConnectivityDiffDeleted  = "DELETED"
	ConnectivityDiffModified = "MODIFIED"
)

// LocationPairChangeCount is the number of connectivity changes of one kind from location Src to location Dst
// (LocationConnChangeCount).
type LocationPairChangeCount struct {
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Count int    `json:"count"`
}

// ConnectivityDiffLocationPairs returns, for one diff type (ConnectivityDiffAdded, Deleted or Modified), how many connectivity changes each
// pair of locations has (GET .../subnet-connectivity?view=locationPairs&type=[&loc=]; VIEW_CONNECTIVITY_DIFFS and the org property
// LOCATION_CONNECTIVITY_DIFFS, which only Forward support can set: an org admin gets the 403 ErrFeatureGated; advanced reachability must
// be on for the network). loc narrows it to pairs involving one location; empty reads all. While Forward is still computing the
// comparison the answer is an EMPTY list, which is not "no change" (seen live: the first ask answers {"counts":[]}, a later one the real
// answer), so use WaitConnectivityDiffLocationPairs unless you already waited with WaitForSubnetConnectivity. Preview: the shape is from
// Forward's source (LocationConnChangeCounts), the empty-while-computing behaviour was seen live.
func (s *DiffsService) ConnectivityDiffLocationPairs(ctx context.Context, beforeSnapshotID, afterSnapshotID, diffType, locationID string) ([]LocationPairChangeCount, *Response, error) {
	if diffType != ConnectivityDiffAdded && diffType != ConnectivityDiffDeleted && diffType != ConnectivityDiffModified {
		return nil, nil, fmt.Errorf("forward: diff type %q must be %s, %s or %s", diffType, ConnectivityDiffAdded, ConnectivityDiffDeleted, ConnectivityDiffModified)
	}
	path, err := diffsPath(beforeSnapshotID, afterSnapshotID, "subnet-connectivity")
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{"view": []string{"locationPairs"}, "type": []string{diffType}}
	if locationID = strings.TrimSpace(locationID); locationID != "" {
		query.Set("loc", locationID)
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.ConnectivityDiffLocationPairs")
	var out struct {
		Counts []LocationPairChangeCount `json:"counts"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Counts == nil {
		out.Counts = []LocationPairChangeCount{}
	}
	return out.Counts, resp, nil
}

// WaitConnectivityDiffLocationPairs waits for the connectivity comparison to finish (WaitForSubnetConnectivity, with the same options and
// the same ErrConnectivityDiffPartial on timeout) and then reads ConnectivityDiffLocationPairs, so an empty answer means no change.
func (s *DiffsService) WaitConnectivityDiffLocationPairs(ctx context.Context, beforeSnapshotID, afterSnapshotID, diffType, locationID string, options ConnectivityWaitOptions) ([]LocationPairChangeCount, *Response, error) {
	if _, resp, err := s.WaitForSubnetConnectivity(ctx, beforeSnapshotID, afterSnapshotID, options); err != nil {
		return nil, resp, err
	}
	return s.ConnectivityDiffLocationPairs(ctx, beforeSnapshotID, afterSnapshotID, diffType, locationID)
}
