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

// NetworksService manages Forward networks.
type NetworksService service

// Network represents a modeled network in Forward.
type Network struct {
	ID              Identifier `json:"id"`
	Name            string     `json:"name"`
	OrgID           Identifier `json:"orgId"`
	ParentID        Identifier `json:"parentId,omitempty"`
	Creator         string     `json:"creator,omitempty"`
	CreatorID       Identifier `json:"creatorId,omitempty"`
	CreatedAt       string     `json:"createdAt,omitempty"`
	Note            string     `json:"note,omitempty"`
	RetentionDays   *int32     `json:"retentionDays,omitempty"`
	SecondsToExpiry *int64     `json:"secondsToExpiry,omitempty"`
}

// NetworkUpdate contains the mutable fields of a Network. Pointer fields
// distinguish an omitted field from an explicit zero value.
type NetworkUpdate struct {
	Name          *string `json:"name,omitempty"`
	Note          *string `json:"note,omitempty"`
	RetentionDays *int32  `json:"retentionDays,omitempty"`
}

// WorkspaceNetworkRequest creates a network scoped to selected parent-network
// sources. Leave RetentionDays nil to use the appliance default.
type WorkspaceNetworkRequest struct {
	Name          string   `json:"name"`
	Note          string   `json:"note,omitempty"`
	Devices       []string `json:"devices,omitempty"`
	CloudAccounts []string `json:"cloudAccounts,omitempty"`
	VCenters      []string `json:"vcenters,omitempty"`
	Omissions     []string `json:"omissions,omitempty"`
	RetentionDays *int32   `json:"retentionDays,omitempty"`
}

// List returns all networks visible to the authenticated principal.
func (s *NetworksService) List(ctx context.Context) ([]Network, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks", nil)
	if err != nil {
		return nil, nil, err
	}

	var networks []Network
	resp, err := s.client.Do(req, &networks)
	if err != nil {
		return nil, resp, err
	}
	return networks, resp, nil
}

// CheckAccess preserves status-only consumers that deliberately do not require
// a JSON list body.
func (s *NetworksService) CheckAccess(ctx context.Context) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Networks.CheckAccess")
	return s.client.Do(req, nil)
}

type PathSearchRequest struct {
	From    string
	SrcIP   string
	DstIP   string
	IPProto string
	SrcPort string
	DstPort string
	Intent  string
}
type PathHop struct {
	DeviceName       string `json:"deviceName"`
	IngressInterface string `json:"ingressInterface"`
	EgressInterface  string `json:"egressInterface"`
}
type NetworkPathResult struct {
	ForwardingOutcome string    `json:"forwardingOutcome"`
	SecurityOutcome   string    `json:"securityOutcome"`
	Hops              []PathHop `json:"hops"`
}
type PathSearchInfo struct {
	Paths     []NetworkPathResult `json:"paths"`
	TotalHits json.RawMessage     `json:"totalHits"`
}
type PathSearchResponse struct {
	QueryURL string         `json:"queryUrl"`
	Info     PathSearchInfo `json:"info"`
}

func (s *NetworksService) Paths(ctx context.Context, networkID string, input PathSearchRequest) (*PathSearchResponse, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(input.DstIP) == "" {
		return nil, nil, errors.New("forward: destination IP is required")
	}
	q := url.Values{"dstIp": []string{strings.TrimSpace(input.DstIP)}}
	for key, value := range map[string]string{"from": input.From, "srcIp": input.SrcIP, "ipProto": input.IPProto, "srcPort": input.SrcPort, "dstPort": input.DstPort, "intent": strings.ToUpper(input.Intent)} {
		if value = strings.TrimSpace(value); value != "" {
			q.Set(key, value)
		}
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/paths?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Networks.Paths")
	out := new(PathSearchResponse)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// Create creates a network with name.
func (s *NetworksService) Create(ctx context.Context, name string) (*Network, *Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, errors.New("forward: network name is required")
	}

	query := url.Values{"name": []string{name}}
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/networks?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}

	network := new(Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

// CreateWorkspace creates a workspace network beneath parentNetworkID.
func (s *NetworksService) CreateWorkspace(
	ctx context.Context,
	parentNetworkID string,
	request WorkspaceNetworkRequest,
) (*Network, *Response, error) {
	if err := s.client.requireCapability(CapabilityWorkspaceNetworks); err != nil {
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
	network := new(Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	s.client.observeCapability(CapabilityWorkspaceNetworks)
	return network, resp, nil
}

// Update changes fields on an existing network.
func (s *NetworksService) Update(
	ctx context.Context,
	networkID string,
	update NetworkUpdate,
) (*Network, *Response, error) {
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

	network := new(Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

// Delete deletes a network and returns its final representation.
func (s *NetworksService) Delete(ctx context.Context, networkID string) (*Network, *Response, error) {
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

	network := new(Network)
	resp, err := s.client.Do(req, network)
	if err != nil {
		return nil, resp, err
	}
	return network, resp, nil
}

func networkPath(networkID string) (string, error) {
	networkID = strings.TrimSpace(networkID)
	if networkID == "" {
		return "", errors.New("forward: network ID is required")
	}
	return fmt.Sprintf("/api/networks/%s", url.PathEscape(networkID)), nil
}
