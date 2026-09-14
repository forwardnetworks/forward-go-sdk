package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// PathSearchRequest asks what a packet does between two points.
//
// SnapshotID is what makes the question answerable about a network that does
// not exist yet: point it at a predicted snapshot and the answer is what a
// change would do, before the change is made.
type PathSearchRequest struct {
	From                    string
	SrcIP                   string
	DstIP                   string
	Intent                  string
	SnapshotID              string
	IPProto                 *int
	SrcPort                 string
	DstPort                 string
	IcmpType                *int
	TCPFlags                PathTCPFlags
	AppID                   string
	UserID                  string
	UserGroupID             string
	URL                     string
	IncludeTags             *bool
	IncludeNetworkFunctions *bool
	MaxCandidates           *int
	MaxResults              *int
	MaxReturnPathResults    *int
	MaxSeconds              *int
}

// PathTCPFlags represents optional TCP flag filters.
type PathTCPFlags struct {
	FIN *int
	SYN *int
	RST *int
	PSH *int
	ACK *int
	URG *int
}

// PathSearchResponse captures path analysis output.
type PathSearchResponse struct {
	SrcIPLocationType string                `json:"srcIpLocationType"`
	DstIPLocationType string                `json:"dstIpLocationType"`
	Info              PathSearchInfo        `json:"info"`
	ReturnPathInfo    PathSearchInfo        `json:"returnPathInfo"`
	TimedOut          bool                  `json:"timedOut"`
	QueryURL          string                `json:"queryUrl"`
	Unrecognized      PathUnrecognizedValue `json:"unrecognizedValues"`
}

// PathSearchInfo is a set of paths with aggregation info.
type PathSearchInfo struct {
	Paths     []NetworkPathResult `json:"paths"`
	TotalHits struct {
		Type  string `json:"type"`
		Value int64  `json:"value"`
	} `json:"totalHits"`
}

// PathUnrecognizedValue enumerates value mismatches returned by API.
type PathUnrecognizedValue struct {
	AppID       []string `json:"appId"`
	UserID      []string `json:"userId"`
	UserGroupID []string `json:"userGroupId"`
}

// NetworkPathResult is a single path through the network.
type NetworkPathResult struct {
	ForwardingOutcome string    `json:"forwardingOutcome"`
	SecurityOutcome   string    `json:"securityOutcome"`
	Hops              []PathHop `json:"hops"`
}

// PathHop represents an individual hop in the path search.
type PathHop struct {
	DeviceName       string               `json:"deviceName"`
	DisplayName      string               `json:"displayName"`
	DeviceType       string               `json:"deviceType"`
	Tags             []string             `json:"tags"`
	ParseError       *bool                `json:"parseError"`
	IngressInterface string               `json:"ingressInterface"`
	EgressInterface  string               `json:"egressInterface"`
	Behaviors        []string             `json:"behaviors"`
	NetworkFunctions *PathNetworkFunction `json:"networkFunctions"`
	BackfilledFrom   string               `json:"backfilledFrom"`
}

// PathNetworkFunction captures ACL and zone context for a hop.
type PathNetworkFunction struct {
	ACL     []PathACL           `json:"acl"`
	Ingress PathInterfaceDetail `json:"ingress"`
	Egress  PathInterfaceDetail `json:"egress"`
}

// PathACL describes ACL evaluation on a hop.
type PathACL struct {
	Name    string `json:"name"`
	Context string `json:"context"`
	Action  string `json:"action"`
}

// PathInterfaceDetail captures interface/zone information for ingress/egress.
type PathInterfaceDetail struct {
	L2           PathInterface `json:"l2"`
	L3           PathInterface `json:"l3"`
	SecurityZone string        `json:"securityZone"`
}

// PathInterface describes a layer interface context.
type PathInterface struct {
	InterfaceName string `json:"interfaceName"`
	VRF           string `json:"vrf"`
}

// Paths executes a path analysis query.
//
// Either From or SrcIP is required, and DstIP always is: a search with no
// destination has no question in it.
func (s *NetworksService) Paths(ctx context.Context, networkID string, input PathSearchRequest) (*PathSearchResponse, *Response, error) {
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
