package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Firewall predict: security rules staged on a change set for a classic firewall device (a
// PAN-OS VM-Series, say), in Forward's own rule model rather than vendor CLI. Objects are
// named by scope and name as the device's own object tables list them; "PREDEFINED" holds
// the vendor's built-in applications, services and the "any" user.

// SecurityObjectID names an object in one of the device's scopes.
type SecurityObjectID struct {
	ScopeID string `json:"scopeId"`
	Name    string `json:"name"`
}

// Predefined names a built-in object such as the application "postgres" or the user "any".
func Predefined(name string) SecurityObjectID {
	return SecurityObjectID{ScopeID: "PREDEFINED", Name: name}
}

// AddressSpecification is one side of a rule: subnets stated directly and/or objects by reference.
type AddressSpecification struct {
	Subnets []string           `json:"subnets,omitempty"`
	Objects []SecurityObjectID `json:"objects,omitempty"`
	Groups  []SecurityObjectID `json:"groups,omitempty"`
	Negated bool               `json:"negated,omitempty"`
}

// ObjectSpecification lists objects by reference (users, applications).
type ObjectSpecification struct {
	Objects []SecurityObjectID `json:"objects,omitempty"`
	Groups  []SecurityObjectID `json:"groups,omitempty"`
}

// ServiceSpecification is the rule's services: the applications' defaults, or objects by reference.
type ServiceSpecification struct {
	UseApplicationDefaults bool               `json:"useApplicationDefaults,omitempty"`
	Objects                []SecurityObjectID `json:"objects,omitempty"`
	Groups                 []SecurityObjectID `json:"groups,omitempty"`
}

// SecurityRuleDefinition is a rule as Forward models it. Action is PERMIT or DENY.
type SecurityRuleDefinition struct {
	Name                 string               `json:"name"`
	ID                   string               `json:"id,omitempty"`
	Description          string               `json:"description"`
	Enabled              bool                 `json:"enabled"`
	Action               string               `json:"action"`
	SourceZones          []string             `json:"sourceZones,omitempty"`
	DestinationZones     []string             `json:"destinationZones,omitempty"`
	SourceAddresses      AddressSpecification `json:"sourceAddresses"`
	DestinationAddresses AddressSpecification `json:"destinationAddresses"`
	SourceUsers          ObjectSpecification  `json:"sourceUsers"`
	Applications         ObjectSpecification  `json:"applications"`
	Services             ServiceSpecification `json:"services"`
}

// NewSecurityRule stages a rule. Predecessor is the id of the rule it goes after; empty puts it at the top.
type NewSecurityRule struct {
	Predecessor string                 `json:"predecessor,omitempty"`
	Definition  SecurityRuleDefinition `json:"definition"`
}

// SecurityRulebase is one rulebase of a device, as the rules diff lists them.
type SecurityRulebase struct {
	ScopeID     string `json:"scopeId"`
	RulebaseID  string `json:"rulebaseId"`
	Editable    bool   `json:"editable"`
	DisplayName string `json:"displayName"`
}

// ScopedSecurityRule is a rule with its place in a rulebase.
type ScopedSecurityRule struct {
	ScopeID    string                 `json:"scopeId"`
	RulebaseID string                 `json:"rulebaseId"`
	Rank       int                    `json:"rank"`
	Definition SecurityRuleDefinition `json:"definition"`
}

// SecurityRuleDiffEntry is one row of the device's rulebase diff: a = collected, b = staged.
type SecurityRuleDiffEntry struct {
	DiffType string              `json:"diffType"`
	A        *ScopedSecurityRule `json:"a,omitempty"`
	B        *ScopedSecurityRule `json:"b,omitempty"`
}

// SecurityRuleTableDiff is a device's rulebases and every rule, marked with how the change set alters it.
type SecurityRuleTableDiff struct {
	Rulebases []SecurityRulebase      `json:"rulebases"`
	Entries   []SecurityRuleDiffEntry `json:"entries"`
}

// The one rulebase Forward lets a change set edit on a PAN-OS device: its local rules.
const (
	FirewallScopeLocal      = "LOCAL"
	FirewallRulebasePrimary = "PRIMARY"
)

// AddSecurityRule stages a new rule in the device's rulebase.
func (s *PredictService) AddSecurityRule(
	ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID string, rule NewSecurityRule,
) (*Response, error) {
	if strings.TrimSpace(rule.Definition.Name) == "" {
		return nil, errors.New("forward: security rule name is required")
	}
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/security-rules", rule)
	if err != nil {
		return nil, err
	}
	return s.firewallDo(req, nil)
}

// RemoveSecurityRule stages the removal of the rule with the given id.
func (s *PredictService) RemoveSecurityRule(
	ctx context.Context, networkID, changeSetID, deviceName, scopeID, rulebaseID, ruleID string,
) (*Response, error) {
	path, err := s.rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID)
	if err != nil {
		return nil, err
	}
	ruleID = strings.TrimSpace(ruleID)
	if ruleID == "" {
		return nil, errors.New("forward: security rule id is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path+"/security-rules/"+url.PathEscape(ruleID), nil)
	if err != nil {
		return nil, err
	}
	return s.firewallDo(req, nil)
}

// SecurityRulesDiff reads the device's rulebases with every rule marked by how the change set alters it.
func (s *PredictService) SecurityRulesDiff(
	ctx context.Context, networkID, changeSetID, deviceName string,
) (*SecurityRuleTableDiff, *Response, error) {
	path, err := s.changeSetDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/security-rules-diff", nil)
	if err != nil {
		return nil, nil, err
	}
	diff := new(SecurityRuleTableDiff)
	resp, err := s.firewallDo(req, diff)
	if err != nil {
		return nil, resp, err
	}
	return diff, resp, nil
}

func (s *PredictService) firewallDo(req *http.Request, out any) (*Response, error) {
	if err := s.client.requireCapability(CapabilityPredict); err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req, out)
	if err != nil {
		return resp, err
	}
	s.client.observeCapability(CapabilityPredict)
	return resp, nil
}

// changeSetDevicePath is the firewall-predict device path: unlike the draft commands path, these
// endpoints hang directly off the change set.
func (s *PredictService) changeSetDevicePath(networkID, changeSetID, deviceName string) (string, error) {
	path, err := changeSetPath(networkID, changeSetID)
	if err != nil {
		return "", err
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return path + "/devices/" + url.PathEscape(deviceName), nil
}

func (s *PredictService) rulebasePath(networkID, changeSetID, deviceName, scopeID, rulebaseID string) (string, error) {
	path, err := s.changeSetDevicePath(networkID, changeSetID, deviceName)
	if err != nil {
		return "", err
	}
	if scopeID == "" || rulebaseID == "" {
		return "", errors.New("forward: rulebase scope and id are required")
	}
	return fmt.Sprintf("%s/scopes/%s/rulebases/%s", path, url.PathEscape(scopeID), url.PathEscape(rulebaseID)), nil
}
