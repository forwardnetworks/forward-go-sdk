package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// RPBGPAttributes is RPPrefixDetails.Attributes for a BGP node, as Forward's RpRouteAttributes (BgpRouteAttributes) serializes it. Preview:
// read from Forward's source (core/.../predict/unified/BgpRouteAttributes.java), not yet decoded from a live response, so a field Forward
// names differently stays at its zero value; Raw keeps the whole object.
//
// The PREDICTED side differs from the collected side (Devices.BGPAdvertisements, BGPAdvertisement) in ways that matter when comparing:
//   - MED is a plain number. Forward models "MED not set" internally as -1 but serializes it as 0 (getJsonMed), so a predicted MED of
//     0 means unset OR an explicit 0; a collected advertisement with no MED omits the field. Compare an absent collected MED with 0.
//   - LocalPref is always present (Forward's default is 100 when nothing set it), where a collected advertisement received over eBGP
//     with no local-preference policy omits it.
//   - ASPath is a list of SETS of AS numbers (an AS_SEQUENCE step is a one-element set), not a flat list.
type RPBGPAttributes struct {
	ASPath    [][]int64 `json:"asPath"`
	LocalPref int64     `json:"localPref"`
	MED       int64     `json:"med"`
	AIGP      int64     `json:"aigpMetric"`
	NextHop   string    `json:"bgpNextHopAddr"`
	Origin    string    `json:"origin"`
	// Communities holds the standard, extended and large communities as Forward sent them (their text form is not pinned down here).
	StandardCommunities []json.RawMessage `json:"standardCommunities,omitempty"`
	ExtendedCommunities []json.RawMessage `json:"extendedCommunities,omitempty"`
	LargeCommunities    []json.RawMessage `json:"largeCommunities,omitempty"`
	NextHopDiscard      bool              `json:"nextHopDiscard,omitempty"`
	// Raw is the whole attributes object as Forward sent it.
	Raw json.RawMessage `json:"-"`
}

// FlatASPath is ASPath with every set flattened in order, the shape of a collected BGPAdvertisement.ASPath. A multi-member set (an
// AS_SET) loses its set-ness here, in the numeric order Forward sent.
func (a RPBGPAttributes) FlatASPath() []int64 {
	var out []int64
	for _, set := range a.ASPath {
		out = append(out, set...)
	}
	return out
}

// BGPAttributes decodes Attributes when this node is a BGP node. It returns an error naming the protocol for any other node (OSPF, IS-IS,
// config, OMP), or when the answer carries no attributes.
func (d *RPPrefixDetails) BGPAttributes() (*RPBGPAttributes, error) {
	if d == nil || len(d.Attributes) == 0 || string(d.Attributes) == "null" {
		return nil, errors.New("forward: the prefix details carry no attributes")
	}
	var probe struct {
		Protocol string `json:"protocol"`
	}
	if err := json.Unmarshal(d.Attributes, &probe); err != nil {
		return nil, fmt.Errorf("forward: decode prefix attributes: %w", err)
	}
	if probe.Protocol != "" && !strings.EqualFold(probe.Protocol, "BGP") {
		return nil, fmt.Errorf("forward: the node's attributes are %s, not BGP", probe.Protocol)
	}
	attrs := new(RPBGPAttributes)
	if err := json.Unmarshal(d.Attributes, attrs); err != nil {
		return nil, fmt.Errorf("forward: decode BGP attributes: %w", err)
	}
	attrs.Raw = append(json.RawMessage(nil), d.Attributes...)
	return attrs, nil
}
