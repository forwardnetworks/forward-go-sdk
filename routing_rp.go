package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The rp-prefix-* routes (PredictController), in the request shapes of Forward master (fe0daf63c899). They cover every protocol Predict
// models, BGP, OSPF, IS_IS, OMP and CONFIG: master's node keys name all five (there is no OMP or IS-IS node read elsewhere, so this is how
// their routes are traced), and speak the new bodies: a router and origin are a DeviceVRFProtocol (the protocol is required) and details
// takes a node as its key plus route set id.
//
// WHICH BUILDS. Stable b1feb6fe1c34 does not have the routes at all (404 "No endpoint", errors.Is(err, ErrEndpointNotServed); a build
// without the view=rp-prefix-info mapping answers the plain device instead, which RPDevicePrefixInfo returns as ErrEndpointNotServed too). Primary
// 5f7e4ec1841 has the routes but the older bodies (routerKey and a full node, like bgp-prefix-*), which these methods do not send; on a
// build that old use PrefixSearch, PrefixTrace and PrefixDetails. Preview: not in the published spec. Same permission and gates as the
// bgp-* routes (RP_PREFIX_SEARCH, which only the network role ADMIN holds, PREDICT_MODELING, a snapshot past creation).

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

// RPPrefixSearch answers "where does this prefix come from and who has it?" for any modelled protocol (POST .../rp-prefix-search). The
// answer has the shape of PrefixSearch's.
func (s *RoutingService) RPPrefixSearch(ctx context.Context, networkID, snapshotID string, query RPPrefixSearch) (*RoutingPrefixSearchResult, *Response, error) {
	if strings.TrimSpace(query.Prefix) == "" {
		return nil, nil, errors.New("forward: prefix is required")
	}
	if !validRouteOutcome(query.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", query.Outcome)
	}
	for _, node := range []*DeviceVRFProtocol{query.Router, query.OriginatedFrom} {
		if node != nil && (strings.TrimSpace(node.Device) == "" || strings.TrimSpace(node.Protocol) == "") {
			return nil, nil, errors.New("forward: a router or origin needs a device and a protocol")
		}
	}
	out := new(RoutingPrefixSearchResult)
	resp, err := s.post(ctx, networkID, "rp-prefix-search", snapshotID, "Routing.RPPrefixSearch", query, out)
	return out, resp, err
}

// RPPrefixTrace returns the propagation of a prefix from its origin to a router (POST .../rp-prefix-trace): a list of paths, each a list
// of hops. The hops carry the node key RoutingNode.Ref needs.
func (s *RoutingService) RPPrefixTrace(ctx context.Context, networkID, snapshotID string, request RPPrefixTrace) ([][]RoutingNode, *Response, error) {
	request.Router.Device = strings.TrimSpace(request.Router.Device)
	request.OriginatedFrom.Device = strings.TrimSpace(request.OriginatedFrom.Device)
	request.Router.Protocol = strings.TrimSpace(request.Router.Protocol)
	request.OriginatedFrom.Protocol = strings.TrimSpace(request.OriginatedFrom.Protocol)
	if strings.TrimSpace(request.Prefix) == "" || request.Router.Device == "" || request.OriginatedFrom.Device == "" ||
		request.Router.Protocol == "" || request.OriginatedFrom.Protocol == "" || request.Outcome == "" {
		return nil, nil, errors.New("forward: prefix, router, originatedFrom (each with a protocol) and outcome are required")
	}
	if !validRouteOutcome(request.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", request.Outcome)
	}
	var out [][]RoutingNode
	resp, err := s.post(ctx, networkID, "rp-prefix-trace", snapshotID, "Routing.RPPrefixTrace", request, &out)
	return out, resp, err
}

// RPPrefixDetails returns the route attributes and policies at one hop of a trace (POST .../rp-prefix-details). A node that does not
// resolve is a RoutingLookupError.
func (s *RoutingService) RPPrefixDetails(ctx context.Context, networkID, snapshotID string, request RPPrefixDetailsRequest) (*RPPrefixDetails, *Response, error) {
	if strings.TrimSpace(request.Prefix) == "" || len(request.Node.NodeKey) == 0 {
		return nil, nil, errors.New("forward: prefix and node (with its key) are required")
	}
	var raw json.RawMessage
	resp, err := s.post(ctx, networkID, "rp-prefix-details", snapshotID, "Routing.RPPrefixDetails", request, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(RPPrefixDetails)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// RPDevicePrefixInfo returns what one device has for a prefix, per VRF and protocol (GET .../devices/{device}?view=rp-prefix-info&prefix=).
// A device with no model for it answers a RoutingLookupError.
func (s *RoutingService) RPDevicePrefixInfo(ctx context.Context, networkID, snapshotID, device, prefix string) (*RPDevicePrefixInfo, *Response, error) {
	query, err := routingSnapshot(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(device) == "" || strings.TrimSpace(prefix) == "" {
		return nil, nil, errors.New("forward: device and prefix are required")
	}
	path, err := s.path(networkID, "devices", url.PathEscape(strings.TrimSpace(device)))
	if err != nil {
		return nil, nil, err
	}
	query.Set("view", "rp-prefix-info")
	query.Set("prefix", strings.TrimSpace(prefix))
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.doRequired(markOperation(req, "Routing.RPDevicePrefixInfo"), &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(RPDevicePrefixInfo)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	// A build without the view=rp-prefix-info mapping answers this URL with the plain device (200), which would read as "no routes".
	var probe struct {
		VRFs json.RawMessage `json:"vrfs"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.VRFs == nil {
		return nil, resp, fmt.Errorf("%w: this build answered the device, not a prefix view (use DevicePrefixInfo)", ErrEndpointNotServed)
	}
	return out, resp, nil
}
