package forward

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// A typed intent query (Forward's PathQuery): the "filters" of an Isolation, Reachability or Existential check, and the question of a path
// search. Forward's definition is read back from Postgres jsonb, which reorders object keys, so two equal queries can differ byte for byte.
// Build the filters here and send CanonicalJSON, or run a stored definition through CanonicalIntentQuery before comparing: both give one fixed
// key order, the spelling Forward itself writes ("value" for one item, "values" for several, a bare address for a /32), and sorted,
// de-duplicated value sets. A key this SDK does not model is an error, never silently dropped, so two different queries never read equal.
// Preview: ~/src/fwd core/.../query/json/PathQuery.java and its filter classes (master fe0daf63c899).

// Location filter types (Forward's ApiFilterType names, as sent in "type").
const (
	LocationHost            = "HostFilter"
	LocationDevice          = "DeviceFilter"
	LocationInterface       = "InterfaceFilter"
	LocationTunnelInterface = "TunnelInterfaceFilter"
	LocationSecurityZone    = "SecurityZoneFilter"
	LocationDeviceVRF       = "DeviceVrfFilter"
	LocationVRF             = "VrfFilter"
	LocationHostAlias       = "HostAliasFilter"
	LocationDeviceAlias     = "DeviceAliasFilter"
	LocationInterfaceAlias  = "InterfaceAliasFilter"
	LocationSubnet          = "SubnetLocationFilter"
	FilterNot               = "NotFilter"
	HeaderPacket            = "PacketFilter"
	HeaderPacketAlias       = "PacketAliasFilter"
)

// Packet header field names used in a packet filter (Forward's HeaderField names, case-insensitive on the way in; sent lower case).
const (
	HeaderIPv4Src = "ipv4_src"
	HeaderIPv4Dst = "ipv4_dst"
	HeaderIPv6Src = "ipv6_src"
	HeaderIPv6Dst = "ipv6_dst"
	HeaderIPProto = "ip_proto"
	HeaderTPSrc   = "tp_src"
	HeaderTPDst   = "tp_dst"
)

// PathFilter is one location or packet-header filter. Build one with the constructors (HostLocation, DeviceLocation, SubnetLocation,
// PacketHeaders, Not, ...); the zero value is invalid.
type PathFilter struct {
	Type string
	// Values is the value set of Host, Device, Interface and VRF filters.
	Values []string
	// Value is the single value of a tunnel interface, alias, subnet or packet alias filter, or the zone of a SecurityZone filter.
	Value string
	// Device names the device of a SecurityZone or DeviceVrf filter, and optionally of a Subnet filter (then Host may be set too).
	Device string
	VRF    string
	Host   string
	// Fields maps header field name to its values (packet filter); Direction is "SRC" or "DST" for a packet alias.
	Fields    map[string][]string
	Direction string
	// Clause is what a Not filter negates.
	Clause *PathFilter
}

// HostLocation matches DISCOVERED hosts by IP or name ("10.0.0.5", "web-1"): Forward fails a query whose host it has not discovered ("No
// hosts matching the alias"). For an address or prefix that may be undiscovered, such as a static route's destination, use SubnetLocation.
func HostLocation(values ...string) PathFilter { return PathFilter{Type: LocationHost, Values: values} }

// DeviceLocation matches devices by name ("*" for any).
func DeviceLocation(values ...string) PathFilter {
	return PathFilter{Type: LocationDevice, Values: values}
}

// InterfaceLocation matches interfaces, "device interface" as Forward names them.
func InterfaceLocation(values ...string) PathFilter {
	return PathFilter{Type: LocationInterface, Values: values}
}

// VRFLocation matches VRFs by name.
func VRFLocation(values ...string) PathFilter { return PathFilter{Type: LocationVRF, Values: values} }

// DeviceVRFLocation is one VRF of one device.
func DeviceVRFLocation(device, vrf string) PathFilter {
	return PathFilter{Type: LocationDeviceVRF, Device: device, VRF: vrf}
}

// SecurityZoneLocation is a named security zone of a device.
func SecurityZoneLocation(device, zone string) PathFilter {
	return PathFilter{Type: LocationSecurityZone, Device: device, Value: zone}
}

// SubnetLocation is an address or subnet, found whether or not a host is discovered there.
func SubnetLocation(subnet string) PathFilter { return PathFilter{Type: LocationSubnet, Value: subnet} }

// AliasLocation refers to a named alias of the given kind (LocationHostAlias, LocationDeviceAlias or LocationInterfaceAlias).
func AliasLocation(kind, name string) PathFilter { return PathFilter{Type: kind, Value: name} }

// PacketHeaders constrains packet header fields: field name to the values it may take ("ipv4_dst": {"10.0.0.0/8"}, "ip_proto": {"6"}).
func PacketHeaders(fields map[string][]string) PathFilter {
	return PathFilter{Type: HeaderPacket, Fields: fields}
}

// Not negates a location or packet filter. Forward refuses a Not of a Not.
func Not(clause PathFilter) PathFilter { return PathFilter{Type: FilterNot, Clause: &clause} }

// normalizeHostValue writes a host or subnet value the way Forward echoes it: a single address, not a /32 or /128 prefix.
func normalizeHostValue(v string) string {
	v = strings.TrimSpace(v)
	if prefix, err := netip.ParsePrefix(v); err == nil && prefix.Bits() == prefix.Addr().BitLen() {
		return prefix.Addr().String()
	}
	return v
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// strictUnmarshal refuses a key this SDK does not model: dropping one silently would let two different queries canonicalize to the same bytes.
func strictUnmarshal(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

// MarshalJSON writes the filter in its one canonical form.
func (f PathFilter) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	put := func(first bool, key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if !first {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q:%s", key, raw)
		return nil
	}
	b.WriteByte('{')
	if err := put(true, "type", f.Type); err != nil {
		return nil, err
	}
	var err error
	switch f.Type {
	case LocationHost, LocationDevice, LocationInterface, LocationVRF:
		values := sortedUnique(f.Values)
		if f.Type == LocationHost {
			for i, v := range values {
				values[i] = normalizeHostValue(v)
			}
			values = sortedUnique(values)
		}
		switch len(values) {
		case 0:
			return nil, fmt.Errorf("forward: a %s needs at least one value", f.Type)
		case 1:
			err = put(false, "value", values[0]) // the spelling Forward itself writes for one value
		default:
			err = put(false, "values", values)
		}
	case LocationTunnelInterface, LocationHostAlias, LocationDeviceAlias, LocationInterfaceAlias:
		if strings.TrimSpace(f.Value) == "" {
			return nil, fmt.Errorf("forward: a %s needs a value", f.Type)
		}
		err = put(false, "value", strings.TrimSpace(f.Value))
	case LocationSubnet:
		if strings.TrimSpace(f.Value) == "" {
			return nil, errors.New("forward: a subnet filter needs a value")
		}
		if err = put(false, "value", normalizeHostValue(f.Value)); err == nil && f.Host != "" {
			err = put(false, "host", f.Host)
		}
		if err == nil && f.Device != "" {
			err = put(false, "device", f.Device)
		}
	case LocationSecurityZone:
		if f.Device == "" || strings.TrimSpace(f.Value) == "" {
			return nil, errors.New("forward: a security zone filter needs a device and a zone")
		}
		if err = put(false, "device", f.Device); err == nil {
			err = put(false, "value", strings.TrimSpace(f.Value))
		}
	case LocationDeviceVRF:
		if f.Device == "" || strings.TrimSpace(f.VRF) == "" {
			return nil, errors.New("forward: a device VRF filter needs a device and a VRF")
		}
		if err = put(false, "device", f.Device); err == nil {
			err = put(false, "vrf", strings.TrimSpace(f.VRF))
		}
	case HeaderPacket:
		if len(f.Fields) == 0 {
			return nil, errors.New("forward: a packet filter needs at least one header field")
		}
		fields := map[string][]string{}
		for name, values := range f.Fields {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" || len(sortedUnique(values)) == 0 {
				return nil, errors.New("forward: a packet filter field needs a name and values")
			}
			if _, dup := fields[name]; dup {
				return nil, fmt.Errorf("forward: header field %q is given twice", name)
			}
			fields[name] = sortedUnique(values)
		}
		err = put(false, "values", fields) // encoding/json writes map keys in sorted order
	case HeaderPacketAlias:
		if strings.TrimSpace(f.Value) == "" {
			return nil, errors.New("forward: a packet alias needs a value")
		}
		if err = put(false, "value", strings.TrimSpace(f.Value)); err == nil && f.Direction != "" {
			err = put(false, "direction", strings.ToLower(f.Direction))
		}
	case FilterNot:
		if f.Clause == nil || f.Clause.Type == FilterNot {
			return nil, errors.New("forward: a not filter negates one filter, and not another not")
		}
		clause, cerr := f.Clause.MarshalJSON()
		if cerr != nil {
			return nil, cerr
		}
		err = put(false, "clause", json.RawMessage(clause))
	default:
		return nil, fmt.Errorf("forward: unknown path filter type %q", f.Type)
	}
	if err != nil {
		return nil, err
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON reads a filter in any spelling Forward accepts ("value" or "values", any key order) so it can be written back canonically. A
// key it does not know is an error.
func (f *PathFilter) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type      string          `json:"type"`
		Value     *string         `json:"value"`
		Values    json.RawMessage `json:"values"`
		Device    string          `json:"device"`
		VRF       string          `json:"vrf"`
		Host      string          `json:"host"`
		Direction string          `json:"direction"`
		Clause    *PathFilter     `json:"clause"`
	}
	if err := strictUnmarshal(data, &raw); err != nil {
		return err
	}
	out := PathFilter{Type: raw.Type, Device: raw.Device, VRF: raw.VRF, Host: raw.Host, Direction: raw.Direction, Clause: raw.Clause}
	if raw.Value != nil {
		out.Value = *raw.Value
	}
	if len(raw.Values) > 0 && string(raw.Values) != "null" {
		if raw.Type == HeaderPacket {
			fields := map[string][]string{}
			if err := json.Unmarshal(raw.Values, &fields); err != nil {
				return fmt.Errorf("forward: packet filter values: %w", err)
			}
			out.Fields = fields
		} else if err := json.Unmarshal(raw.Values, &out.Values); err != nil {
			return fmt.Errorf("forward: filter values: %w", err)
		}
	}
	switch raw.Type {
	case LocationHost, LocationDevice, LocationInterface, LocationVRF:
		if len(out.Values) == 0 && out.Value != "" { // the single spelling
			out.Values, out.Value = []string{out.Value}, ""
		}
	}
	*f = out
	return nil
}

// IntentEndpoint is the "from" or "to" of a query: where, and optionally the packet headers there, within an optional logical network.
type IntentEndpoint struct {
	LogicalNetwork string       `json:"logicalNetwork,omitempty"`
	Location       PathFilter   `json:"location"`
	Headers        []PathFilter `json:"headers,omitempty"`
}

// IntentHop is one stop the path must go through, enter or leave by (Transit is "through", "ingress" or "egress").
type IntentHop struct {
	Transit  string       `json:"transitType"`
	Location PathFilter   `json:"location"`
	Headers  []PathFilter `json:"headers,omitempty"`
}

// IntentQuery is a path query (Forward's PathQuery). From and To are what the check's intent compares; either may be empty (Forward allows
// it); Chain, Bypass, ForwardingTypes, FlowTypes (Forward's ForwardingTypeSpecifier and FlowTypeSpecifier names, such as L3 and VALID) and Mode
// (PERMIT_ALL) are optional. Forward's hidden deviceHops key is not modelled and is refused when read.
type IntentQuery struct {
	From            *IntentEndpoint
	Chain           []IntentHop
	To              *IntentEndpoint
	Bypass          []PathFilter
	ForwardingTypes []string
	FlowTypes       []string
	Mode            string
}

var (
	// Where each filter may go (Forward's LocationFilter, HopLocationFilter, BypassFilter and PacketHeaderFilter hierarchies).
	endpointLocationTypes = setOf(LocationHost, LocationDevice, LocationInterface, LocationTunnelInterface, LocationSecurityZone, LocationDeviceVRF,
		LocationVRF, LocationHostAlias, LocationDeviceAlias, LocationInterfaceAlias, LocationSubnet)
	hopLocationTypes = setOf(LocationDevice, LocationInterface, LocationTunnelInterface, LocationSecurityZone, LocationDeviceVRF, LocationVRF,
		LocationDeviceAlias, LocationInterfaceAlias)
	bypassTypes = setOf(LocationDevice, LocationDeviceAlias)
	headerTypes = setOf(HeaderPacket, HeaderPacketAlias)
)

func setOf(values ...string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		out[v] = true
	}
	return out
}

// place checks a filter belongs where it is put; a Not is as good as what it negates (a bypass takes no Not).
func place(f PathFilter, allowed map[string]bool, where string, allowNot bool) error {
	inner := f
	if f.Type == FilterNot && allowNot && f.Clause != nil {
		inner = *f.Clause
	}
	if !allowed[inner.Type] {
		return fmt.Errorf("forward: a %s filter cannot be used in %s", f.Type, where)
	}
	return nil
}

func validateEndpoint(e *IntentEndpoint, name string) error {
	if err := place(e.Location, endpointLocationTypes, "'"+name+"' location", true); err != nil {
		return err
	}
	for _, h := range e.Headers {
		if err := place(h, headerTypes, "'"+name+"' headers", true); err != nil {
			return err
		}
	}
	return nil
}

func normalizeTransit(t string) string { return strings.ToLower(strings.TrimSpace(t)) }

// CanonicalJSON writes the query in the one canonical form: fixed key order (from, chain, to, bypass, forwardingTypes, flowTypes, mode),
// Forward's own spelling of values, sorted and de-duplicated value sets and header fields, empty parts left out. Chain order is kept (it is a
// path; a bypass is an OR, so its order is kept too). It refuses a filter placed where Forward refuses it.
func (q IntentQuery) CanonicalJSON() (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	put := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%q:%s", key, raw)
		return nil
	}
	if q.From != nil {
		if err := validateEndpoint(q.From, "from"); err != nil {
			return nil, err
		}
		if err := put("from", q.From); err != nil {
			return nil, err
		}
	}
	if len(q.Chain) > 0 {
		hops := make([]IntentHop, len(q.Chain))
		for i, hop := range q.Chain {
			hop.Transit = normalizeTransit(hop.Transit)
			switch hop.Transit {
			case "through", "ingress", "egress":
			default:
				return nil, fmt.Errorf("forward: a chain hop's transit is through, ingress or egress, not %q", hop.Transit)
			}
			if err := place(hop.Location, hopLocationTypes, "a chain hop's location", true); err != nil {
				return nil, err
			}
			for _, h := range hop.Headers {
				if err := place(h, headerTypes, "a chain hop's headers", true); err != nil {
					return nil, err
				}
			}
			hops[i] = hop
		}
		if err := put("chain", hops); err != nil {
			return nil, err
		}
	}
	if q.To != nil {
		if err := validateEndpoint(q.To, "to"); err != nil {
			return nil, err
		}
		if err := put("to", q.To); err != nil {
			return nil, err
		}
	}
	if len(q.Bypass) > 0 {
		for _, f := range q.Bypass {
			if err := place(f, bypassTypes, "bypass", false); err != nil {
				return nil, err
			}
		}
		if err := put("bypass", q.Bypass); err != nil {
			return nil, err
		}
	}
	if values := sortedUnique(q.ForwardingTypes); len(values) > 0 {
		if err := put("forwardingTypes", values); err != nil {
			return nil, err
		}
	}
	if values := sortedUnique(q.FlowTypes); len(values) > 0 {
		if err := put("flowTypes", values); err != nil {
			return nil, err
		}
	}
	if q.Mode != "" {
		if err := put("mode", q.Mode); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ParseIntentQuery reads a stored or hand-written filters object, in any key order and spelling, into an IntentQuery. A key it does not
// model (such as the hidden deviceHops) is an error.
func ParseIntentQuery(raw json.RawMessage) (IntentQuery, error) {
	var in struct {
		From            *IntentEndpoint `json:"from"`
		Chain           []IntentHop     `json:"chain"`
		To              *IntentEndpoint `json:"to"`
		Bypass          []PathFilter    `json:"bypass"`
		ForwardingTypes []string        `json:"forwardingTypes"`
		FlowTypes       []string        `json:"flowTypes"`
		Mode            string          `json:"mode"`
	}
	if err := strictUnmarshal(raw, &in); err != nil {
		return IntentQuery{}, fmt.Errorf("forward: read intent query: %w", err)
	}
	return IntentQuery{From: in.From, Chain: in.Chain, To: in.To, Bypass: in.Bypass, ForwardingTypes: in.ForwardingTypes, FlowTypes: in.FlowTypes, Mode: in.Mode}, nil
}

// CanonicalIntentQuery rewrites a stored filters object into the canonical form, so two spellings of one query compare equal as bytes. It
// fails on a key or filter type this SDK does not know rather than guess.
func CanonicalIntentQuery(raw json.RawMessage) (json.RawMessage, error) {
	q, err := ParseIntentQuery(raw)
	if err != nil {
		return nil, err
	}
	return q.CanonicalJSON()
}
