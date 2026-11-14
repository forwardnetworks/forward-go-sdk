package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TraceService reads the per-hop detail of a flow's path: the headers the packet
// has after each hop (so NAT and VLAN rewrites show), the network functions
// that processed it (L2, L3 routes, ACLs, NAT) with the configuration lines they
// came from, and the processing stages in order.
//
// PREVIEW / UNDOCUMENTED. These two routes are the GUI's own flow-trace calls.
// They are not in Forward's published OpenAPI set, Forward makes no stability
// promise for them, and their request and response shapes can change between
// builds (the GUI ships with the server, so it never sees a mismatch). Do not
// build anything durable on the exact field set; decode defensively and expect
// to revisit this file after a Forward upgrade. Use PathSearchRequest through
// Networks.Paths for anything that must stay stable.
//
// Verified against web/.../controller/TraceController.java (getTraceFacets and
// getTrace, both at the primary and stable pins; the controllers return a
// CompressedBytesPB that the server decompresses, so the wire is plain JSON),
// core/.../trace/FrontendTraceRequest.java, query/json/PathQuery.java and
// search/.../trace/json/Trace*Serializer.java, and then captured live against a
// demo snapshot (two flows plus an SNAT flow and a switch-only hop). Not checked:
// other vendors, ACL kinds that carry rule lines, static NAT or port
// translation, a predicted snapshot, loops, multicast or non-IPv4 flows.
//
// Source location differs from Networks.Paths: for a SrcIP that is a firewall's own interface address, the
// facets started the path on that firewall (dropped at its input ACL) while getPaths located the source as
// "self" and returned two security-denied paths through five devices (seen by forward-skills, live). Do not
// assume the two agree on where a flow starts; name the device with From when it matters.
//
// The trace routes need the VIEW_PATHS network permission and a snapshot
// processed to the reachability stage.
type TraceService service

// ErrTraceEmpty is returned by Trace when Forward found no flow for the selected path (or ran out of time looking).
var ErrTraceEmpty = errors.New("forward: the trace found no flow for the selected path")

// ErrTraceAmbiguousSource is returned by Explain when the source address could be at more than one place;
// set TraceQuery.From to choose.
var ErrTraceAmbiguousSource = errors.New("forward: the source address is at more than one location; set TraceQuery.From")

// TraceQuery is the flow to trace. Give SrcIP, From (a device name), or both;
// DstIP is required. With From alone the facets enumerate every source Forward
// can find on that device, which is how to discover paths without host
// addresses. IPProto, SrcPort and DstPort add header filters.
type TraceQuery struct {
	// SrcIP is a source address or prefix. With From also set, the address is resolved on that device.
	SrcIP string
	// From is the device the flow enters at.
	From string
	// DstIP is the destination address (required).
	DstIP string
	// IPProto is the IP protocol number, for example 6 for TCP.
	IPProto *int
	// SrcPort and DstPort are L4 ports; they need IPProto 6 or 17.
	SrcPort string
	DstPort string
}

func (q TraceQuery) validate() error {
	if strings.TrimSpace(q.DstIP) == "" {
		return errors.New("forward: a trace needs a destination ip")
	}
	if strings.TrimSpace(q.SrcIP) == "" && strings.TrimSpace(q.From) == "" {
		return errors.New("forward: a trace needs a source ip, a starting device, or both")
	}
	if strings.Contains(q.SrcIP, ":") || strings.Contains(q.DstIP, ":") {
		// The GUI sends ipv6_dst for an IPv6 destination; this wrapper only does IPv4, so refuse rather than send a wrong filter.
		return errors.New("forward: trace supports IPv4 flows only")
	}
	if (q.SrcPort != "" || q.DstPort != "") && (q.IPProto == nil || (*q.IPProto != 6 && *q.IPProto != 17)) {
		return errors.New("forward: a trace with ports needs ip protocol 6 or 17")
	}
	return nil
}

// filters renders the query as the GUI's PathQuery. Every header filter sits in
// from.headers, as the GUI does. A destination placed under "to" returned no
// paths in the live capture; the source suggests "to" constrains where the
// packet leaves the network, which is a different question.
func (q TraceQuery) filters() map[string]any {
	location := map[string]any{}
	if q.SrcIP != "" {
		location["type"] = "SubnetLocationFilter"
		location["value"] = q.SrcIP
		if q.From != "" {
			location["device"] = q.From
		}
	} else {
		location["type"] = "DeviceFilter"
		location["values"] = []string{q.From}
	}
	headers := []map[string]any{}
	add := func(field, value string) {
		headers = append(headers, map[string]any{"type": "PacketFilter", "values": map[string][]string{field: {value}}})
	}
	if q.IPProto != nil {
		add("ip_proto", strconv.Itoa(*q.IPProto))
	}
	if q.SrcPort != "" {
		add("tp_src", q.SrcPort)
	}
	if q.DstPort != "" {
		add("tp_dst", q.DstPort)
	}
	add("ipv4_dst", q.DstIP)
	return map[string]any{"from": map[string]any{"location": location, "headers": headers}}
}

// TraceFacetsOptions tunes a Facets call. The zero value uses the server's defaults.
type TraceFacetsOptions struct {
	// MaxResults caps the flows the facets are computed over (server default 5000, maximum enforced by Forward).
	MaxResults int
	// MaxFacetValues caps the values listed per facet (server default 50).
	MaxFacetValues int
}

// TraceFacets is the Facets response: the distinct paths the query takes,
// grouped by what happens to the traffic. Raw is the response body, unchanged.
type TraceFacets struct {
	PathGroups   []TracePathGroup    `json:"pathGroups"`
	Outcomes     map[string][]string `json:"outcomes,omitempty"`
	TotalResults json.RawMessage     `json:"totalResults,omitempty"`
	PagedResults int                 `json:"pagedResults,omitempty"`
	TimedOut     bool                `json:"timedOut,omitempty"`
	CoveredTopo  json.RawMessage     `json:"coveredTopo,omitempty"`
	// PossibleIPLocations is set INSTEAD of path groups when an address could be at more than one place
	// (the source for a TraceQuery, either side for a TracePathQuery): name the device with TraceQuery.From,
	// or use TraceSubnetAtDevice, and ask again. PossibleLocations decodes it.
	PossibleIPLocations json.RawMessage `json:"possibleIpLocations,omitempty"`
	// Raw is the whole response body, unchanged. The path groups and outcomes sit under its "facets" object.
	Raw json.RawMessage `json:"-"`
}

// TracePathGroup is the paths that share one trace type (VALID, DROPPED, BLACKHOLE, LOOP, ...).
type TracePathGroup struct {
	TraceType string           `json:"traceType"`
	Paths     []TracePathFacet `json:"paths"`
}

// TracePathFacet is one distinct device path. BehaviorSample must be sent back
// unchanged to select this path in Trace, so it is kept as raw JSON.
type TracePathFacet struct {
	Path                 []string        `json:"path"`
	GroupSignatureHops   []int           `json:"groupSignatureHops,omitempty"`
	OutToOverlayLinkHops []int           `json:"outToOverlayLinkHops,omitempty"`
	OverlaySignature     []int           `json:"overlaySignature,omitempty"`
	BehaviorSample       json.RawMessage `json:"behaviorSample,omitempty"`
}

// TraceSelection picks one path from a Facets response for Trace.
type TraceSelection struct {
	Path      []string
	TraceType string
	// Behavior is the path's BehaviorSample, unchanged.
	Behavior json.RawMessage
}

// Selection returns the TraceSelection for a path in a group of this response.
func (g TracePathGroup) Selection(i int) (TraceSelection, bool) {
	if i < 0 || i >= len(g.Paths) {
		return TraceSelection{}, false
	}
	return TraceSelection{Path: g.Paths[i].Path, TraceType: g.TraceType, Behavior: g.Paths[i].BehaviorSample}, true
}

// TraceResult is one path's trace. Raw is the response body, unchanged; the typed
// fields are a convenience over it and cover only what was observed.
type TraceResult struct {
	TraceType          string                    `json:"traceType"`
	Statefulness       string                    `json:"statefulness,omitempty"`
	Behavior           TraceBehavior             `json:"behavior"`
	IngressHeaders     map[string]map[string]any `json:"ingressHeaders,omitempty"`
	DeviceHops         []TraceHop                `json:"deviceHops"`
	GroupSignatureHops []int                     `json:"groupSignatureHops,omitempty"`
	PathDescriptor     string                    `json:"pathDescriptor,omitempty"`
	TimedOut           bool                      `json:"timedOut,omitempty"`
	MissingDevices     json.RawMessage           `json:"missingDevices,omitempty"`
	CoveredTopo        json.RawMessage           `json:"coveredTopo,omitempty"`
	Raw                json.RawMessage           `json:"-"`
}

// TraceBehavior lists, per hop, the processing stages the packet went through in
// order (for example INPUT, DNAT, IN_ACL, IPV4, SNAT, OUTPUT). ACLDropHops are
// the hop indexes where an ACL dropped it.
type TraceBehavior struct {
	ProcessingStages [][]string `json:"processingStages,omitempty"`
	ACLDropHops      []int      `json:"aclDropHops,omitempty"`
}

// TraceHop is one device on the path. Headers are the packet's values AFTER this
// hop, grouped by protocol (ETHERNET, IPV4, TCP, L7, ...); use IngressHeaders on
// the result for the packet entering the first hop. A value can be symbolic
// (a MAC of "host_mac", a NAT value of "Egress Interface IP"). OutPorts is
// absent on a hop that dropped the packet or ended the path.
type TraceHop struct {
	Device           string                        `json:"device"`
	InPorts          []string                      `json:"inPorts,omitempty"`
	OutPorts         []string                      `json:"outPorts,omitempty"`
	Headers          map[string]map[string]any     `json:"headers,omitempty"`
	NetworkFunctions map[string][]TraceNetworkFunc `json:"networkFunctions,omitempty"`
	// DropTables is in Forward\'s serializer comment but was never seen in a response; treat as unobserved.
	DropTables       json.RawMessage `json:"dropTables,omitempty"`
	OutToOverlayLink bool            `json:"outToOverlayLink,omitempty"`
}

// TraceNetworkFunc is one thing a device did to the packet: a route lookup
// (IP_ROUTE: the matched prefix in Matches["ipv4_dst"], VRF and route type in
// Attributes, next hop and out port in the actions), ARP, MAC forwarding, an
// interface input or output, an ACL (ACCESS_CONTROL: name and context, action
// PERMIT or DENY, often with no FileLines), or NAT (DNAT, SNAT).
type TraceNetworkFunc struct {
	Type string `json:"type"`
	// FileLines maps "<device>,<file>" to the line ranges of the evidence in that collected file.
	FileLines           map[string][]TraceLineRange `json:"fileLines,omitempty"`
	Attributes          map[string]any              `json:"attributes,omitempty"`
	Matches             map[string]any              `json:"matches,omitempty"`
	ActionBuckets       []TraceActionBucket         `json:"actionBuckets,omitempty"`
	ProcessingStageType string                      `json:"processingStageType,omitempty"`
}

// TraceLineRange is an inclusive line range in a collected file. Forward's LineRange is 0-based; add 1 for editor line numbers.
type TraceLineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// TraceActionBucket is one outcome of a function: field rewrites (SET_FIELD) or a verdict (PERMIT, DENY).
type TraceActionBucket struct {
	Attributes map[string]any `json:"attributes,omitempty"`
	Actions    []TraceAction  `json:"actions,omitempty"`
}

// TraceAction is one action of a bucket. For SET_FIELD, Field and Value name the header rewritten.
type TraceAction struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
	Value any    `json:"value,omitempty"`
}

func (s *TraceService) request(ctx context.Context, route, operation, networkID, snapshotID string, query url.Values, body map[string]any) (*http.Request, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	if snapshotID != "" {
		query.Set("snapshotId", snapshotID)
	}
	path := "/api/networks/" + url.PathEscape(networkID) + route
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return markOperation(req, operation), nil
}

// Facets lists the distinct paths a flow takes, grouped by trace type. An empty
// snapshotID uses the network's latest. A predicted snapshot id is accepted by
// Forward's snapshot resolution but was not run live here.
//
// This is a read sent as a POST, so a retry policy resends it only after a 429
// or 503 or a failure to connect.
func (s *TraceService) Facets(ctx context.Context, networkID, snapshotID string, q TraceQuery, opts TraceFacetsOptions) (*TraceFacets, *Response, error) {
	if err := q.validate(); err != nil {
		return nil, nil, err
	}
	return s.facets(ctx, networkID, snapshotID, q.filters(), opts, "Trace.Facets")
}

// facets sends a trace-facets request for an already built PathQuery and decodes the answer. Facets and FacetsQuery share it.
func (s *TraceService) facets(ctx context.Context, networkID, snapshotID string, filters map[string]any, opts TraceFacetsOptions, operation string) (*TraceFacets, *Response, error) {
	if opts.MaxResults < 0 || opts.MaxFacetValues < 0 {
		return nil, nil, errors.New("forward: trace limits must not be negative")
	}
	body := map[string]any{"filters": filters}
	if opts.MaxResults > 0 {
		body["maxResults"] = opts.MaxResults
	}
	query := url.Values{}
	if opts.MaxFacetValues > 0 {
		query.Set("maxFacetValues", strconv.Itoa(opts.MaxFacetValues))
	}
	req, err := s.request(ctx, "/trace-facets", operation, networkID, snapshotID, query, body)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.doRequired(req, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := &TraceFacets{Raw: raw}
	var wrapper struct {
		Facets json.RawMessage `json:"facets"`
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, resp, fmt.Errorf("forward: decode trace facets: %w", err)
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Facets) > 0 {
		var inner struct {
			PathGroups []TracePathGroup    `json:"pathGroups"`
			Outcomes   map[string][]string `json:"outcomes"`
		}
		if err := json.Unmarshal(wrapper.Facets, &inner); err != nil {
			return nil, resp, fmt.Errorf("forward: decode trace facets: %w", err)
		}
		out.PathGroups, out.Outcomes = inner.PathGroups, inner.Outcomes
	}
	return out, resp, nil
}

// Trace returns the per-hop detail of one path chosen from Facets, with the
// same query. An empty snapshotID uses the network's latest.
func (s *TraceService) Trace(ctx context.Context, networkID, snapshotID string, q TraceQuery, sel TraceSelection) (*TraceResult, *Response, error) {
	if err := q.validate(); err != nil {
		return nil, nil, err
	}
	if len(sel.Path) == 0 || strings.TrimSpace(sel.TraceType) == "" {
		return nil, nil, errors.New("forward: a trace needs the path and trace type from a facets response")
	}
	picked := map[string]any{"path": sel.Path, "traceType": sel.TraceType}
	if len(sel.Behavior) > 0 {
		picked["behavior"] = sel.Behavior
	}
	body := map[string]any{
		"filters":        q.filters(),
		"selectedFacets": map[string]any{"paths": []any{picked}},
		"limit":          100,
	}
	req, err := s.request(ctx, "/trace", "Trace.Trace", networkID, snapshotID, url.Values{}, body)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.doRequired(req, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := &TraceResult{Raw: raw}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, resp, fmt.Errorf("forward: decode trace: %w", err)
	}
	if out.TraceType == "" && len(out.DeviceHops) == 0 {
		// Forward answers {"timedOut":..} alone when it finds no flow for the selection.
		return nil, resp, fmt.Errorf("%w (timed out: %t)", ErrTraceEmpty, out.TimedOut)
	}
	return out, resp, nil
}

// TracedPath is one path of an Explain: where it came from in the facets and its trace.
type TracedPath struct {
	// ID is "g<group>.p<path>", stable only within one Explain response.
	ID         string   `json:"id"`
	Group      int      `json:"group"`
	Index      int      `json:"index"`
	TraceType  string   `json:"traceType"`
	Path       []string `json:"path"`
	Descriptor string   `json:"descriptor,omitempty"`
	// Trace is nil when Error is set.
	Trace *TraceResult `json:"trace,omitempty"`
	Error string       `json:"error,omitempty"`
	// Err is the typed error behind Error.
	Err error `json:"-"`
}

// TraceExplanation is what Explain found, with the reasons it can be incomplete.
type TraceExplanation struct {
	Paths []TracedPath `json:"paths"`
	// Truncated is true when more than ExplainLimit paths were listed and the rest were not traced.
	Truncated bool `json:"truncated"`
	// FacetsTimedOut is true when Forward ran out of time listing paths, so more may exist.
	FacetsTimedOut bool `json:"facetsTimedOut"`
	// ResultsLowerBound is true when Forward capped the flows it examined, so the path list may be partial.
	ResultsLowerBound bool `json:"resultsLowerBound"`
}

// ExplainLimit is the most paths Explain traces, so a flow with a very wide
// fan-out does not turn into thousands of requests.
const ExplainLimit = 50

// Explain runs Facets and then Trace for every path it lists, up to ExplainLimit.
// Forward groups paths by device path, so parallel links between the same two
// devices and different behaviours on one device path appear as ONE entry (only
// its sample behaviour is traced); alternatives through different devices each
// get their own. It returns ErrTraceAmbiguousSource when the source could be at
// several places. A path whose trace fails is reported on its own entry; an
// error that would fail every path (no permission, unknown network, route not
// served, rate limit) or a cancelled context fails the call.
func (s *TraceService) Explain(ctx context.Context, networkID, snapshotID string, q TraceQuery) (*TraceExplanation, *Response, error) {
	facets, resp, err := s.Facets(ctx, networkID, snapshotID, q, TraceFacetsOptions{})
	if err != nil {
		return nil, resp, err
	}
	out := &TraceExplanation{FacetsTimedOut: facets.TimedOut}
	if len(facets.TotalResults) > 0 {
		var total struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(facets.TotalResults, &total)
		out.ResultsLowerBound = total.Type == "LOWER_BOUND"
	}
	if len(facets.PathGroups) == 0 && len(facets.PossibleIPLocations) > 0 && string(facets.PossibleIPLocations) != "null" {
		return nil, resp, fmt.Errorf("%w: %s", ErrTraceAmbiguousSource, facets.PossibleIPLocations)
	}
	for gi, g := range facets.PathGroups {
		for pi := range g.Paths {
			if len(out.Paths) >= ExplainLimit {
				out.Truncated = true
				return out, resp, nil
			}
			sel, _ := g.Selection(pi)
			tp := TracedPath{ID: fmt.Sprintf("g%d.p%d", gi, pi), Group: gi, Index: pi, TraceType: g.TraceType, Path: sel.Path}
			trace, _, terr := s.Trace(ctx, networkID, snapshotID, q, sel)
			if terr != nil {
				if ctx.Err() != nil {
					return out, resp, ctx.Err()
				}
				if IsStatus(terr, http.StatusUnauthorized) || IsStatus(terr, http.StatusForbidden) || IsStatus(terr, http.StatusNotFound) ||
					IsStatus(terr, http.StatusTooManyRequests) || errors.Is(terr, ErrEndpointNotServed) {
					return out, resp, terr
				}
				tp.Err, tp.Error = terr, terr.Error()
			} else {
				tp.Trace, tp.Descriptor = trace, trace.PathDescriptor
			}
			out.Paths = append(out.Paths, tp)
		}
	}
	return out, resp, nil
}
