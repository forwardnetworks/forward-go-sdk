package domain

import (
	"encoding/json"
	"errors"
)

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
