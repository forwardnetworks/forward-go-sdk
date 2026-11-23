package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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

// RPPrefixSearch answers "where does this prefix come from and who has it?" for any modelled protocol (POST .../rp-prefix-search). The
// answer has the shape of PrefixSearch's.
func (s *Routing) RPPrefixSearch(ctx context.Context, networkID, snapshotID string, query ports.RPPrefixSearch) (*ports.RoutingPrefixSearchResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(query.Prefix) == "" {
		return nil, nil, errors.New("forward: prefix is required")
	}
	if !validRouteOutcome(query.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", query.Outcome)
	}
	for _, node := range []*ports.DeviceVRFProtocol{query.Router, query.OriginatedFrom} {
		if node != nil && (strings.TrimSpace(node.Device) == "" || strings.TrimSpace(node.Protocol) == "") {
			return nil, nil, errors.New("forward: a router or origin needs a device and a protocol")
		}
	}
	out := new(ports.RoutingPrefixSearchResult)
	resp, err := s.post(ctx, networkID, "rp-prefix-search", snapshotID, "Routing.RPPrefixSearch", query, out)
	return out, resp, err
}

// RPPrefixTrace returns the propagation of a prefix from its origin to a router (POST .../rp-prefix-trace): a list of paths, each a list
// of hops. The hops carry the node key RoutingNode.Ref needs.
func (s *Routing) RPPrefixTrace(ctx context.Context, networkID, snapshotID string, request ports.RPPrefixTrace) ([][]ports.RoutingNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	var out [][]ports.RoutingNode
	resp, err := s.post(ctx, networkID, "rp-prefix-trace", snapshotID, "Routing.RPPrefixTrace", request, &out)
	return out, resp, err
}

// RPPrefixDetails returns the route attributes and policies at one hop of a trace (POST .../rp-prefix-details). A node that does not
// resolve is a RoutingLookupError.
func (s *Routing) RPPrefixDetails(ctx context.Context, networkID, snapshotID string, request ports.RPPrefixDetailsRequest) (*ports.RPPrefixDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(request.Prefix) == "" || len(request.Node.NodeKey) == 0 {
		return nil, nil, errors.New("forward: prefix and node (with its key) are required")
	}
	var raw json.RawMessage
	resp, err := s.post(ctx, networkID, "rp-prefix-details", snapshotID, "Routing.RPPrefixDetails", request, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(ports.RPPrefixDetails)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// RPDevicePrefixInfo returns what one device has for a prefix, per VRF and protocol (GET .../devices/{device}?view=rp-prefix-info&prefix=).
// A device with no model for it answers a RoutingLookupError.
func (s *Routing) RPDevicePrefixInfo(ctx context.Context, networkID, snapshotID, device, prefix string) (*ports.RPDevicePrefixInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.RPDevicePrefixInfo)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	// A build without the view=rp-prefix-info mapping answers this URL with the plain device (200), which would read as "no routes".
	var probe struct {
		VRFs json.RawMessage `json:"vrfs"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.VRFs == nil {
		return nil, resp, fmt.Errorf("%w: this build answered the device, not a prefix view (use DevicePrefixInfo)", ports.ErrEndpointNotServed)
	}
	return out, resp, nil
}
