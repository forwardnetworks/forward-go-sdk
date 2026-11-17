package forward

import (
	"context"
	"fmt"
)

// Typed reads of a firewall's object collections in a Predict change set's baseline snapshot, one method per collection. They send the
// routes FirewallObjects sends (GET .../change-sets/{id}/devices/{device}/{kind}; VIEW_CHANGE_SET_SECURITY_DATA and the organization
// property FIREWALL_PREDICT) and decode Forward's record collections (app/.../predict/firewall/*Collection.java and Scoped*.java at
// origin/master): each answer is an object with one list, and a collection that is empty is {} (several are @JsonInclude NON_EMPTY), which
// reads as an empty list. Shapes are from that source and are not recorded live; Forward's own comment says the rows mirror its NQE
// schema types. FirewallObjects stays for a caller that wants the raw body.

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

func firewallList[T any](ctx context.Context, s *PredictService, networkID, changeSetID, deviceName string, kind FirewallObjectKind, key string) ([]T, *Response, error) {
	raw, resp, err := s.FirewallObjects(ctx, networkID, changeSetID, deviceName, kind)
	if err != nil {
		return nil, resp, err
	}
	out := listResponse[T]{Keys: []string{key}, AllowEmptyObject: true}
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, resp, fmt.Errorf("forward: decode firewall %s: %w", kind, err)
	}
	if out.Items == nil {
		out.Items = []T{}
	}
	return out.Items, resp, nil
}

// ListAddressObjects returns the device's address objects (key "addressObjects").
func (s *PredictService) ListAddressObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallAddressObject, *Response, error) {
	return firewallList[FirewallAddressObject](ctx, s, networkID, changeSetID, deviceName, FirewallAddressObjects, "addressObjects")
}

// ListAddressGroups returns the device's address groups (key "addressGroups").
func (s *PredictService) ListAddressGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallAddressGroup, *Response, error) {
	return firewallList[FirewallAddressGroup](ctx, s, networkID, changeSetID, deviceName, FirewallAddressGroups, "addressGroups")
}

// ListApplicationObjects returns the device's application objects (key "applicationObjects").
func (s *PredictService) ListApplicationObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallApplicationObject, *Response, error) {
	return firewallList[FirewallApplicationObject](ctx, s, networkID, changeSetID, deviceName, FirewallApplicationObjects, "applicationObjects")
}

// ListApplicationGroups returns the device's application groups (key "applicationGroups").
func (s *PredictService) ListApplicationGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallApplicationGroup, *Response, error) {
	return firewallList[FirewallApplicationGroup](ctx, s, networkID, changeSetID, deviceName, FirewallApplicationGroups, "applicationGroups")
}

// ListURLObjects returns the device's URL objects (key "urlObjects").
func (s *PredictService) ListURLObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallURLObject, *Response, error) {
	return firewallList[FirewallURLObject](ctx, s, networkID, changeSetID, deviceName, FirewallURLObjects, "urlObjects")
}

// ListServiceObjects returns the device's service objects across all scopes (key "serviceObjects").
func (s *PredictService) ListServiceObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallServiceObject, *Response, error) {
	return firewallList[FirewallServiceObject](ctx, s, networkID, changeSetID, deviceName, FirewallServiceObjects, "serviceObjects")
}

// ListServiceGroups returns the device's service groups (key "serviceGroups").
func (s *PredictService) ListServiceGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallServiceGroup, *Response, error) {
	return firewallList[FirewallServiceGroup](ctx, s, networkID, changeSetID, deviceName, FirewallServiceGroups, "serviceGroups")
}

// ListUserObjects returns the device's user objects (key "userObjects").
func (s *PredictService) ListUserObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallUserObject, *Response, error) {
	return firewallList[FirewallUserObject](ctx, s, networkID, changeSetID, deviceName, FirewallUserObjects, "userObjects")
}

// ListFirewallZones returns the device's security zones (key "zones"); see also Networks.SecurityZones for a network's zones.
func (s *PredictService) ListFirewallZones(ctx context.Context, networkID, changeSetID, deviceName string) ([]FirewallZone, *Response, error) {
	return firewallList[FirewallZone](ctx, s, networkID, changeSetID, deviceName, FirewallZones, "zones")
}
