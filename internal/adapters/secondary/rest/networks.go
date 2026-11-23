package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NetworksService manages Forward networks.
type Networks service

// List returns all networks visible to the authenticated principal.
func (s *Networks) List(ctx context.Context) ([]ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks", nil)
	if err != nil {
		return nil, nil, err
	}

	var networks []ports.Network
	resp, err := s.client.Do(req, &networks)
	if err != nil {
		return nil, resp, err
	}
	return networks, resp, nil
}

// CheckAccess preserves status-only consumers that deliberately do not require
// a JSON list body.
func (s *Networks) CheckAccess(ctx context.Context) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Networks.CheckAccess")
	return s.client.Do(req, nil)
}

// Paths executes a path analysis query.
//
// Either From or SrcIP is required, and DstIP always is: a search with no
// destination has no question in it.
func (s *Networks) Paths(ctx context.Context, networkID string, input ports.PathSearchRequest) (*ports.PathSearchResponse, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(input.DstIP) == "" {
		return nil, nil, errors.New("forward: destination IP is required")
	}
	if strings.TrimSpace(input.From) == "" && strings.TrimSpace(input.SrcIP) == "" {
		return nil, nil, errors.New("forward: either from or srcIp is required")
	}
	params := input

	query := url.Values{}
	if params.From != "" {
		query.Set("from", params.From)
	}
	if params.SrcIP != "" {
		query.Set("srcIp", params.SrcIP)
	}
	query.Set("dstIp", params.DstIP)

	if params.Intent != "" {
		query.Set("intent", params.Intent)
	}
	if params.SnapshotID != "" {
		query.Set("snapshotId", params.SnapshotID)
	}

	addInt := func(key string, value *int) {
		if value != nil {
			query.Set(key, strconv.Itoa(*value))
		}
	}

	if params.IPProto != nil {
		query.Set("ipProto", strconv.Itoa(*params.IPProto))
	}
	if params.SrcPort != "" {
		query.Set("srcPort", params.SrcPort)
	}
	if params.DstPort != "" {
		query.Set("dstPort", params.DstPort)
	}
	addInt("icmpType", params.IcmpType)
	addInt("fin", params.TCPFlags.FIN)
	addInt("syn", params.TCPFlags.SYN)
	addInt("rst", params.TCPFlags.RST)
	addInt("psh", params.TCPFlags.PSH)
	addInt("ack", params.TCPFlags.ACK)
	addInt("urg", params.TCPFlags.URG)

	if params.AppID != "" {
		query.Set("appId", params.AppID)
	}
	if params.UserID != "" {
		query.Set("userId", params.UserID)
	}
	if params.Domain != "" {
		query.Set("domain", params.Domain)
	}
	if params.UserGroupID != "" {
		query.Set("userGroupId", params.UserGroupID)
	}
	if params.URL != "" {
		query.Set("url", params.URL)
	}

	if params.IncludeTags != nil {
		query.Set("includeTags", strconv.FormatBool(*params.IncludeTags))
	}
	if params.IncludeNetworkFunctions != nil {
		query.Set("includeNetworkFunctions", strconv.FormatBool(*params.IncludeNetworkFunctions))
	}
	addInt("maxCandidates", params.MaxCandidates)
	addInt("maxResults", params.MaxResults)
	addInt("maxReturnPathResults", params.MaxReturnPathResults)
	addInt("maxSeconds", params.MaxSeconds)

	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/paths?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.Paths")
	out := new(ports.PathSearchResponse)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// getByID is Get's direct read, GET /api/networks/{id}, of a network ID already resolved. It is kept apart from the
// List fallback so that, after the split (the v0.9 hexagonal split), the HTTP adapter sends this request and
// the use case owns the fallback, and nothing inside the adapter calls its own List.
func (s *Networks) GetByID(ctx context.Context, networkID string) (*ports.Network, *ports.Response, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	network := new(ports.Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

// Create creates a network with name.
func (s *Networks) Create(ctx context.Context, name string) (*ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, errors.New("forward: network name is required")
	}

	query := url.Values{"name": []string{name}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/networks?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}

	network := new(ports.Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

// CreateWorkspace creates a workspace network beneath parentNetworkID.
func (s *Networks) CreateWorkspace(
	ctx context.Context,
	parentNetworkID string,
	request ports.WorkspaceNetworkRequest,
) (*ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.client.requireCapability(ports.CapabilityWorkspaceNetworks); err != nil {
		return nil, nil, err
	}
	parentNetworkID, err := s.client.resolveNetworkID(parentNetworkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(parentNetworkID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(request.Name) == "" {
		return nil, nil, errors.New("forward: workspace network name is required")
	}
	path += "/workspaces"
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}
	network := new(ports.Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(ports.CapabilityWorkspaceNetworks)
	return network, resp, nil
}

// Update changes fields on an existing network.
func (s *Networks) Update(
	ctx context.Context,
	networkID string,
	update ports.NetworkUpdate,
) (*ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, nil, err
	}

	network := new(ports.Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

// Delete deletes a network and returns its final representation.
func (s *Networks) Delete(ctx context.Context, networkID string) (*ports.Network, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	network := new(ports.Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

func networkPath(networkID string) (string, error) {
	networkID, err := ports.RequireNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/api/networks/%s", url.PathEscape(networkID)), nil
}

// networkSubPath is /api/networks/{id}/{rest...} for a network ID that may be
// empty (a ForNetwork client supplies its bound network). Callers escape each
// element of rest themselves, because some end in a query such as
// "assets?action=modifyCriticality".
func (c *Client) networkSubPath(networkID string, rest ...string) (string, error) {
	networkID, err := c.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/" + strings.Join(rest, "/"), nil
}

// bodyRequest builds a request that carries body as JSON, or no body when body
// is nil.
func (c *Client) bodyRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if body == nil {
		return c.NewRequest(ctx, method, path, nil)
	}
	return c.newJSONRequest(ctx, method, path, body)
}

// L7Applications returns the application names path search recognizes. GET
// /api/l7-applications (getL7Applications, published; PathSearchController on
// primary 15398425a69 and stable 67e89c87124). The list is the appserver's,
// not a network's, so no network is named.
func (s *Networks) L7Applications(ctx context.Context) ([]ports.L7Application, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/l7-applications", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.L7Applications")
	var out []ports.L7Application
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// SecurityZones returns the security zones each device defines, as device name to zone names, for a snapshot (the
// latest processed one when snapshotID is empty, which sends no parameter so the appserver applies its own default).
// GET /api/networks/{networkId}/security-zones (SecurityAnalysisController.getSecurityZones; VIEW_SECURITY_ANALYSIS;
// needs the snapshot to have reached the CREATION stage; on primary 15398425a69 and stable 67e89c87124). Preview: not
// in the published spec. The zone-to-zone security matrix routes return compressed protobuf and are not modelled.
func (s *Networks) SecurityZones(ctx context.Context, networkID, snapshotID string) (map[string][]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path := "/api/networks/" + url.PathEscape(networkID) + "/security-zones"
	if snapshotID = strings.TrimSpace(snapshotID); snapshotID != "" {
		path += "?" + url.Values{"snapshotId": []string{snapshotID}}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.SecurityZones")
	zones := map[string][]string{}
	response, err := s.client.doRequired(req, &zones)
	if err != nil {
		return nil, response, err
	}
	return zones, response, nil
}
