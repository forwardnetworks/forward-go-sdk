package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// SyntheticNodesService manages the synthetic devices a network can carry:
// the single internet node, intranet nodes, and L3 VPNs. They are not
// collected from real equipment -- they stand in for the parts of the world
// outside the modelled network, and Skyforge derives them from the topology.
//
// Preview: these routes are not in the published OpenAPI set.
type SyntheticNodesService service

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

// collectionPath returns the list/PUT-all route for a kind, and "" for the
// internet node, which has no collection.
func (s *SyntheticNodesService) collectionPath(networkID string, kind SyntheticNodeKind) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	base := "/api/networks/" + url.PathEscape(networkID)
	switch kind {
	case SyntheticIntranet:
		return base + "/intranet-nodes", nil
	case SyntheticL3VPN:
		return base + "/l3-vpns", nil
	case SyntheticL2VPN:
		return base + "/l2-vpns", nil
	case SyntheticAdjacentNetwork:
		return base + "/adjacent-networks", nil
	case SyntheticInternet:
		return "", nil
	default:
		return "", errors.New("forward: unsupported synthetic node kind " + string(kind))
	}
}

func (s *SyntheticNodesService) nodePath(networkID string, kind SyntheticNodeKind, name string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	base := "/api/networks/" + url.PathEscape(networkID)
	switch kind {
	case SyntheticInternet:
		return base + "/internet-node", nil
	case SyntheticIntranet, SyntheticL3VPN, SyntheticL2VPN, SyntheticAdjacentNetwork:
		name = strings.TrimSpace(name)
		if name == "" {
			// Never let an empty name collapse onto the COLLECTION route: a
			// PUT there replaces every node, and a DELETE there is worse.
			return "", errors.New("forward: synthetic node name is required")
		}
		suffix := map[SyntheticNodeKind]string{SyntheticIntranet: "/intranet-nodes/", SyntheticL3VPN: "/l3-vpns/", SyntheticL2VPN: "/l2-vpns/", SyntheticAdjacentNetwork: "/adjacent-networks/"}[kind]
		return base + suffix + url.PathEscape(name), nil
	default:
		return "", errors.New("forward: unsupported synthetic node kind " + string(kind))
	}
}

// opName gives each (kind, verb) pair its OWN bounded operation name. The
// coverage catalog maps one symbol to one route, and these methods address a
// different route per kind, so a single name would be ambiguous -- and the
// telemetry would lump an internet-node write together with an L3 VPN write.
func (k SyntheticNodeKind) opName(verb string) string {
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

// Get returns one synthetic node, or (nil, nil) when it does not exist.
// Absence is not an error: callers read before writing precisely to find out
// whether to create.
func (s *SyntheticNodesService) Get(ctx context.Context, networkID string, kind SyntheticNodeKind, name string) (*SyntheticNode, *Response, error) {
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.opName("Get"))
	out := new(SyntheticNode)
	resp, err := s.client.Do(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Put creates or replaces one synthetic node.
func (s *SyntheticNodesService) Put(ctx context.Context, networkID string, kind SyntheticNodeKind, name string, node SyntheticNode) (*Response, error) {
	if kind == SyntheticL2VPN || kind == SyntheticAdjacentNetwork {
		return nil, errors.New("forward: Put does not model " + string(kind) + " connections and would drop them; use SetQuery to change its query")
	}
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, err
	}
	node.QueryResult = nil // computed by Forward, never sent
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, node)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, kind.opName("Put"))
	return s.client.Do(req, nil)
}

// SetQuery points a node at a saved NQE query (queryID "Q_..." in the organization's library, "FQ_..." in Forward's) so Forward generates dynamic connections from its rows, or
// clears it (queryID ""). It changes nothing else on the node, and returns the node as Forward now holds it, including QueryResult (check its
// Error: a query of the wrong row type is accepted and reported there, not refused). Preview: PATCH {"queryId": ...} on the node route; the property
// is hidden in Forward's spec (FWD-37393). Clearing sends an explicit null, which the server reads as "remove" (JsonProp).
func (s *SyntheticNodesService) SetQuery(ctx context.Context, networkID string, kind SyntheticNodeKind, name, queryID string) (*SyntheticNode, *Response, error) {
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, nil, err
	}
	var value any
	if queryID = strings.TrimSpace(queryID); queryID != "" {
		value = queryID
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, map[string]any{"queryId": value})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.opName("SetQuery"))
	out := new(SyntheticNode)
	resp, err := s.client.Do(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ComputeQuery runs queryID as Forward would for a node of this kind and returns the connections it would generate, or the error it hits,
// without changing anything: a dry run for SetQuery. Forward runs the query's last commit against the network's latest processed snapshot,
// and checks its row type against the kind's connection record (InetConnection for internet and intranet nodes, L3VpnConnection for L3
// VPNs and adjacent networks, L2VpnConnection for L2 VPNs). Preview: POST ?action=computeNqeBasedConnections&queryId= on the kind's
// collection route, or the internet node's own route (served on primary 15398425a69 and stable 67e89c87124).
func (s *SyntheticNodesService) ComputeQuery(ctx context.Context, networkID string, kind SyntheticNodeKind, queryID string) (*SyntheticQueryResult, *Response, error) {
	if queryID = strings.TrimSpace(queryID); queryID == "" {
		return nil, nil, errors.New("forward: NQE query ID is required")
	}
	path, err := s.collectionPath(networkID, kind)
	if err != nil {
		return nil, nil, err
	}
	if kind == SyntheticInternet {
		// The internet node has no collection; its compute action is on the node route.
		if path, err = s.nodePath(networkID, kind, ""); err != nil {
			return nil, nil, err
		}
	}
	query := url.Values{"action": []string{"computeNqeBasedConnections"}, "queryId": []string{queryID}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.opName("ComputeQuery"))
	out := new(SyntheticQueryResult)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// SyntheticDeviceQuery is a saved NQE query whose rows fit a synthetic node kind.
type SyntheticDeviceQuery struct {
	QueryID string `json:"queryId"`
	Path    string `json:"path"`
}

// CompatibleQueries lists the saved NQE queries a node of this kind can use: parameterless queries whose row type is the kind's connection
// record, typed against the network's latest processed snapshot (empty when the network has none). Preview: GET
// /api/synthetic-device-queries?type=&networkId= (SyntheticDeviceController; served on primary 15398425a69 and stable 67e89c87124).
func (s *SyntheticNodesService) CompatibleQueries(ctx context.Context, networkID string, kind SyntheticNodeKind) ([]SyntheticDeviceQuery, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	deviceType, ok := map[SyntheticNodeKind]string{
		SyntheticInternet: "INTERNET", SyntheticIntranet: "INTRANET", SyntheticL3VPN: "L3VPN",
		SyntheticL2VPN: "L2VPN", SyntheticAdjacentNetwork: "ADJACENT_NETWORK",
	}[kind]
	if !ok {
		return nil, nil, errors.New("forward: unsupported synthetic node kind " + string(kind))
	}
	query := url.Values{"type": []string{deviceType}, "networkId": []string{networkID}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/synthetic-device-queries?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SyntheticNodes.CompatibleQueries")
	result := listResponse[SyntheticDeviceQuery]{Keys: []string{"queries"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Delete removes one synthetic node. A 404 is success. The internet node
// cannot be deleted -- there is one per network and no route for it.
func (s *SyntheticNodesService) Delete(ctx context.Context, networkID string, kind SyntheticNodeKind, name string) (*Response, error) {
	if kind == SyntheticInternet {
		return nil, errors.New("forward: the internet node cannot be deleted")
	}
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, kind.opName("Delete"))
	resp, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return resp, nil
	}
	return resp, err
}

// List returns the synthetic nodes of a kind. The internet node has no
// collection and yields nothing.
//
// Forward returns them as an ARRAY under a per-kind key -- {"l3Vpns": [...]},
// {"intranetNodes": [...]} -- which is worth stating because the Java field
// behind it is a MAP keyed by name; the serialized form is the array, pinned
// by Forward's own L3VpnListTest. A bare array is accepted too.
func (s *SyntheticNodesService) List(ctx context.Context, networkID string, kind SyntheticNodeKind) ([]SyntheticNode, *Response, error) {
	path, err := s.collectionPath(networkID, kind)
	if err != nil {
		return nil, nil, err
	}
	if path == "" {
		return nil, nil, nil
	}
	result := listResponse[SyntheticNode]{Keys: []string{"l3Vpns", "intranetNodes", "l2Vpns", "adjacentNetworks", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.opName("List"))
	resp, err := s.client.doRequired(req, &result)
	if isStatus(err, http.StatusNotFound) {
		// A network with no such collection is not an error; it has none.
		return nil, resp, nil
	}
	return result.Items, resp, err
}

// The per-resource methods below are the named surface the coverage catalog
// maps route-by-route. They are thin: the behaviour lives in the kind-based
// calls above, which is what a caller iterating over kinds should use.

// GetInternetNode returns the network's single internet node, or nil if it
// has none.
func (s *SyntheticNodesService) GetInternetNode(ctx context.Context, networkID string) (*SyntheticNode, *Response, error) {
	return s.Get(ctx, networkID, SyntheticInternet, "")
}

// PutInternetNode creates or replaces the network's internet node.
func (s *SyntheticNodesService) PutInternetNode(ctx context.Context, networkID string, node SyntheticNode) (*Response, error) {
	return s.Put(ctx, networkID, SyntheticInternet, "", node)
}

// ListIntranetNodes returns the network's intranet nodes.
func (s *SyntheticNodesService) ListIntranetNodes(ctx context.Context, networkID string) ([]SyntheticNode, *Response, error) {
	return s.List(ctx, networkID, SyntheticIntranet)
}

// GetIntranetNode returns one intranet node, or nil if absent.
func (s *SyntheticNodesService) GetIntranetNode(ctx context.Context, networkID, name string) (*SyntheticNode, *Response, error) {
	return s.Get(ctx, networkID, SyntheticIntranet, name)
}

// PutIntranetNode creates or replaces one intranet node.
func (s *SyntheticNodesService) PutIntranetNode(ctx context.Context, networkID, name string, node SyntheticNode) (*Response, error) {
	return s.Put(ctx, networkID, SyntheticIntranet, name, node)
}

// DeleteIntranetNode removes one intranet node. A 404 is success.
func (s *SyntheticNodesService) DeleteIntranetNode(ctx context.Context, networkID, name string) (*Response, error) {
	return s.Delete(ctx, networkID, SyntheticIntranet, name)
}

// ListL3VPNs returns the network's L3 VPNs.
func (s *SyntheticNodesService) ListL3VPNs(ctx context.Context, networkID string) ([]SyntheticNode, *Response, error) {
	return s.List(ctx, networkID, SyntheticL3VPN)
}

// GetL3VPN returns one L3 VPN, or nil if absent.
func (s *SyntheticNodesService) GetL3VPN(ctx context.Context, networkID, name string) (*SyntheticNode, *Response, error) {
	return s.Get(ctx, networkID, SyntheticL3VPN, name)
}

// PutL3VPN creates or replaces one L3 VPN.
func (s *SyntheticNodesService) PutL3VPN(ctx context.Context, networkID, name string, node SyntheticNode) (*Response, error) {
	return s.Put(ctx, networkID, SyntheticL3VPN, name, node)
}

// DeleteL3VPN removes one L3 VPN. A 404 is success.
func (s *SyntheticNodesService) DeleteL3VPN(ctx context.Context, networkID, name string) (*Response, error) {
	return s.Delete(ctx, networkID, SyntheticL3VPN, name)
}

// InternetConnectionSuggestion is a connection Forward proposes for the internet node: an interface that appears to face the internet.
type InternetConnectionSuggestion struct {
	UplinkInterface             string `json:"uplinkInterface"`
	UplinkInterfaceDescription  string `json:"uplinkInterfaceDescription,omitempty"`
	VLAN                        *int   `json:"vlan,omitempty"`
	GatewayInterface            string `json:"gatewayInterface,omitempty"`
	GatewayInterfaceDescription string `json:"gatewayInterfaceDescription,omitempty"`
}

// InternetConnectionSuggestions returns the connections Forward suggests for the internet node, computed from the latest processed snapshot
// (GET /api/networks/{id}/internet-node/connection-suggestions): the GUI's "suggested connections". It is read-only; add one with Put or Patch on the
// internet node. Preview: not in the published spec.
func (s *SyntheticNodesService) InternetConnectionSuggestions(ctx context.Context, networkID string) ([]InternetConnectionSuggestion, *Response, error) {
	path, err := s.nodePath(networkID, SyntheticInternet, "")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[InternetConnectionSuggestion]{Keys: []string{"suggestions", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/connection-suggestions", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SyntheticNodes.InternetConnectionSuggestions")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}
