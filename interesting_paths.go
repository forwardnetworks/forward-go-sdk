package forward

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// InterestingPathsKind picks what a snapshot's interesting-path search looks for. FIREWALL and LOADBALANCER rank sampled host-to-host
// paths that go through the network's firewalls or load balancers; the others find suspicious paths (blackholes, drops, storms,
// loops), which the UI lists separately.
type InterestingPathsKind string

const (
	InterestingPathsFirewall                  InterestingPathsKind = "FIREWALL"
	InterestingPathsLoadBalancer              InterestingPathsKind = "LOADBALANCER"
	InterestingPathsIPMPLSBlackhole           InterestingPathsKind = "IP_MPLS_BLACKHOLE"
	InterestingPathsIPMPLSDrop                InterestingPathsKind = "IP_MPLS_DROP"
	InterestingPathsL2Storm                   InterestingPathsKind = "L2_STORM"
	InterestingPathsForwardingDomainBlackhole InterestingPathsKind = "FORWARDING_DOMAIN_BLACKHOLE"
	InterestingPathsNonStaticL3Loop           InterestingPathsKind = "NON_STATIC_L3_LOOP"
)

var interestingPathsKinds = map[InterestingPathsKind]bool{
	InterestingPathsFirewall: true, InterestingPathsLoadBalancer: true, InterestingPathsIPMPLSBlackhole: true, InterestingPathsIPMPLSDrop: true,
	InterestingPathsL2Storm: true, InterestingPathsForwardingDomainBlackhole: true, InterestingPathsNonStaticL3Loop: true,
}

// InterestingPath is one ranked path. Score is Forward's rudimentary rank, practically 0 to 100 with no upper bound. For a FIREWALL or
// LOADBALANCER search it is one point per device hop, plus points per processing stage the path goes through (an ACL stage 2; MPLS,
// NAT-translation, VXLAN and FabricPath stages 4; policy-based routing, EPG and autoroute stages 12), plus 3 each when a destination or
// source translation actually happens (LoadbalancerFirewallPathFinder). The suspicious kinds score differently: the blackhole and
// drop kinds report how many flows end at that device, loops sum per loop, and L2 storms always score 1.
//
// Do NOT rely on the order. Forward computes in slices of up to 60 seconds and joins the slices unordered, so a run past one slice is
// not sorted; sort by Score yourself. Only the blackhole and drop kinds keep one path per terminal device.
type InterestingPath struct {
	// ID depends on the type: the stable path (FIREWALL, LOADBALANCER), a device id (blackhole and drop kinds) or a hash of the loop's
	// devices (loops).
	ID string `json:"id"`
	// SearchPath is a link into Forward's web application (a "stable path": /{networkId}/{snapshotId}/paths/{base64url path query}),
	// not an API route. PathQuery decodes the path query out of it.
	SearchPath      string          `json:"searchPath"`
	Score           int             `json:"score"`
	SamplePathGroup SamplePathGroup `json:"samplePathGroup"`
}

// SamplePathGroup describes the sampled path: the processing stages it goes through as Forward's ProcessingStageType names (tags such as
// IN_ACL, OUT_ACL, XLATE_DNAT, MPLS; there is no FIREWALL or LOADBALANCER tag, so filter on the stages), how many device hops it has, and
// its flow type (VALID, LOOP, BLACKHOLE, DROPPED, ...).
type SamplePathGroup struct {
	Tags     []string `json:"tags"`
	NumHops  int      `json:"numHops"`
	FlowType string   `json:"flowType"`
}

// PathQuery decodes the path query embedded in SearchPath (Forward's PathQuery JSON, the question the UI would ask), for use in a
// path search. It returns an error if SearchPath is not a paths link.
func (p InterestingPath) PathQuery() (json.RawMessage, error) {
	parts := strings.Split(strings.Trim(p.SearchPath, "/"), "/")
	if len(parts) != 4 || parts[2] != "paths" {
		return nil, fmt.Errorf("forward: %q is not a path-search link", p.SearchPath)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[3], "="))
	if err != nil {
		return nil, fmt.Errorf("forward: path query in %q is not base64url: %w", p.SearchPath, err)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("forward: path query in %q is not JSON", p.SearchPath)
	}
	return raw, nil
}

// InterestingPaths is the search result: the paths found and how long Forward computed (Duration, as Forward's JSON writer gives it:
// a number of milliseconds today, the sum of the slice compute times rather than wall time; the SDK does not interpret it). Forward gives
// no flag saying whether the run was cut short by maxSeconds.
type InterestingPaths struct {
	Paths    []InterestingPath `json:"interestingPathQueries"`
	Duration json.RawMessage   `json:"duration,omitempty"`
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
func (s *NetworksService) InterestingPaths(ctx context.Context, networkID, snapshotID string, kind InterestingPathsKind, maxSeconds int) (*InterestingPaths, *Response, error) {
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
	out := new(InterestingPaths)
	resp, err := s.client.doWithTimeout(req, out, false, nil, timeout)
	return out, resp, err
}
