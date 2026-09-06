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

type SyntheticDevicePort struct {
	Device string `json:"device"`
	Iface  string `json:"iface"`
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
	case SyntheticIntranet, SyntheticL3VPN:
		name = strings.TrimSpace(name)
		if name == "" {
			// Never let an empty name collapse onto the COLLECTION route: a
			// PUT there replaces every node, and a DELETE there is worse.
			return "", errors.New("forward: synthetic node name is required")
		}
		suffix := "/intranet-nodes/"
		if kind == SyntheticL3VPN {
			suffix = "/l3-vpns/"
		}
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
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, node)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, kind.opName("Put"))
	return s.client.Do(req, nil)
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
	result := listResponse[SyntheticNode]{Keys: []string{"l3Vpns", "intranetNodes", "items", "data", "results"}}
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
