package domain

import (
	"encoding/json"
	"errors"
)

// RPPrefixSearch is the question of RPPrefixSearch. Prefix is required; Router and OriginatedFrom each need a protocol when set. Outcome
// is INSTALLED, FILTERED_OUT or NOT_PREFERRED to look only at routers with that outcome.
type RPPrefixSearch struct {
	Prefix         string             `json:"prefix"`
	Router         *DeviceVRFProtocol `json:"router,omitempty"`
	OriginatedFrom *DeviceVRFProtocol `json:"originatedFrom,omitempty"`
	Outcome        string             `json:"outcome,omitempty"`
}

// RPPrefixTrace names the prefix, the router to look at, the origin it was learned from and the outcome: all four are required, and the
// router and origin carry their protocol. Take them from a RPPrefixSearch result.
type RPPrefixTrace struct {
	Prefix         string            `json:"prefix"`
	Router         DeviceVRFProtocol `json:"router"`
	OriginatedFrom DeviceVRFProtocol `json:"originatedFrom"`
	Outcome        string            `json:"outcome"`
}

// RPNodeRef is a node as RPPrefixDetails takes it: the node's key (opaque; its protocol member says which protocol's key it is) and the
// route set id. Make one from a trace hop with RoutingNode.Ref.
type RPNodeRef struct {
	NodeKey    json.RawMessage `json:"nodeKey"`
	RouteSetID int             `json:"routeSetId"`
}

// Ref returns the node as RPPrefixDetails takes it. The node must have been read from a trace on a build that sends a nodeKey
// (master's rp-prefix-trace does); a hand-built node, or one from a bgp-prefix-trace on a pinned build, has none.
func (n RoutingNode) Ref() (RPNodeRef, error) {
	var probe struct {
		NodeKey json.RawMessage `json:"nodeKey"`
	}
	if len(n.Raw) == 0 || json.Unmarshal(n.Raw, &probe) != nil || len(probe.NodeKey) == 0 || string(probe.NodeKey) == "null" {
		return RPNodeRef{}, errors.New("forward: the node carries no nodeKey (use a hop from RPPrefixTrace)")
	}
	return RPNodeRef{NodeKey: probe.NodeKey, RouteSetID: n.RouteSetID}, nil
}

// RPPrefixDetailsRequest asks for the route attributes at Node and the policies applied there. PreviousNode and PreviousPreviousNode give
// the import policies, NextNode the export policies; take them all from one trace path.
type RPPrefixDetailsRequest struct {
	Prefix               string     `json:"prefix"`
	Node                 RPNodeRef  `json:"node"`
	PreviousNode         *RPNodeRef `json:"previousNode,omitempty"`
	NextNode             *RPNodeRef `json:"nextNode,omitempty"`
	PreviousPreviousNode *RPNodeRef `json:"previousPreviousNode,omitempty"`
}

// RPPrefixDetails is the route a node holds for the prefix. Attributes and NodeAttributes (BGP and OSPF nodes only) carry a "protocol"
// member and differ by protocol; Policies maps a rule type to the matched policy rules. All are left raw. The node's device and VRF are
// in NodeKey.
type RPPrefixDetails struct {
	Prefix         string                     `json:"prefix"`
	NodeKey        json.RawMessage            `json:"nodeKey,omitempty"`
	Attributes     json.RawMessage            `json:"attributes,omitempty"`
	NodeAttributes json.RawMessage            `json:"nodeAttributes,omitempty"`
	Policies       map[string]json.RawMessage `json:"policies,omitempty"`
}

// RPReceivedPrefix is one received advertisement of the prefix on a device VRF, with the protocol it arrived by and the node it was
// originated from.
type RPReceivedPrefix struct {
	Nexthop        string            `json:"nexthop"`
	Protocol       string            `json:"protocol"`
	Outcome        string            `json:"outcome"`
	OriginatedFrom DeviceVRFProtocol `json:"originatedFrom"`
}

// RPDeviceVRFPrefixInfo is what one VRF of a device has for the prefix.
type RPDeviceVRFPrefixInfo struct {
	VRF                         string             `json:"vrf"`
	RecvAdvertisements          []RPReceivedPrefix `json:"recvAdvertisements"`
	MoreSpecificInstalledRoutes []string           `json:"moreSpecificInstalledRoutes"`
}

// RPDevicePrefixInfo is what a device has for a prefix, per VRF.
type RPDevicePrefixInfo struct {
	Device string                  `json:"device"`
	Vendor string                  `json:"vendor"`
	Model  string                  `json:"model"`
	VRFs   []RPDeviceVRFPrefixInfo `json:"vrfs"`
}
