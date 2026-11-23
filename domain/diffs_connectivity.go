package domain

import (
	"errors"
	"fmt"
	"time"
)

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
