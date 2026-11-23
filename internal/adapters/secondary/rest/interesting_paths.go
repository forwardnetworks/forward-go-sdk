package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var interestingPathsKinds = map[ports.InterestingPathsKind]bool{
	ports.InterestingPathsFirewall: true, ports.InterestingPathsLoadBalancer: true, ports.InterestingPathsIPMPLSBlackhole: true, ports.InterestingPathsIPMPLSDrop: true,
	ports.InterestingPathsL2Storm: true, ports.InterestingPathsForwardingDomainBlackhole: true, ports.InterestingPathsNonStaticL3Loop: true,
}

// InterestingPaths runs Forward's interesting-path search on a snapshot (GET /api/networks/{id}/interesting-path-queries?snapshotId=&type=
// &maxSeconds=, InterestingPathsController; the UI's "View interesting paths"). Read-only as far as network data goes (it caches its
// result and records a usage event), but SYNCHRONOUS and expensive: Forward samples hosts and searches flows on the backend, and the call
// blocks until it is done or maxSeconds passes. maxSeconds is a soft deadline: Forward returns what it has found, and a run can overshoot
// it by a search or two. maxSeconds 0 sends nothing, so Forward's own default of 300 seconds applies (the SDK never sends 0: Forward fails
// on it); the HTTP timeout is maxSeconds plus 90 seconds, and at most 3600 seconds is accepted here. The job is reusable and cached per
// snapshot, type and maxSeconds, and the sampling is deterministic, so calling again after a client timeout returns the stored result
// cheaply. Needs the network operation VIEW_PATHS (licence tier B gets 403 unlicensed-operation) and a snapshot processed through its
// advanced-reachability stage: otherwise Forward answers 400 "Advanced reachability <STATE> for snapshot ...", typed here as
// ErrSnapshotNotProcessed (or ErrSnapshotProcessingFailed for a canceled, timed-out or failed run); start it with
// Snapshots.ComputeAdvancedReachability. Forward would pick the latest snapshot if none were named; this SDK requires one.
func (s *Networks) InterestingPaths(ctx context.Context, networkID, snapshotID string, kind ports.InterestingPathsKind, maxSeconds int) (*ports.InterestingPaths, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, nil, errors.New("forward: snapshot ID is required")
	}
	if !interestingPathsKinds[kind] {
		return nil, nil, fmt.Errorf("forward: %q is not an interesting-paths type (FIREWALL, LOADBALANCER, IP_MPLS_BLACKHOLE, IP_MPLS_DROP, L2_STORM, FORWARDING_DOMAIN_BLACKHOLE, NON_STATIC_L3_LOOP)", kind)
	}
	if maxSeconds < 0 || maxSeconds > 3600 {
		return nil, nil, errors.New("forward: maxSeconds must be between 0 and 3600")
	}
	query := url.Values{"snapshotId": []string{snapshotID}, "type": []string{string(kind)}}
	timeout := 390 * time.Second
	if maxSeconds > 0 {
		query.Set("maxSeconds", strconv.Itoa(maxSeconds))
		timeout = time.Duration(maxSeconds)*time.Second + 90*time.Second
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/interesting-path-queries?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.InterestingPaths")
	out := new(ports.InterestingPaths)
	resp, err := s.client.doWithTimeout(req, out, false, nil, timeout)
	return out, resp, err
}
