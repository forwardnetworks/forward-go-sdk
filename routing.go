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

// RoutingService reads the routing-protocol model Forward builds for a snapshot: where a prefix originates, which devices install,
// filter or lose it, the hop-by-hop propagation, the policies applied at each hop, and what each device has installed. It is the
// "routing app" behind Forward's BGP prefix search. Preview: appserver PredictController, not the published OpenAPI. Every route needs
// the NetworkOperation BGP_PREFIX_SEARCH (named RP_PREFIX_SEARCH in newer Forward), which only the network role ADMIN holds (an
// OPERATOR gets 403), the organization property PREDICT_MODELING, and a snapshot whose processing has reached the creation stage.
//
// The routes are the bgp-prefix-* spellings (bgp-prefix-search, -trace, -details, devices/{d}?view=bgp-prefix-info and
// bgp-installed-prefixes) because both Forward builds this SDK pins serve them. Newer Forward source marks them @Deprecated in
// favour of rp-prefix-* (and view=rp-prefix-info), which delegate to the same handlers and cover non-BGP protocols in the same way;
// the pinned build does not have the rp-* names, so they are not used here. Move to them when the pins move.
type RoutingService service

// Trace outcomes of a prefix on a router.
const (
	RouteOutcomeInstalled    = "INSTALLED"
	RouteOutcomeFilteredOut  = "FILTERED_OUT"
	RouteOutcomeNotPreferred = "NOT_PREFERRED"
)

// DeviceVRF names a node by device and VRF.
type DeviceVRF struct {
	Device string `json:"device"`
	VRF    string `json:"vrf,omitempty"`
}

// DeviceVRFProtocol is DeviceVRF plus the routing protocol (BGP, OSPF, IS_IS, EIGRP, RIP, CONFIG, OMP, STP), which tells apart nodes
// that device and VRF alone do not.
type DeviceVRFProtocol struct {
	Device   string `json:"device"`
	VRF      string `json:"vrf,omitempty"`
	Protocol string `json:"protocol"`
}

// RoutingPrefixSearch is the question of PrefixSearch. Prefix is required. Leave Router and OriginatedFrom empty to search every
// router and origin; set Outcome to look only at routers where the prefix has that outcome. RouterKey and OriginatedFromKey supersede
// Router and OriginatedFrom and add the protocol; current Forward builds require the key whenever the router or origin is set (the
// answer is a 500 without it), so PrefixSearch fills a missing key from Router or OriginatedFrom with the protocol BGP. Set the key
// yourself for another protocol.
type RoutingPrefixSearch struct {
	Prefix            string             `json:"prefix"`
	Router            *DeviceVRF         `json:"router,omitempty"`
	OriginatedFrom    *DeviceVRF         `json:"originatedFrom,omitempty"`
	Outcome           string             `json:"outcome,omitempty"`
	RouterKey         *DeviceVRFProtocol `json:"routerKey,omitempty"`
	OriginatedFromKey *DeviceVRFProtocol `json:"originatedFromKey,omitempty"`
}

// RoutingDevice is a node in a search result.
type RoutingDevice struct {
	Device     string `json:"device"`
	VRF        string `json:"vrf"`
	LocationID string `json:"locationId,omitempty"`
	Protocol   string `json:"protocol"`
}

// RoutingPrefixSearchResult says where a prefix originates (Origin) and, for each outcome, the nodes with that outcome. It is empty
// when Forward has no routing data for the prefix.
type RoutingPrefixSearchResult struct {
	Origin           []RoutingDevice            `json:"origin"`
	DevicesByOutcome map[string][]RoutingDevice `json:"devicesByOutcome"`
}

// RoutingNode is one hop of a prefix trace. Pass it back unchanged to PrefixDetails: Forward needs fields (the node key, the route
// set) that this type does not interpret, so the node keeps the JSON it was read from and sends that. The node key is in Raw, and
// MarshalJSON sends Raw as is, ignoring edits to the fields, whenever it is set; clear Raw to send the fields instead (a hand-built
// node is rarely enough for PrefixDetails).
type RoutingNode struct {
	Device     string `json:"device"`
	VRF        string `json:"vrf"`
	RouteSetID int    `json:"routeSetId"`
	ASNumber   int64  `json:"asNumber"`
	BoundaryID string `json:"boundaryId,omitempty"`
	LocationID string `json:"locationId,omitempty"`
	RouterID   string `json:"routerId,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	// ImportPolicyNames and ExportPolicyNames are the policies applied on the way in and out of this node.
	ImportPolicyNames []string `json:"importPolicyNames,omitempty"`
	ExportPolicyNames []string `json:"exportPolicyNames,omitempty"`
	// Raw is the node exactly as Forward sent it.
	Raw json.RawMessage `json:"-"`
}

func (n *RoutingNode) UnmarshalJSON(data []byte) error {
	type plain RoutingNode
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*n = RoutingNode(p)
	n.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (n RoutingNode) MarshalJSON() ([]byte, error) {
	if len(n.Raw) > 0 {
		return n.Raw, nil
	}
	type plain RoutingNode
	return json.Marshal(plain(n))
}

// RoutingPrefixDetailsRequest asks for the route attributes at Node and the policies applied there. Forward needs the neighbours to
// find the policies: PreviousNode (and PreviousPreviousNode) for import policies, NextNode for export policies. Take all of them from
// one trace path.
type RoutingPrefixDetailsRequest struct {
	Prefix               string       `json:"prefix"`
	Node                 RoutingNode  `json:"node"`
	PreviousNode         *RoutingNode `json:"previousNode,omitempty"`
	NextNode             *RoutingNode `json:"nextNode,omitempty"`
	PreviousPreviousNode *RoutingNode `json:"previousPreviousNode,omitempty"`
}

// RoutingPrefixDetails is the route a node holds for the prefix. Attributes is protocol-specific and carries a "protocol" member (BGP:
// communities, asPath, localPref, med, bgpNextHopAddr, origin, ...; OSPF: cost, lsaType, ...; CONFIG: nextHop, routeType, ...); Policies
// maps a rule type (IMPORT, EXPORT, REDISTRIBUTE, ...) to the matched policy rules. Both are left raw because their shapes vary by
// protocol and vendor.
type RoutingPrefixDetails struct {
	Prefix     string                     `json:"prefix"`
	Device     string                     `json:"device"`
	VRF        string                     `json:"vrf"`
	Attributes json.RawMessage            `json:"attributes,omitempty"`
	Policies   map[string]json.RawMessage `json:"policies,omitempty"`
}

// RoutingReceivedPrefix is one received advertisement of the prefix on a device VRF.
type RoutingReceivedPrefix struct {
	Nexthop        string    `json:"nexthop"`
	Outcome        string    `json:"outcome"`
	OriginatedFrom DeviceVRF `json:"originatedFrom"`
}

// RoutingDeviceVRFPrefixInfo is what one VRF of a device has for the prefix.
type RoutingDeviceVRFPrefixInfo struct {
	VRF                         string                  `json:"vrf"`
	RecvAdvertisements          []RoutingReceivedPrefix `json:"recvAdvertisements"`
	MoreSpecificInstalledRoutes []string                `json:"moreSpecificInstalledRoutes"`
}

// RoutingDevicePrefixInfo is what a device has for a prefix, per VRF.
type RoutingDevicePrefixInfo struct {
	Device string                       `json:"device"`
	Vendor string                       `json:"vendor"`
	Model  string                       `json:"model"`
	VRFs   []RoutingDeviceVRFPrefixInfo `json:"vrfs"`
}

// RoutingNodePrefixes lists the prefixes installed on one modelled node (device and VRF).
type RoutingNodePrefixes struct {
	Device   string   `json:"device"`
	VendorOS string   `json:"vendorOs"`
	VRF      string   `json:"vrf"`
	Prefixes []string `json:"prefixes"`
}

// RoutingInstalledPrefixes is the prefixes BGP nodes installed in their Loc-RIB after modelling.
type RoutingInstalledPrefixes struct {
	TotalPrefixCount int                   `json:"totalPrefixCount"`
	Nodes            []RoutingNodePrefixes `json:"nodes"`
}

// RoutingInstalledOptions narrows InstalledPrefixes. Vendors are Forward's lowercase vendor-OS names (for example cisco_ios; other case
// is a 400, and responses carry the lowercase form). Device and Vendors cannot be combined (the vendor filter applies across all
// devices). Limit caps the total number of prefixes returned and defaults to 200 on Forward; a negative or zero value leaves Forward's
// default.
type RoutingInstalledOptions struct {
	Device  string
	Vendors []string
	Limit   int
}

// RoutingLookupError is Forward's HTTP 200 answer {"error": "..."} for a lookup that matched nothing: a node that does not resolve in
// this snapshot (a hand-built node, one from another snapshot, a stale routeSetId) or a device it has no model for. errors.Is(err,
// ErrRoutingLookup) matches it.
type RoutingLookupError struct{ Message string }

func (e *RoutingLookupError) Error() string { return "forward: routing lookup: " + e.Message }

// Is reports ErrRoutingLookup.
func (e *RoutingLookupError) Is(target error) bool { return target == ErrRoutingLookup }

// ErrRoutingLookup matches any RoutingLookupError.
var ErrRoutingLookup = errors.New("forward: routing lookup found nothing")

// decodeLookup decodes raw into out, or returns the lookup error Forward sent in its place.
func decodeLookup(raw json.RawMessage, out any) error {
	var probe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Error != "" {
		return &RoutingLookupError{Message: probe.Error}
	}
	return json.Unmarshal(raw, out)
}

func (s *RoutingService) path(networkID string, rest ...string) (string, error) {
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
	return outcome == "" || outcome == RouteOutcomeInstalled || outcome == RouteOutcomeFilteredOut || outcome == RouteOutcomeNotPreferred
}

func (s *RoutingService) post(ctx context.Context, networkID, route, snapshotID, operation string, body, out any) (*Response, error) {
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
func (s *RoutingService) PrefixSearch(ctx context.Context, networkID, snapshotID string, query RoutingPrefixSearch) (*RoutingPrefixSearchResult, *Response, error) {
	if strings.TrimSpace(query.Prefix) == "" {
		return nil, nil, errors.New("forward: prefix is required")
	}
	if !validRouteOutcome(query.Outcome) {
		return nil, nil, fmt.Errorf("forward: outcome %q must be INSTALLED, FILTERED_OUT or NOT_PREFERRED", query.Outcome)
	}
	query.RouterKey = keyFor(query.Router, query.RouterKey)
	query.OriginatedFromKey = keyFor(query.OriginatedFrom, query.OriginatedFromKey)
	out := new(RoutingPrefixSearchResult)
	resp, err := s.post(ctx, networkID, "bgp-prefix-search", snapshotID, "Routing.PrefixSearch", query, out)
	return out, resp, err
}

// RoutingTraceRequest names the prefix, the router where to look, the origin it was learned from and the outcome to trace.
// RouterKey and OriginatedFromKey are filled with the protocol BGP when unset (current Forward builds fail without them).
type RoutingTraceRequest struct {
	Prefix            string             `json:"prefix"`
	Router            DeviceVRF          `json:"router"`
	OriginatedFrom    DeviceVRF          `json:"originatedFrom"`
	Outcome           string             `json:"outcome"`
	RouterKey         *DeviceVRFProtocol `json:"routerKey,omitempty"`
	OriginatedFromKey *DeviceVRFProtocol `json:"originatedFromKey,omitempty"`
}

// keyFor returns key if set, else a BGP key made from node, else nil.
func keyFor(node *DeviceVRF, key *DeviceVRFProtocol) *DeviceVRFProtocol {
	if key != nil || node == nil {
		return key
	}
	return &DeviceVRFProtocol{Device: node.Device, VRF: node.VRF, Protocol: "BGP"}
}

// PrefixTrace returns the propagation of a prefix from its origin to a router (POST .../bgp-prefix-trace): a list of paths, each a list
// of hops from origin to the router. Take a router, origin and outcome from PrefixSearch. A prefix with no routing data gives no paths.
func (s *RoutingService) PrefixTrace(ctx context.Context, networkID, snapshotID string, request RoutingTraceRequest) ([][]RoutingNode, *Response, error) {
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
	var out [][]RoutingNode
	resp, err := s.post(ctx, networkID, "bgp-prefix-trace", snapshotID, "Routing.PrefixTrace", request, &out)
	return out, resp, err
}

// PrefixDetails returns the route attributes and policies at one hop of a trace (POST .../bgp-prefix-details).
func (s *RoutingService) PrefixDetails(ctx context.Context, networkID, snapshotID string, request RoutingPrefixDetailsRequest) (*RoutingPrefixDetails, *Response, error) {
	request.Node.Device = strings.TrimSpace(request.Node.Device)
	if strings.TrimSpace(request.Prefix) == "" || request.Node.Device == "" {
		return nil, nil, errors.New("forward: prefix and node are required")
	}
	var raw json.RawMessage
	resp, err := s.post(ctx, networkID, "bgp-prefix-details", snapshotID, "Routing.PrefixDetails", request, &raw)
	if err != nil {
		return nil, resp, err
	}
	out := new(RoutingPrefixDetails)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// DevicePrefixInfo returns what one device has for a prefix: the advertisements it received, with their outcome and origin, and the
// more specific routes it installed (GET .../devices/{device}?view=bgp-prefix-info&prefix=). A device that is not in the snapshot makes
// Forward answer 500 (an unhandled lookup), and one it has no model for answers 200 with an error, returned as a RoutingLookupError;
// with a retry policy set, the 500 is retried like any GET.
func (s *RoutingService) DevicePrefixInfo(ctx context.Context, networkID, snapshotID, device, prefix string) (*RoutingDevicePrefixInfo, *Response, error) {
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
	out := new(RoutingDevicePrefixInfo)
	if err := decodeLookup(raw, out); err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// InstalledPrefixes lists the prefixes BGP nodes installed after modelling (GET .../bgp-installed-prefixes), for one device or, with no
// device, across the snapshot (vendor-balanced and capped at Limit).
func (s *RoutingService) InstalledPrefixes(ctx context.Context, networkID, snapshotID string, options RoutingInstalledOptions) (*RoutingInstalledPrefixes, *Response, error) {
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
	out := new(RoutingInstalledPrefixes)
	resp, err := s.client.doRequired(markOperation(req, "Routing.InstalledPrefixes"), out)
	return out, resp, err
}
