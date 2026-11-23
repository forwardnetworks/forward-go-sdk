package domain

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Alias is one alias active at a snapshot (the published Alias schema). Type
// is HOSTS, DEVICES, INTERFACES, HEADERS or LOGICAL_NETWORK. The members
// depend on the type (values, locations, vlanIds, edgeNodes, ...), so
// Definition keeps the whole object as Forward sent it.
type Alias struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	CreatorID  Identifier      `json:"creatorId,omitempty"`
	Definition json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the common fields and keeps the full object.
func (a *Alias) UnmarshalJSON(data []byte) error {
	type plain Alias
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*a = Alias(out)
	a.Definition = append(json.RawMessage(nil), data...)
	return nil
}

// Alias types (AliasType).
const (
	AliasTypeHosts          = "HOSTS"
	AliasTypeDevices        = "DEVICES"
	AliasTypeInterfaces     = "INTERFACES"
	AliasTypeHeaders        = "HEADERS"
	AliasTypeLogicalNetwork = "LOGICAL_NETWORK"
)

// aliasHeaderTypes are the HEADERS alias keys (TrafficAliasType, lower-cased
// on the wire): categories of header fields, not header field names.
var aliasHeaderTypes = map[string]bool{
	"mac_addr": true, "eth_type": true, "vlan_vid": true, "ip_addr": true, "ip_proto": true, "tp_port": true,
}

// AliasBuilder is an alias to create or replace. Set Type and only the fields
// it uses; a field another type owns is refused, since Forward would ignore
// it silently:
//
//   - HOSTS: Values (host names, IP addresses or subnets, MAC addresses; no
//     globs) and/or Locations (device or interface names, or aliases, the
//     hosts attach to). At least one of the two.
//   - DEVICES: Values, device names and globs ("bbr?_rtr").
//   - INTERFACES: Values ("device port", globs allowed) and/or VLANIDs (ranges
//     such as "20-29") with optional VLANIntfTypes (ACCESS, TRUNK; omitted
//     means both); with both, VLANs narrow Values. IsExposurePoint marks it
//     usable for host exposure analysis.
//   - HEADERS: HeaderValues, keyed by category -- mac_addr, eth_type, vlan_vid,
//     ip_addr, ip_proto or tp_port -- to the allowed values ("UDP", "0x800",
//     "10000", "10.0.0.0/8"). At least one category. Sent as "values".
//   - LOGICAL_NETWORK: Devices and/or EdgeNodes, names and globs.
//
// Forward validates the values themselves (a 400 for one it cannot parse).
type AliasBuilder struct {
	Name            string
	Type            string
	Values          []string
	Locations       []string
	VLANIDs         []string
	VLANIntfTypes   []string
	IsExposurePoint *bool
	HeaderValues    map[string][]string
	Devices         []string
	EdgeNodes       []string
}

func (b AliasBuilder) Check() error {
	if b.Name == "" {
		return errors.New("forward: alias name is required")
	}
	type field struct {
		name string
		set  bool
	}
	owned := map[string][]field{
		AliasTypeHosts:          {{"Values", len(b.Values) != 0}, {"Locations", len(b.Locations) != 0}},
		AliasTypeDevices:        {{"Values", len(b.Values) != 0}},
		AliasTypeInterfaces:     {{"Values", len(b.Values) != 0}, {"VLANIDs", len(b.VLANIDs) != 0}, {"VLANIntfTypes", len(b.VLANIntfTypes) != 0}, {"IsExposurePoint", b.IsExposurePoint != nil}},
		AliasTypeHeaders:        {{"HeaderValues", len(b.HeaderValues) != 0}},
		AliasTypeLogicalNetwork: {{"Devices", len(b.Devices) != 0}, {"EdgeNodes", len(b.EdgeNodes) != 0}},
	}
	mine, ok := owned[b.Type]
	if !ok {
		return fmt.Errorf("forward: unknown alias type %q", b.Type)
	}
	allowed := map[string]bool{}
	for _, f := range mine {
		allowed[f.name] = true
	}
	for _, other := range owned {
		for _, f := range other {
			if f.set && !allowed[f.name] {
				return fmt.Errorf("forward: alias field %s does not apply to a %s alias", f.name, b.Type)
			}
		}
	}
	switch b.Type {
	case AliasTypeHosts:
		if len(b.Values) == 0 && len(b.Locations) == 0 {
			return errors.New("forward: a HOSTS alias needs Values or Locations")
		}
	case AliasTypeHeaders:
		if len(b.HeaderValues) == 0 {
			return errors.New("forward: a HEADERS alias needs at least one header category")
		}
		for key := range b.HeaderValues {
			if !aliasHeaderTypes[key] {
				return fmt.Errorf("forward: %q is not a HEADERS alias category (mac_addr, eth_type, vlan_vid, ip_addr, ip_proto, tp_port)", key)
			}
		}
	case AliasTypeInterfaces:
		for _, t := range b.VLANIntfTypes {
			if t != "ACCESS" && t != "TRUNK" {
				return fmt.Errorf("forward: VLAN interface type %q must be ACCESS or TRUNK", t)
			}
		}
	}
	return nil
}

// MarshalJSON writes the body Forward's AliasBuilder expects for the type.
func (b AliasBuilder) MarshalJSON() ([]byte, error) {
	if err := b.Check(); err != nil {
		return nil, err
	}
	body := map[string]any{"name": b.Name, "type": b.Type}
	set := func(key string, values []string) {
		if len(values) != 0 {
			body[key] = values
		}
	}
	switch b.Type {
	case AliasTypeHosts:
		set("values", b.Values)
		set("locations", b.Locations)
	case AliasTypeDevices:
		set("values", b.Values)
	case AliasTypeInterfaces:
		set("values", b.Values)
		set("vlanIds", b.VLANIDs)
		set("vlanIntfTypes", b.VLANIntfTypes)
		if b.IsExposurePoint != nil {
			body["isExposurePoint"] = *b.IsExposurePoint
		}
	case AliasTypeHeaders:
		body["values"] = b.HeaderValues
	case AliasTypeLogicalNetwork:
		set("devices", b.Devices)
		set("edgeNodes", b.EdgeNodes)
	}
	return json.Marshal(body)
}
