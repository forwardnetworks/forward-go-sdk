package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A typed path query for Trace.FacetsQuery: the GUI Search page's PathQuery (core/.../query/json/PathQuery.java at origin/master) with
// locations, through hops, header filters and the PERMIT_ALL mode, where TraceQuery covers one IPv4 flow. PREVIEW, like the rest of
// TraceService. Each shape below is marked "source-only" (read from Forward source) or "confirmed live" (seen by forward-skills on a primary-pin org,
// Forward 26.11.0-03, snapshot of a two-device NX-OS lab, read-only probes; the lab had no IPv6, no internet node and no chain hop in the probes);
// anything not marked confirmed was not run.

// Transit types of a chain hop (TransitType; lower case on the wire). Source-only: no chain hop was in the live probes.
const (
	TraceTransitThrough = "through"
	TraceTransitIngress = "ingress"
	TraceTransitEgress  = "egress"
)

// TraceModePermitAll computes flows as if every ACL-drop rule permitted the traffic (PathQuery mode PERMIT_ALL). Source-only: the org
// property ACL_LESS_ANALYSIS (default on, configurable on-prem) enables the computation. A request with it was accepted live (200, no
// results in the lab); what a build answers with the property off was not seen.
const TraceModePermitAll = "PERMIT_ALL"

// TraceLocation is one location filter. Build it with the TraceDevice, TraceInterface, TraceSubnet and sibling constructors; the zero value
// is invalid.
type TraceLocation struct {
	kind   string
	values []string
	value  string
	device string
	vrf    string
	clause *TraceLocation
}

const (
	traceKindHost      = "HostFilter"
	traceKindDevice    = "DeviceFilter"
	traceKindInterface = "InterfaceFilter"
	traceKindVRF       = "VrfFilter"
	traceKindDeviceVRF = "DeviceVrfFilter"
	traceKindZone      = "SecurityZoneFilter"
	traceKindSubnet    = "SubnetLocationFilter"
	traceKindNot       = "NotFilter"
)

// TraceHost matches host names (HostFilter). Source-only. Not allowed in a chain hop.
func TraceHost(names ...string) TraceLocation {
	return TraceLocation{kind: traceKindHost, values: names}
}

// TraceDevice matches device names (DeviceFilter). Allowed everywhere.
func TraceDevice(names ...string) TraceLocation {
	return TraceLocation{kind: traceKindDevice, values: names}
}

// TraceInterface matches one interface of a device (InterfaceFilter). The value is "<device> <interface>" joined by a SPACE: the comma
// form of Forward's port names ("n1,eth1/1") was refused live with 400 "Could not generate traffic for ... are you sure it exists, is up,
// and its members exist?", and the space form was accepted (confirmed live). As "to" it produced paths live; as "from" it was accepted and
// the probe returned no results, which is not conclusive. Also allowed as a chain hop.
func TraceInterface(device, iface string) TraceLocation {
	device, iface = strings.TrimSpace(device), strings.TrimSpace(iface)
	if device == "" || iface == "" {
		// A blank part would build " eth1/1": leave a blank value, which validation refuses.
		return TraceLocation{kind: traceKindInterface, values: []string{""}}
	}
	return TraceLocation{kind: traceKindInterface, values: []string{device + " " + iface}}
}

// TraceInterfaceValues matches interfaces by Forward's own value strings, for a caller that already has them.
func TraceInterfaceValues(values ...string) TraceLocation {
	return TraceLocation{kind: traceKindInterface, values: values}
}

// TraceVRF matches VRF names on any device (VrfFilter). Source-only; allowed as a hop and as a from/to location.
func TraceVRF(names ...string) TraceLocation { return TraceLocation{kind: traceKindVRF, values: names} }

// TraceDeviceVRF matches one VRF of one device (DeviceVrfFilter). Source-only.
func TraceDeviceVRF(device, vrf string) TraceLocation {
	return TraceLocation{kind: traceKindDeviceVRF, device: device, vrf: vrf}
}

// TraceSecurityZone matches a security zone of one device (SecurityZoneFilter, device and zone both required). Source-only.
func TraceSecurityZone(device, zone string) TraceLocation {
	return TraceLocation{kind: traceKindZone, device: device, value: zone}
}

// TraceSubnet matches an address or prefix (SubnetLocationFilter). In "from" it locates the source. Placed in "to" it returned NO results
// live, even for an address on a device in the network (confirmed live); an interface in "to" did return paths (confirmed live). The source
// suggests "to" is where traffic LEAVES the network (not confirmed), so give an in-network destination as a header filter (ipv4_dst) in
// from.Headers or as an interface in "to". Loopback prefixes are refused by Forward (400, source-only). Not allowed in a chain hop.
func TraceSubnet(cidr string) TraceLocation { return TraceLocation{kind: traceKindSubnet, value: cidr} }

// TraceSubnetAtDevice is TraceSubnet restricted to one device (SubnetLocationFilter with a device). Source-only.
func TraceSubnetAtDevice(cidr, device string) TraceLocation {
	return TraceLocation{kind: traceKindSubnet, value: cidr, device: device}
}

// TraceNot negates a location (NotFilter). Source-only.
func TraceNot(l TraceLocation) TraceLocation { return TraceLocation{kind: traceKindNot, clause: &l} }

// MarshalJSON renders the filter object Forward reads.
func (l TraceLocation) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": l.kind}
	switch l.kind {
	case traceKindHost, traceKindDevice, traceKindInterface, traceKindVRF:
		m["values"] = l.values
	case traceKindDeviceVRF:
		m["device"], m["vrf"] = l.device, l.vrf
	case traceKindZone:
		m["device"], m["value"] = l.device, l.value
	case traceKindSubnet:
		m["value"] = l.value
		if l.device != "" {
			m["device"] = l.device
		}
	case traceKindNot:
		if l.clause == nil {
			return nil, errors.New("forward: a negated location needs a clause")
		}
		m["clause"] = l.clause
	default:
		return nil, errors.New("forward: an empty trace location")
	}
	return json.Marshal(m)
}

// validate refuses what Forward refuses: blank names, and in a chain hop a host or subnet location.
func (l TraceLocation) validate(hop bool) error {
	switch l.kind {
	case traceKindHost, traceKindDevice, traceKindInterface, traceKindVRF:
		if len(l.values) == 0 {
			return fmt.Errorf("forward: a %s needs at least one value", l.kind)
		}
		for _, v := range l.values {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("forward: a %s cannot contain a blank value", l.kind)
			}
		}
	case traceKindDeviceVRF:
		if strings.TrimSpace(l.device) == "" || strings.TrimSpace(l.vrf) == "" {
			return errors.New("forward: a device VRF location needs a device and a VRF")
		}
	case traceKindZone:
		if strings.TrimSpace(l.device) == "" || strings.TrimSpace(l.value) == "" {
			return errors.New("forward: a security zone location needs a device and a zone")
		}
	case traceKindSubnet:
		if strings.TrimSpace(l.value) == "" {
			return errors.New("forward: a subnet location needs an address or prefix")
		}
	case traceKindNot:
		if l.clause == nil {
			return errors.New("forward: a negated location needs a clause")
		}
		if l.clause.kind == traceKindNot {
			return errors.New("forward: a negated location cannot contain another negation")
		}
		return l.clause.validate(hop)
	default:
		return errors.New("forward: an empty trace location")
	}
	if hop && (l.kind == traceKindHost || l.kind == traceKindSubnet) {
		return fmt.Errorf("forward: a %s cannot be a hop in a chain", l.kind)
	}
	return nil
}

// TraceHeaders is a packet header filter (PacketFilter): header field to the values it may take, for example
// {"ipv4_dst": {"10.0.3.0/24"}, "ip_proto": {"6"}, "tp_dst": {"443"}}. Field names are Forward's lower case names; ipv4_dst and ip_proto are
// confirmed live and ipv6_dst was accepted live (no results to read in the lab). Values may be numbers, ranges, prefixes and wildcards.
// Source-only: Forward allows app_id, user_id, user_group_id and every layer-7 field (url, domain, dns_*, http_method, content_server_*,
// rpc_number, ms_rpc_uuid) in From only and answers 400 elsewhere. The SDK refuses only the first three names itself; it leaves the layer-7
// names to Forward's 400. An empty filter is refused by the SDK (Forward accepts it).
type TraceHeaders map[string][]string

func (h TraceHeaders) validate(fromOnlyOK bool) error {
	if len(h) == 0 {
		return errors.New("forward: a header filter needs at least one field")
	}
	for field, values := range h {
		if strings.TrimSpace(field) == "" || len(values) == 0 {
			return errors.New("forward: a header filter field needs a name and at least one value")
		}
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("forward: header %s has a blank value", field)
			}
		}
		if !fromOnlyOK && (field == "app_id" || field == "user_id" || field == "user_group_id") {
			return fmt.Errorf("forward: header %s is permitted only in From", field)
		}
	}
	return nil
}

func (h TraceHeaders) wire() map[string]any {
	return map[string]any{"type": "PacketFilter", "values": map[string][]string(h)}
}

// TraceEndpoint is a From or To clause: a location with optional header filters.
type TraceEndpoint struct {
	Location       TraceLocation
	Headers        []TraceHeaders
	LogicalNetwork string
}

// TraceChainHop is one hop the traffic must pass (an item of the chain): THROUGH (anywhere along the path), INGRESS or EGRESS at the
// location.
type TraceChainHop struct {
	Transit  string
	Location TraceLocation
	Headers  []TraceHeaders
}

// TracePathQuery is the GUI Search page's query. From is where traffic enters the network, Chain the hops it must pass, and To where it
// LEAVES the network (see TraceSubnet, source-only reading); header filters ride on each. Mode is "" or TraceModePermitAll. The SDK
// refuses a query with no From, To or Chain (Forward itself does not enforce that; a mode alone is not a query).
type TracePathQuery struct {
	From  *TraceEndpoint
	Chain []TraceChainHop
	To    *TraceEndpoint
	Mode  string
}

func (e *TraceEndpoint) wire(name string, fromOnlyOK bool) (map[string]any, error) {
	if err := e.Location.validate(false); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	out := map[string]any{"location": e.Location}
	if e.LogicalNetwork != "" {
		out["logicalNetwork"] = e.LogicalNetwork
	}
	if len(e.Headers) > 0 {
		headers := make([]map[string]any, 0, len(e.Headers))
		for _, h := range e.Headers {
			if err := h.validate(fromOnlyOK); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			headers = append(headers, h.wire())
		}
		out["headers"] = headers
	}
	return out, nil
}

// filters renders the query as the PathQuery object the request body carries as "filters".
func (q TracePathQuery) filters() (map[string]any, error) {
	if q.Mode != "" && q.Mode != TraceModePermitAll {
		return nil, fmt.Errorf("forward: unknown trace mode %q (only %s)", q.Mode, TraceModePermitAll)
	}
	if q.From == nil && q.To == nil && len(q.Chain) == 0 {
		return nil, errors.New("forward: a path query needs a from, a to or a chain")
	}
	out := map[string]any{}
	if q.From != nil {
		from, err := q.From.wire("from", true)
		if err != nil {
			return nil, err
		}
		out["from"] = from
	}
	if q.To != nil {
		to, err := q.To.wire("to", false)
		if err != nil {
			return nil, err
		}
		out["to"] = to
	}
	if len(q.Chain) > 0 {
		chain := make([]map[string]any, 0, len(q.Chain))
		for i, hop := range q.Chain {
			if hop.Transit != TraceTransitThrough && hop.Transit != TraceTransitIngress && hop.Transit != TraceTransitEgress {
				return nil, fmt.Errorf("forward: chain hop %d needs a transit of through, ingress or egress", i)
			}
			if err := hop.Location.validate(true); err != nil {
				return nil, fmt.Errorf("chain hop %d: %w", i, err)
			}
			item := map[string]any{"transitType": hop.Transit, "location": hop.Location}
			if len(hop.Headers) > 0 {
				headers := make([]map[string]any, 0, len(hop.Headers))
				for _, h := range hop.Headers {
					if err := h.validate(false); err != nil {
						return nil, fmt.Errorf("chain hop %d: %w", i, err)
					}
					headers = append(headers, h.wire())
				}
				item["headers"] = headers
			}
			chain = append(chain, item)
		}
		out["chain"] = chain
	}
	if q.Mode != "" {
		out["mode"] = q.Mode
	}
	return out, nil
}

// FacetsQuery is Facets for a TracePathQuery: the distinct paths the GUI Search page's query takes (POST /api/networks/{id}/trace-facets).
// An empty snapshotID uses the network's latest. See TraceFacets for the response, and Total and PossibleLocations for the typed readings
// of totalResults and possibleIpLocations. Read-only, sent as a POST, so a retry policy resends it only after a 429 or 503 or a failure to
// connect. A TraceQuery-based Facets is unchanged.
func (s *TraceService) FacetsQuery(ctx context.Context, networkID, snapshotID string, q TracePathQuery, opts TraceFacetsOptions) (*TraceFacets, *Response, error) {
	filters, err := q.filters()
	if err != nil {
		return nil, nil, err
	}
	return s.facets(ctx, networkID, snapshotID, filters, opts, "Trace.FacetsQuery")
}

// TraceTotal is the totalResults of a facets response: Value flows, and Type "EXACT" or "LOWER_BOUND" (Forward capped the flows it examined).
// Both fields were seen live, always with Type "EXACT"; LOWER_BOUND is from source. Forward leaves totalResults out (null) when the request
// timed out and when it answered with possibleIpLocations instead of paths.
type TraceTotal struct {
	Value int64  `json:"value"`
	Type  string `json:"type"`
}

// Total decodes TotalResults. It reports false when the response carried none (a timed out request, or an ambiguous-location answer).
func (f *TraceFacets) Total() (TraceTotal, bool, error) {
	if f == nil || len(f.TotalResults) == 0 || string(f.TotalResults) == "null" {
		return TraceTotal{}, false, nil
	}
	var total TraceTotal
	if err := json.Unmarshal(f.TotalResults, &total); err != nil {
		return TraceTotal{}, false, fmt.Errorf("forward: decode trace totalResults: %w", err)
	}
	return total, true, nil
}

// TraceLocationCandidate is one place an ambiguous address or prefix could be: a device and the subnet it holds.
type TraceLocationCandidate struct {
	Device string `json:"device"`
	Subnet string `json:"subnet"`
}

// TraceLocationGroup is the candidates for one side of the query. Type is Forward's IpLocationOrigin; Locations is sorted smallest subnet
// first.
type TraceLocationGroup struct {
	Type      string                   `json:"type"`
	Locations []TraceLocationCandidate `json:"locations"`
}

// TracePossibleLocations is possibleIpLocations: for an address or prefix in From or To that resolves to more than one place, the places.
// A side is nil when it was not ambiguous. Source-only: absent in every live response so far.
type TracePossibleLocations struct {
	From *TraceLocationGroup `json:"from,omitempty"`
	To   *TraceLocationGroup `json:"to,omitempty"`
}

// PossibleLocations decodes PossibleIPLocations. It reports false when the response carried none.
func (f *TraceFacets) PossibleLocations() (TracePossibleLocations, bool, error) {
	if f == nil || len(f.PossibleIPLocations) == 0 || string(f.PossibleIPLocations) == "null" {
		return TracePossibleLocations{}, false, nil
	}
	var out TracePossibleLocations
	if err := json.Unmarshal(f.PossibleIPLocations, &out); err != nil {
		return TracePossibleLocations{}, false, fmt.Errorf("forward: decode trace possibleIpLocations: %w", err)
	}
	return out, true, nil
}
