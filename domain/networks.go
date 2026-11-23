package domain

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
	Domain                  string
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

// L7Application is an application name path search can filter on.
type L7Application struct {
	ID string `json:"id"`
}
