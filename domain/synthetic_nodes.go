package domain

// SyntheticNode is one synthetic device. Connections is deliberately opaque
// json.RawMessage-free: the connection shape differs per node kind and per
// Forward build, and this SDK does not model it -- callers send back what
// they built.
type SyntheticNode struct {
	Name        string              `json:"name"`
	Connections []SyntheticNodeConn `json:"connections"`
	// QueryID is the id of a saved NQE query (Q_... in the organization's library, FQ_... in Forward's) whose rows Forward turns into extra, dynamic connections of
	// this node on the latest processed snapshot. Preview: the property is not in Forward's published spec (the server marks it hidden,
	// FWD-37393) but is live on current builds. A node READ with a QueryID and written back without it LOSES the query, so a caller that
	// round-trips a node must keep this field.
	QueryID string `json:"queryId,omitempty"`
	// QueryResult is what the query produced, computed by Forward; it is read-only and never sent.
	QueryResult *SyntheticQueryResult `json:"queryResult,omitempty"`

	// Translations and SubnetsToExclude exist on the INTERNET node only. They are carried so that a node read and written back with Put
	// keeps them: Forward's internet-node PUT replaces the whole node, and an absent subnetsToExclude or translations becomes an empty one
	// (InternetNode's JSON creator), which would silently wipe them. Change the excluded subnets on their own with
	// SetInternetExcludedSubnets.
	Translations     []SyntheticNatEntry `json:"translations,omitempty"`
	SubnetsToExclude []string            `json:"subnetsToExclude,omitempty"`
}

// SyntheticNatEntry is one static NAT translation on the internet node: OutsideAddress is the public address InsideAddress appears as.
type SyntheticNatEntry struct {
	InsideAddress  string `json:"insideAddress"`
	OutsideAddress string `json:"outsideAddress"`
}

// SyntheticQueryResult is the outcome of the node's query: the connections it generated, or why it could not.
type SyntheticQueryResult struct {
	Connections []SyntheticNodeConn  `json:"connections,omitempty"`
	Error       *SyntheticQueryError `json:"error,omitempty"`
}

// SyntheticQueryError says why a node's query produced no connections. Status is Forward's own code: NO_LATEST_SNAPSHOT, QUERY_RUN_ERROR,
// COLUMN_DATATYPE_MISMATCH (the rows are not the connection type this kind needs), MISSING_REQUIRED_COLUMNS, INVALID_IDENTIFIER or QUERY_MISSING.
type SyntheticQueryError struct {
	Message string `json:"errorMsg"`
	Status  string `json:"status"`
}

// SyntheticNodeConn is one uplink into a synthetic node. Gateway, VLAN and
// the advertise flag are POINTERS because "not stated" and "stated as
// zero/false" are different requests to Forward.
type SyntheticNodeConn struct {
	UplinkPort             SyntheticDevicePort   `json:"uplinkPort"`
	GatewayPort            *SyntheticDevicePort  `json:"gatewayPort,omitempty"`
	Vlan                   *int                  `json:"vlan,omitempty"`
	Name                   string                `json:"name,omitempty"`
	Site                   string                `json:"site,omitempty"`
	SubnetAutoDiscovery    string                `json:"subnetAutoDiscovery,omitempty"`
	Subnets                []string              `json:"subnets,omitempty"`
	PeerIPs                []string              `json:"peerIps,omitempty"`
	BackdoorLinkPorts      []SyntheticDevicePort `json:"backdoorLinkPorts,omitempty"`
	AdvertisesDefaultRoute *bool                 `json:"advertisesDefaultRoute,omitempty"`
	VRF                    string                `json:"vrf,omitempty"`
}

// SyntheticDevicePort names one end of a connection. The wire fields are
// "device" and "port" -- measured, not guessed: Skyforge has been writing
// synthetic nodes with exactly this shape in production.
type SyntheticDevicePort struct {
	Device string `json:"device"`
	Port   string `json:"port"`
}

// SyntheticNodeKind selects which resource a call addresses. Forward gives
// each its own route rather than one polymorphic collection.
type SyntheticNodeKind string

const (
	// SyntheticInternet is the SINGLE internet node of a network: one per
	// network, addressed without a name, so it has no list and no delete.
	SyntheticInternet SyntheticNodeKind = "internet"
	SyntheticIntranet SyntheticNodeKind = "intranet"
	SyntheticL3VPN    SyntheticNodeKind = "l3vpn"
	// SyntheticL2VPN and SyntheticAdjacentNetwork exist so a node's NQE query can be read, previewed and attached (Get, List, SetQuery,
	// ComputeQuery, CompatibleQueries) and the node deleted. Their bodies are not fully modelled -- an L2 VPN connection has an edge
	// interface, not an uplink, and an adjacent network carries ownedSubnets -- so Put refuses them rather than write back a node with
	// those fields silently dropped.
	SyntheticL2VPN           SyntheticNodeKind = "l2vpn"
	SyntheticAdjacentNetwork SyntheticNodeKind = "adjacent-network"
)

// OpName gives each (kind, verb) pair its OWN bounded operation name. The
// coverage catalog maps one symbol to one route, and these methods address a
// different route per kind, so a single name would be ambiguous -- and the
// telemetry would lump an internet-node write together with an L3 VPN write.
func (k SyntheticNodeKind) OpName(verb string) string {
	switch k {
	case SyntheticInternet:
		return "SyntheticNodes." + verb + "InternetNode"
	case SyntheticIntranet:
		if verb == "List" {
			return "SyntheticNodes.ListIntranetNodes"
		}
		return "SyntheticNodes." + verb + "IntranetNode"
	case SyntheticL3VPN:
		if verb == "List" {
			return "SyntheticNodes.ListL3VPNs"
		}
		return "SyntheticNodes." + verb + "L3VPN"
	case SyntheticL2VPN:
		return "SyntheticNodes." + verb + "L2VPN"
	case SyntheticAdjacentNetwork:
		return "SyntheticNodes." + verb + "AdjacentNetwork"
	default:
		return "SyntheticNodes." + verb
	}
}

// SyntheticDeviceQuery is a saved NQE query whose rows fit a synthetic node kind.
type SyntheticDeviceQuery struct {
	QueryID string `json:"queryId"`
	Path    string `json:"path"`
}

// InternetConnectionSuggestion is a connection Forward proposes for the internet node: an interface that appears to face the internet.
type InternetConnectionSuggestion struct {
	UplinkInterface             string `json:"uplinkInterface"`
	UplinkInterfaceDescription  string `json:"uplinkInterfaceDescription,omitempty"`
	VLAN                        *int   `json:"vlan,omitempty"`
	GatewayInterface            string `json:"gatewayInterface,omitempty"`
	GatewayInterfaceDescription string `json:"gatewayInterfaceDescription,omitempty"`
}
