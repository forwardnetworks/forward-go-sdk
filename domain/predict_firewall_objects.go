package domain

// FirewallPortRange is an inclusive port range (Forward's IntRange).
type FirewallPortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// FirewallServiceDefinition is one protocol and its port ranges (ServiceDefinition).
type FirewallServiceDefinition struct {
	ProtocolName     string              `json:"protocolName"`
	SourcePorts      []FirewallPortRange `json:"sourcePorts,omitempty"`
	DestinationPorts []FirewallPortRange `json:"destinationPorts,omitempty"`
}

// FirewallAddressObject is an address object with the scope it belongs to (ScopedAddressObject).
type FirewallAddressObject struct {
	ScopeID     string    `json:"scopeId"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Subnets     []string  `json:"subnets,omitempty"`
	IPRanges    []IPRange `json:"ipRanges,omitempty"`
	Domains     []string  `json:"domains,omitempty"`
}

// FirewallAddressGroup is an address group (ScopedAddressGroup); its members are objects and groups by reference.
type FirewallAddressGroup struct {
	ScopeID       string             `json:"scopeId"`
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	Tags          []string           `json:"tags,omitempty"`
	MemberObjects []SecurityObjectID `json:"memberObjects,omitempty"`
	MemberGroups  []SecurityObjectID `json:"memberGroups,omitempty"`
}

// FirewallApplicationObject is an application object with the services it uses by default (ScopedApplicationObject).
type FirewallApplicationObject struct {
	ScopeID                   string                      `json:"scopeId"`
	Name                      string                      `json:"name"`
	Description               string                      `json:"description,omitempty"`
	DefaultServiceDefinitions []FirewallServiceDefinition `json:"defaultServiceDefinitions,omitempty"`
}

// FirewallApplicationGroup is an application group (ScopedApplicationGroup).
type FirewallApplicationGroup struct {
	ScopeID             string             `json:"scopeId"`
	Name                string             `json:"name"`
	MemberObjects       []SecurityObjectID `json:"memberObjects,omitempty"`
	MemberDynamicGroups []SecurityObjectID `json:"memberDynamicGroups,omitempty"`
	MemberGroups        []SecurityObjectID `json:"memberGroups,omitempty"`
}

// FirewallURLObject is a URL object, a list of URL patterns (ScopedUrlObject).
type FirewallURLObject struct {
	ScopeID     string   `json:"scopeId"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
}

// FirewallServiceObject is a service object (ScopedServiceObject).
type FirewallServiceObject struct {
	ScopeID     string                      `json:"scopeId"`
	Name        string                      `json:"name"`
	Description string                      `json:"description,omitempty"`
	Definitions []FirewallServiceDefinition `json:"definitions,omitempty"`
}

// FirewallServiceGroup is a service group (ScopedServiceGroup).
type FirewallServiceGroup struct {
	ScopeID       string             `json:"scopeId"`
	Name          string             `json:"name"`
	MemberObjects []SecurityObjectID `json:"memberObjects,omitempty"`
	MemberGroups  []SecurityObjectID `json:"memberGroups,omitempty"`
}

// FirewallUserObject is a user object (ScopedUserObject).
type FirewallUserObject struct {
	ScopeID string `json:"scopeId"`
	Name    string `json:"name"`
}

// FirewallZone is a security zone and the interfaces in it (SecurityZone).
type FirewallZone struct {
	Name       string   `json:"name"`
	Interfaces []string `json:"interfaces,omitempty"`
}
