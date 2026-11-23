package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// SubnetConnectivity returns the connectivity comparison as Forward has it
// right now, which may be partial. Prefer WaitForSubnetConnectivity.
func (s *Diffs) SubnetConnectivity(ctx context.Context, beforeSnapshotID, afterSnapshotID string) (*ports.SubnetConnectivityDiff, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := diffsPath(beforeSnapshotID, afterSnapshotID, "subnet-connectivity")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Diffs.SubnetConnectivity")
	out := new(ports.SubnetConnectivityDiff)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ConnectivityDiffLocations returns per-location incoming and outgoing change
// counts (view=locations). Empty while the comparison is still partial.
func (s *Diffs) ConnectivityDiffLocations(ctx context.Context, beforeSnapshotID, afterSnapshotID string) ([]ports.LocationConnDiffStat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
		Stats []ports.LocationConnDiffStat `json:"stats"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Stats, resp, nil
}

// ConnectivityDiffLocationPair returns the changed subnet pairs from location
// sourceLocationID to destinationLocationID (view=locationPair&loc1=&loc2=).
func (s *Diffs) ConnectivityDiffLocationPair(
	ctx context.Context,
	beforeSnapshotID string,
	afterSnapshotID string,
	sourceLocationID string,
	destinationLocationID string,
) ([]ports.SubnetPairConnDiffStat, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
		Stats []ports.SubnetPairConnDiffStat `json:"stats"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out.Stats, resp, nil
}

// ConnectivityDiffLocationPairs returns, for one diff type (ConnectivityDiffAdded, Deleted or Modified), how many connectivity changes each
// pair of locations has (GET .../subnet-connectivity?view=locationPairs&type=[&loc=]; VIEW_CONNECTIVITY_DIFFS and the org property
// LOCATION_CONNECTIVITY_DIFFS, which only Forward support can set: an org admin gets the 403 ErrFeatureGated; advanced reachability must
// be on for the network). loc narrows it to pairs involving one location; empty reads all. While Forward is still computing the
// comparison the answer is an EMPTY list, which is not "no change" (seen live: the first ask answers {"counts":[]}, a later one the real
// answer), so use WaitConnectivityDiffLocationPairs unless you already waited with WaitForSubnetConnectivity. Preview: the shape is from
// Forward's source (LocationConnChangeCounts), the empty-while-computing behaviour was seen live.
func (s *Diffs) ConnectivityDiffLocationPairs(ctx context.Context, beforeSnapshotID, afterSnapshotID, diffType, locationID string) ([]ports.LocationPairChangeCount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if diffType != ports.ConnectivityDiffAdded && diffType != ports.ConnectivityDiffDeleted && diffType != ports.ConnectivityDiffModified {
		return nil, nil, fmt.Errorf("forward: diff type %q must be %s, %s or %s", diffType, ports.ConnectivityDiffAdded, ports.ConnectivityDiffDeleted, ports.ConnectivityDiffModified)
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
		Counts []ports.LocationPairChangeCount `json:"counts"`
	}
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	if out.Counts == nil {
		out.Counts = []ports.LocationPairChangeCount{}
	}
	return out.Counts, resp, nil
}
