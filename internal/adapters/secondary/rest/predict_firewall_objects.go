package rest

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// Typed reads of a firewall's object collections in a Predict change set's baseline snapshot, one method per collection. They send the
// routes FirewallObjects sends (GET .../change-sets/{id}/devices/{device}/{kind}; VIEW_CHANGE_SET_SECURITY_DATA and the organization
// property FIREWALL_PREDICT) and decode Forward's record collections (app/.../predict/firewall/*Collection.java and Scoped*.java at
// origin/master): each answer is an object with one list, and a collection that is empty is {} (several are @JsonInclude NON_EMPTY), which
// reads as an empty list. Shapes are from that source and are not recorded live; Forward's own comment says the rows mirror its NQE
// schema types. FirewallObjects stays for a caller that wants the raw body.

func firewallList[T any](ctx context.Context, s *Predict, networkID, changeSetID, deviceName string, kind ports.FirewallObjectKind, key string) ([]T, *ports.Response, error) {
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
func (s *Predict) ListAddressObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallAddressObject, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallAddressObject](ctx, s, networkID, changeSetID, deviceName, ports.FirewallAddressObjects, "addressObjects")
}

// ListAddressGroups returns the device's address groups (key "addressGroups").
func (s *Predict) ListAddressGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallAddressGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallAddressGroup](ctx, s, networkID, changeSetID, deviceName, ports.FirewallAddressGroups, "addressGroups")
}

// ListApplicationObjects returns the device's application objects (key "applicationObjects").
func (s *Predict) ListApplicationObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallApplicationObject, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallApplicationObject](ctx, s, networkID, changeSetID, deviceName, ports.FirewallApplicationObjects, "applicationObjects")
}

// ListApplicationGroups returns the device's application groups (key "applicationGroups").
func (s *Predict) ListApplicationGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallApplicationGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallApplicationGroup](ctx, s, networkID, changeSetID, deviceName, ports.FirewallApplicationGroups, "applicationGroups")
}

// ListURLObjects returns the device's URL objects (key "urlObjects").
func (s *Predict) ListURLObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallURLObject, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallURLObject](ctx, s, networkID, changeSetID, deviceName, ports.FirewallURLObjects, "urlObjects")
}

// ListServiceObjects returns the device's service objects across all scopes (key "serviceObjects").
func (s *Predict) ListServiceObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallServiceObject, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallServiceObject](ctx, s, networkID, changeSetID, deviceName, ports.FirewallServiceObjects, "serviceObjects")
}

// ListServiceGroups returns the device's service groups (key "serviceGroups").
func (s *Predict) ListServiceGroups(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallServiceGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallServiceGroup](ctx, s, networkID, changeSetID, deviceName, ports.FirewallServiceGroups, "serviceGroups")
}

// ListUserObjects returns the device's user objects (key "userObjects").
func (s *Predict) ListUserObjects(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallUserObject, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallUserObject](ctx, s, networkID, changeSetID, deviceName, ports.FirewallUserObjects, "userObjects")
}

// ListFirewallZones returns the device's security zones (key "zones"); see also Networks.SecurityZones for a network's zones.
func (s *Predict) ListFirewallZones(ctx context.Context, networkID, changeSetID, deviceName string) ([]ports.FirewallZone, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return firewallList[ports.FirewallZone](ctx, s, networkID, changeSetID, deviceName, ports.FirewallZones, "zones")
}
