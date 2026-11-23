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

// RoutingService reads the routing-protocol model Forward builds for a snapshot: where a prefix originates, which devices install,
// filter or lose it, the hop-by-hop propagation, the policies applied at each hop, and what each device has installed. It is the
// "routing app" behind Forward's BGP prefix search. Preview: appserver PredictController, not the published OpenAPI. Every route needs
// the NetworkOperation BGP_PREFIX_SEARCH (named RP_PREFIX_SEARCH in newer Forward), which only the network role ADMIN holds (an
// OPERATOR gets 403), the organization property PREDICT_MODELING, and a snapshot whose processing has reached the creation stage.
//
// The routes are the bgp-prefix-* spellings (bgp-prefix-search, -trace, -details, devices/{d}?view=bgp-prefix-info and
// bgp-installed-prefixes) because both Forward builds this SDK pins serve them. Newer Forward source marks them @Deprecated in
// favour of rp-prefix-* (and view=rp-prefix-info), which delegate to the same handlers and cover non-BGP protocols in the same way;
// stable b1feb6fe1c34 does not have the rp-* names, and primary 5f7e4ec1841 has them with the old body shapes, so the methods here use
// the bgp-* names. RPPrefixSearch, RPPrefixTrace, RPPrefixDetails and RPDevicePrefixInfo (routing_rp.go) are the rp-* routes in master's
// newer shapes, which also trace OSPF, IS_IS, OMP and CONFIG nodes. Move to them when the pins move. Forward master's bgp-* routes take the
// NEW bodies too (a protocol on every router and origin, node keys), so the methods here fail on master with a router, origin or node:
// they are for the pinned builds only. The RP* methods on a pinned primary answer a 400 or 500 (it reads the old bodies), not
// ErrEndpointNotServed.
type Routing service

// decodeLookup decodes raw into out, or returns the lookup error Forward sent in its place.
func decodeLookup(raw json.RawMessage, out any) error {
	var probe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Error != "" {
		return &ports.RoutingLookupError{Message: probe.Error}
	}
	return json.Unmarshal(raw, out)
}

func (s *Routing) path(networkID string, rest ...string) (string, error) {
	return s.client.networkSubPath(networkID, rest...)
}

// routingSnapshot requires a snapshot. That is stricter than Forward, which falls back to the network's latest processed snapshot.
func routingSnapshot(snapshotID string) (url.Values, error) {
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID == "" {
		return nil, errors.New("forward: snapshot ID is required")
	}
	return url.Values{"snapshotId": []string{snapshotID}}, nil
}

func validRouteOutcome(outcome string) bool {
	return outcome == "" || outcome == ports.RouteOutcomeInstalled || outcome == ports.RouteOutcomeFilteredOut || outcome == ports.RouteOutcomeNotPreferred
}

func (s *Routing) post(ctx context.Context, networkID, route, snapshotID, operation string, body, out any) (*ports.Response, error) {
	query, err := routingSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	path, err := s.path(networkID, route)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), body)
	if err != nil {
		return nil, err
	}
	return s.client.doRequired(markOperation(req, operation), out)
}

// PrefixSearch answers "where does this prefix come from and who has it?" on a snapshot (POST .../bgp-prefix-search). A prefix Forward
// has no routing data for gives an empty result, not an error.
func (s *Routing) PrefixSearch(ctx context.Context, networkID, snapshotID string, query ports.RoutingPrefixSearch) (*ports.RoutingPrefixSearchResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(query.Prefix) == "" {
		return nil, nil, errors.New("forward: prefix is required")
	}
	if !validRouteOutcome(query.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", query.Outcome)
	}
	query.RouterKey = keyFor(query.Router, query.RouterKey)
	query.OriginatedFromKey = keyFor(query.OriginatedFrom, query.OriginatedFromKey)
	out := new(ports.RoutingPrefixSearchResult)
	resp, err := s.post(ctx, networkID, "bgp-prefix-search", snapshotID, "Routing.PrefixSearch", query, out)
	return out, resp, err
}

// keyFor returns key if set, else a BGP key made from node, else nil.
func keyFor(node *ports.DeviceVRF, key *ports.DeviceVRFProtocol) *ports.DeviceVRFProtocol {
	if key != nil || node == nil {
		return key
	}
	return &ports.DeviceVRFProtocol{Device: node.Device, VRF: node.VRF, Protocol: "BGP"}
}

// PrefixTrace returns the propagation of a prefix from its origin to a router (POST .../bgp-prefix-trace): a list of paths, each a list
// of hops from origin to the router. Take a router, origin and outcome from PrefixSearch. A prefix with no routing data gives no paths.
func (s *Routing) PrefixTrace(ctx context.Context, networkID, snapshotID string, request ports.RoutingTraceRequest) ([][]ports.RoutingNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	request.Router.Device = strings.TrimSpace(request.Router.Device)
	request.OriginatedFrom.Device = strings.TrimSpace(request.OriginatedFrom.Device)
	if strings.TrimSpace(request.Prefix) == "" || request.Router.Device == "" || request.OriginatedFrom.Device == "" || request.Outcome == "" {
		return nil, nil, errors.New("forward: prefix, router, originatedFrom and outcome are required")
	}
	if !validRouteOutcome(request.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", request.Outcome)
	}
	request.RouterKey = keyFor(&request.Router, request.RouterKey)
	request.OriginatedFromKey = keyFor(&request.OriginatedFrom, request.OriginatedFromKey)
	var out [][]ports.RoutingNode
	resp, err := s.post(ctx, networkID, "bgp-prefix-trace", snapshotID, "Routing.PrefixTrace", request, &out)
	return out, resp, err
}

// PrefixDetails returns the route attributes and policies at one hop of a trace (POST .../bgp-prefix-details).
func (s *Routing) PrefixDetails(ctx context.Context, networkID, snapshotID string, request ports.RoutingPrefixDetailsRequest) (*ports.RoutingPrefixDetails, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	request.Node.Device = strings.TrimSpace(request.Node.Device)
	if strings.TrimSpace(request.Prefix) == "" || request.Node.Device == "" {
		return nil, nil, errors.New("forward: prefix and node are required")
	}
	var raw json.RawMessage
	resp, err := s.post(ctx, networkID, "bgp-prefix-details", snapshotID, "Routing.PrefixDetails", request, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(ports.RoutingPrefixDetails)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DevicePrefixInfo returns what one device has for a prefix: the advertisements it received, with their outcome and origin, and the
// more specific routes it installed (GET .../devices/{device}?view=bgp-prefix-info&prefix=). A device that is not in the snapshot makes
// Forward answer 500 (an unhandled lookup), and one it has no model for answers 200 with an error, returned as a RoutingLookupError;
// with a retry policy set, the 500 is retried like any GET.
func (s *Routing) DevicePrefixInfo(ctx context.Context, networkID, snapshotID, device, prefix string) (*ports.RoutingDevicePrefixInfo, *ports.Response, error) {
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
	query.Set("view", "bgp-prefix-info")
	query.Set("prefix", strings.TrimSpace(prefix))
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	resp, err := s.client.doRequired(markOperation(req, "Routing.DevicePrefixInfo"), &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(ports.RoutingDevicePrefixInfo)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// InstalledPrefixes lists the prefixes BGP nodes installed after modelling (GET .../bgp-installed-prefixes), for one device or, with no
// device, across the snapshot (vendor-balanced and capped at Limit).
func (s *Routing) InstalledPrefixes(ctx context.Context, networkID, snapshotID string, options ports.RoutingInstalledOptions) (*ports.RoutingInstalledPrefixes, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	query, err := routingSnapshot(snapshotID)
	if err != nil {
		return nil, nil, err
	}
	if device := strings.TrimSpace(options.Device); device != "" {
		if len(options.Vendors) > 0 {
			return nil, nil, errors.New("forward: device and vendors cannot be combined; the vendor filter applies only across all devices")
		}
		query.Set("deviceName", device)
	}
	for _, vendor := range options.Vendors {
		if vendor = strings.TrimSpace(vendor); vendor != "" {
			query.Add("vendors", vendor)
		}
	}
	if options.Limit > 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	path, err := s.path(networkID, "bgp-installed-prefixes")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.RoutingInstalledPrefixes)
	resp, err := s.client.doRequired(markOperation(req, "Routing.InstalledPrefixes"), out)
	return out, resp, err
}
