package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type SyntheticNodes service

// collectionPath returns the list/PUT-all route for a kind, and "" for the
// internet node, which has no collection.
func (s *SyntheticNodes) collectionPath(networkID string, kind ports.SyntheticNodeKind) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	base := "/api/networks/" + url.PathEscape(networkID)
	switch kind {
	case ports.SyntheticIntranet:
		return base + "/intranet-nodes", nil
	case ports.SyntheticL3VPN:
		return base + "/l3-vpns", nil
	case ports.SyntheticL2VPN:
		return base + "/l2-vpns", nil
	case ports.SyntheticAdjacentNetwork:
		return base + "/adjacent-networks", nil
	case ports.SyntheticInternet:
		return "", nil
	default:
		return "", errors.New("forward: unsupported synthetic node kind " + string(kind))
	}
}

func (s *SyntheticNodes) nodePath(networkID string, kind ports.SyntheticNodeKind, name string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	base := "/api/networks/" + url.PathEscape(networkID)
	switch kind {
	case ports.SyntheticInternet:
		return base + "/internet-node", nil
	case ports.SyntheticIntranet, ports.SyntheticL3VPN, ports.SyntheticL2VPN, ports.SyntheticAdjacentNetwork:
		name = strings.TrimSpace(name)
		if name == "" {
			// Never let an empty name collapse onto the COLLECTION route: a
			// PUT there replaces every node, and a DELETE there is worse.
			return "", errors.New("forward: synthetic node name is required")
		}
		suffix := map[ports.SyntheticNodeKind]string{ports.SyntheticIntranet: "/intranet-nodes/", ports.SyntheticL3VPN: "/l3-vpns/", ports.SyntheticL2VPN: "/l2-vpns/", ports.SyntheticAdjacentNetwork: "/adjacent-networks/"}[kind]
		return base + suffix + url.PathEscape(name), nil
	default:
		return "", errors.New("forward: unsupported synthetic node kind " + string(kind))
	}
}

// Get returns one synthetic node, or (nil, nil) when it does not exist.
// Absence is not an error: callers read before writing precisely to find out
// whether to create.
func (s *SyntheticNodes) Get(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, name string) (*ports.SyntheticNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.nodePath(networkID, kind, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.OpName("Get"))
	out := new(ports.SyntheticNode)
	resp, err := s.client.Do(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Put creates or replaces one synthetic node.
func (s *SyntheticNodes) Put(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, name string, node ports.SyntheticNode) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if kind == ports.SyntheticL2VPN || kind == ports.SyntheticAdjacentNetwork {
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
	req = markOperation(req, kind.OpName("Put"))
	return s.client.Do(req, nil)
}

// SetQuery points a node at a saved NQE query (queryID "Q_..." in the organization's library, "FQ_..." in Forward's) so Forward generates dynamic connections from its rows, or
// clears it (queryID ""). It changes nothing else on the node, and returns the node as Forward now holds it, including QueryResult (check its
// Error: a query of the wrong row type is accepted and reported there, not refused). Preview: PATCH {"queryId": ...} on the node route; the property
// is hidden in Forward's spec (FWD-37393). Clearing sends an explicit null, which the server reads as "remove" (JsonProp).
func (s *SyntheticNodes) SetQuery(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, name, queryID string) (*ports.SyntheticNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	req = markOperation(req, kind.OpName("SetQuery"))
	out := new(ports.SyntheticNode)
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
func (s *SyntheticNodes) ComputeQuery(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, queryID string) (*ports.SyntheticQueryResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if queryID = strings.TrimSpace(queryID); queryID == "" {
		return nil, nil, errors.New("forward: NQE query ID is required")
	}
	path, err := s.collectionPath(networkID, kind)
	if err != nil {
		return nil, nil, err
	}
	if kind == ports.SyntheticInternet {
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
	req = markOperation(req, kind.OpName("ComputeQuery"))
	out := new(ports.SyntheticQueryResult)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CompatibleQueries lists the saved NQE queries a node of this kind can use: parameterless queries whose row type is the kind's connection
// record, typed against the network's latest processed snapshot (empty when the network has none). Preview: GET
// /api/synthetic-device-queries?type=&networkId= (SyntheticDeviceController; served on primary 15398425a69 and stable 67e89c87124).
func (s *SyntheticNodes) CompatibleQueries(ctx context.Context, networkID string, kind ports.SyntheticNodeKind) ([]ports.SyntheticDeviceQuery, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	deviceType, ok := map[ports.SyntheticNodeKind]string{
		ports.SyntheticInternet: "INTERNET", ports.SyntheticIntranet: "INTRANET", ports.SyntheticL3VPN: "L3VPN",
		ports.SyntheticL2VPN: "L2VPN", ports.SyntheticAdjacentNetwork: "ADJACENT_NETWORK",
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
	result := listResponse[ports.SyntheticDeviceQuery]{Keys: []string{"queries"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Backdate applies the network's CURRENT configuration of this kind of synthetic device from snapshotID onward and INVALIDATES every
// snapshot from that one on, so they reprocess and their answers are unavailable until they finish -- the Forward UI's "backdate". POST
// ?op=backdate&snapshotId= on the kind's collection route, or the internet node's own route (e.g. InternetNodeService.backdateTo:
// repo.backdateTo, then invalidateAffectedSnapshots from the snapshot's creation instant with no end). Needs MANAGE_COLLECTION_SOURCES and
// INVALIDATE_SNAPSHOTS. Marked SyntheticNodes.Backdate<Kind> so a hook can recognise it as destructive. Preview: not in the published spec.
func (s *SyntheticNodes) Backdate(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, snapshotID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := s.collectionPath(networkID, kind)
	if err != nil {
		return nil, err
	}
	if kind == ports.SyntheticInternet {
		if path, err = s.nodePath(networkID, kind, ""); err != nil {
			return nil, err
		}
	}
	return backdate(ctx, s.client, path, "op", snapshotID, kind.OpName("Backdate"))
}

// SetInternetExcludedSubnets REPLACES the internet node's excluded subnets -- public CIDRs that Forward will not treat as reachable through
// the internet node -- with subnets, and returns the node as Forward now holds it. An empty slice clears them; nil is refused, so a caller
// cannot clear the list by forgetting to fill it (Forward reads an absent or null value as "leave unchanged"). Forward validates each entry
// (a valid subnet with its host bits zero, public addresses only) and applies the change from the next processed snapshot. PATCH
// /api/networks/{id}/internet-node with only {"subnetsToExclude": [...]} (published, InternetNodePatch).
func (s *SyntheticNodes) SetInternetExcludedSubnets(ctx context.Context, networkID string, subnets []string) (*ports.SyntheticNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if subnets == nil {
		return nil, nil, errors.New("forward: excluded subnets are required; pass an empty slice to clear them")
	}
	path, err := s.nodePath(networkID, ports.SyntheticInternet, "")
	if err != nil {
		return nil, nil, err
	}
	cleaned := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if subnet = strings.TrimSpace(subnet); subnet != "" {
			cleaned = append(cleaned, subnet)
		}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, map[string][]string{"subnetsToExclude": cleaned})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SyntheticNodes.SetInternetExcludedSubnets")
	out := new(ports.SyntheticNode)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Delete removes one synthetic node. A 404 is success. The internet node
// cannot be deleted -- there is one per network and no route for it.
func (s *SyntheticNodes) Delete(ctx context.Context, networkID string, kind ports.SyntheticNodeKind, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if kind == ports.SyntheticInternet {
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
	req = markOperation(req, kind.OpName("Delete"))
	resp, err := s.client.Do(req, nil)
	if ports.IsGone(err) {
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
func (s *SyntheticNodes) List(ctx context.Context, networkID string, kind ports.SyntheticNodeKind) ([]ports.SyntheticNode, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.collectionPath(networkID, kind)
	if err != nil {
		return nil, nil, err
	}
	if path == "" {
		return nil, nil, nil
	}
	result := listResponse[ports.SyntheticNode]{Keys: []string{"l3Vpns", "intranetNodes", "l2Vpns", "adjacentNetworks", "items", "data", "results"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, kind.OpName("List"))
	resp, err := s.client.doRequired(req, &result)
	if ports.IsStatus(err, http.StatusNotFound) {
		// A network with no such collection is not an error; it has none.
		return nil, resp, nil
	}
	return result.Items, resp, err
}

// InternetConnectionSuggestions returns the connections Forward suggests for the internet node, computed from the latest processed snapshot
// (GET /api/networks/{id}/internet-node/connection-suggestions): the GUI's "suggested connections". It is read-only; add one with Put or Patch on the
// internet node. Preview: not in the published spec.
func (s *SyntheticNodes) InternetConnectionSuggestions(ctx context.Context, networkID string) ([]ports.InternetConnectionSuggestion, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.nodePath(networkID, ports.SyntheticInternet, "")
	if err != nil {
		return nil, nil, err
	}
	// Forward's InternetConnectionSuggestions record is @JsonInclude(NON_EMPTY): with nothing to suggest it sends {}.
	result := listResponse[ports.InternetConnectionSuggestion]{Keys: []string{"suggestions", "items"}, AllowEmptyObject: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/connection-suggestions", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SyntheticNodes.InternetConnectionSuggestions")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}
