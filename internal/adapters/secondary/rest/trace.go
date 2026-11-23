package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type Trace service

func (s *Trace) request(ctx context.Context, route, operation, networkID, snapshotID string, query url.Values, body map[string]any) (*http.Request, error) {
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
func (s *Trace) Facets(ctx context.Context, networkID, snapshotID string, q ports.TraceQuery, opts ports.TraceFacetsOptions) (*ports.TraceFacets, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, nil, err
	}
	return s.facets(ctx, networkID, snapshotID, q.Filters(), opts, "Trace.Facets")
}

// facets sends a trace-facets request for an already built PathQuery and decodes the answer. Facets and FacetsQuery share it.
func (s *Trace) facets(ctx context.Context, networkID, snapshotID string, filters map[string]any, opts ports.TraceFacetsOptions, operation string) (*ports.TraceFacets, *ports.Response, error) {
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
	out := &ports.TraceFacets{Raw: raw}
	var wrapper struct {
		Facets json.RawMessage `json:"facets"`
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, resp, fmt.Errorf("forward: decode trace facets: %w", err)
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Facets) > 0 {
		var inner struct {
			PathGroups []ports.TracePathGroup `json:"pathGroups"`
			Outcomes   map[string][]string    `json:"outcomes"`
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
func (s *Trace) Trace(ctx context.Context, networkID, snapshotID string, q ports.TraceQuery, sel ports.TraceSelection) (*ports.TraceResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, nil, err
	}
	return s.trace(ctx, networkID, snapshotID, q.Filters(), sel, "Trace.Trace")
}

// trace sends a trace request for an already built PathQuery and one selected path. Trace and TraceFor share it.
func (s *Trace) trace(ctx context.Context, networkID, snapshotID string, filters map[string]any, sel ports.TraceSelection, operation string) (*ports.TraceResult, *ports.Response, error) {
	if len(sel.Path) == 0 || strings.TrimSpace(sel.TraceType) == "" {
		return nil, nil, errors.New("forward: a trace needs the path and trace type from a facets response")
	}
	picked := map[string]any{"path": sel.Path, "traceType": sel.TraceType}
	if len(sel.Behavior) > 0 {
		picked["behavior"] = sel.Behavior
	}
	body := map[string]any{
		"filters":        filters,
		"selectedFacets": map[string]any{"paths": []any{picked}},
		"limit":          100,
	}
	req, err := s.request(ctx, "/trace", operation, networkID, snapshotID, url.Values{}, body)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.doRequired(req, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := &ports.TraceResult{Raw: raw}
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, resp, fmt.Errorf("forward: decode trace: %w", err)
	}
	if out.TraceType == "" && len(out.DeviceHops) == 0 {
		// Forward answers {"timedOut":..} alone when it finds no flow for the selection.
		return nil, resp, fmt.Errorf("%w (timed out: %t)", ports.ErrTraceEmpty, out.TimedOut)
	}
	return out, resp, nil
}
