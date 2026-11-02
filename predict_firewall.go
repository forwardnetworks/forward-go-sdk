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
	// ObjectID is Forward's newer stable id for the object. Forward is replacing name with it (FWD-59203); for PAN-OS the two are equal today.
	ObjectID string `json:"objectId,omitempty"`
}

// Predefined names a built-in object such as the application "postgres" or the user "any".
func Predefined(name string) SecurityObjectID {
	return SecurityObjectID{ScopeID: "PREDEFINED", Name: name}
}

// IPRange is an inclusive address range in a rule (Forward's IpRange); the ends are IP address strings.
type IPRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// AddressSpecification is one side of a rule: subnets and ranges stated directly and/or objects by reference. Forward's
// SecurityRuleSpecification model (FirewallPredictService, /security-rules).
//
// "Any" when staging is the EMPTY specification: Forward writes an empty side of a PAN-OS rule as "any" (PanosConfigNodePredictor), and
// also accepts the PREDEFINED "any" object and the zone "any". But Forward reads a rule's "any" BACK as the zone "any" and as
// PREDEFINED "any" objects, so compare a rule read back with a rule you staged by meaning, not field by field. Use AnyAddress.
type AddressSpecification struct {
	Subnets  []string           `json:"subnets,omitempty"`
	IPRanges []IPRange          `json:"ipRanges,omitempty"`
	Objects  []SecurityObjectID `json:"objects,omitempty"`
	Groups   []SecurityObjectID `json:"groups,omitempty"`
	// DynamicAddressObjects and Regions are objects by reference like Objects.
	DynamicAddressObjects []SecurityObjectID `json:"dynamicAddressObjects,omitempty"`
	Regions               []SecurityObjectID `json:"regions,omitempty"`
	Negated               bool               `json:"negated,omitempty"`
}

// ObjectSpecification lists objects by reference. It serves both users and applications, but Forward's two models differ and it very
// likely refuses (400) a field the other lacks: a USER specification takes Objects and IdentityReferences only; an APPLICATION
// specification takes Objects, Groups and DynamicGroups only. An empty specification stages as any (AnyUser, AnyApplication).
type ObjectSpecification struct {
	Objects []SecurityObjectID `json:"objects,omitempty"`
	Groups  []SecurityObjectID `json:"groups,omitempty"`
	// Groups and DynamicGroups are the application specification's only.
	DynamicGroups []SecurityObjectID `json:"dynamicGroups,omitempty"`
	// IdentityReferences is the user specification's identity references.
	IdentityReferences []string `json:"identityReferences,omitempty"`
}

// AnyAddress, AnyUser, AnyApplication and AnyService match everything when staged: every field empty (leave SourceZones and
// DestinationZones nil too), which Forward writes to the device as "any". See AddressSpecification for how "any" reads back.
func AnyAddress() AddressSpecification { return AddressSpecification{} }

// AnyUser matches every user; see AnyAddress.
func AnyUser() ObjectSpecification { return ObjectSpecification{} }

// AnyApplication matches every application; see AnyAddress.
func AnyApplication() ObjectSpecification { return ObjectSpecification{} }

// AnyService matches every service; see AnyAddress.
func AnyService() ServiceSpecification { return ServiceSpecification{} }

// ServiceSpecification is the rule's services: the applications' defaults, or objects by reference.
type ServiceSpecification struct {
	UseApplicationDefaults bool               `json:"useApplicationDefaults,omitempty"`
	Objects                []SecurityObjectID `json:"objects,omitempty"`
	Groups                 []SecurityObjectID `json:"groups,omitempty"`
}

// Rule actions Forward models (DeviceSecurityRuleAction). DENY blocks and sends the protocol's default rejection, DROP blocks silently,
// the RESET_ actions block and send a TCP reset to the client, the server or both.
const (
	SecurityActionPermit               = "PERMIT"
	SecurityActionDeny                 = "DENY"
	SecurityActionDrop                 = "DROP"
	SecurityActionResetClient          = "RESET_CLIENT"
	SecurityActionResetClientAndServer = "RESET_CLIENT_AND_SERVER"
	SecurityActionResetServer          = "RESET_SERVER"
)

// SecurityRuleDefinition is a rule as Forward models it. Action is one of the SecurityAction* names. Forward does not model logging, tags or any
// other rule line outside this struct: an EDITED rule keeps those lines from the baseline (matched by the rule's UUID, which Forward keeps
// across an edit) and a NEW rule gets none; a rule without a UUID is ignored when the config is predicted. Forward only lets a PAN-OS
// change set edit the LOCAL scope's PRIMARY rulebase (FirewallScopeLocal, FirewallRulebasePrimary; SecurityRulebase.Editable): add,
// remove, edit and discard on any other rulebase answer 400.
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
	// Schedule names a schedule object the rule is active on; nil is always active. Enabled false means never active, and then Forward
	// ignores the schedule and reads it back as nil, so re-enabling a disabled scheduled rule loses its schedule unless you set it again.
	Schedule *SecurityObjectID `json:"schedule,omitempty"`
	// URLObjects are URL-category objects by reference.
	URLObjects []SecurityObjectID `json:"urlObjects,omitempty"`
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
	// Lifecycle data (Forward's LifecycleData, flattened into the rule when the device recorded it): when the rule was created, last
	// modified and last hit, and a truncated hit count.
	CreatedAt    string `json:"createdAt,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	LastUsed     string `json:"lastUsed,omitempty"`
	HitCount     *int   `json:"hitCount,omitempty"`
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

// RemoveSecurityRule stages the removal of the rule with the given id in the change set's draft (DELETE .../security-rules/{ruleId}). The
// baseline snapshot is untouched. For a rule from the baseline, DiscardSecurityRuleChange brings it back (as the baseline had it, so an
// earlier edit is lost). For a rule added in this change set, removing it just drops the addition and there is nothing to restore.
// Removing the same rule twice does nothing the second time.
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
	return s.firewallDo(markOperation(req, "Predict.RemoveSecurityRule"), nil)
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
