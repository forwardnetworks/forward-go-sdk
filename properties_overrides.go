package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// NetworkProperty is a property a single NETWORK can override for itself (Forward's NetworkProperty: a subset of the org properties).
type NetworkProperty string

// The network properties Forward defines (NetworkProperty at master, a subset of the org properties), in the lower-case spelling a live
// 26.10.6 appserver answers and accepts. A build may define more or fewer: an unknown one is a 400 (ErrUnknownNetworkProperty).
const (
	NetworkPropertyDisableFlowComputation          NetworkProperty = "disable_flow_computation"
	NetworkPropertyAlertNewDeviceFailurePercentage NetworkProperty = "alert_new_device_failure_percentage"
	NetworkPropertyAdvancedReachabilityAnalysis    NetworkProperty = "advanced_reachability_analysis"
)

// Values of NetworkPropertyAdvancedReachabilityAnalysis (Forward's AdvancedReachabilityAnalysisMode): ON_DEMAND (the default) computes the
// advanced analysis only when a user triggers it, ASYNC computes it after each snapshot finishes processing. An unknown value is a 400 that
// changes nothing. The retired INLINE value is read as ASYNC by Forward.
const (
	AdvancedReachabilityAsync    = "ASYNC"
	AdvancedReachabilityOnDemand = "ON_DEMAND"
)

func propertyName(property string) (string, error) {
	if property = strings.TrimSpace(property); property == "" {
		return "", errors.New("forward: a property name is required")
	}
	return url.PathEscape(property), nil
}

// OrganizationValue reads one property's effective value for one org (GET /api/orgs/{orgId}/config/{property}; Forward support).
func (s *PropertiesService) OrganizationValue(ctx context.Context, orgID string, property OrgProperty) (PropertyValues, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	name, err := propertyName(string(property))
	if err != nil {
		return nil, nil, err
	}
	return s.get(ctx, "Properties.OrganizationValue", "/api/orgs/"+url.PathEscape(orgID)+"/config/"+name)
}

// OrganizationOverrides reads, for ONE property, the stored org-level rows, keyed by org id (GET /api/global-config/{property}?
// view=overrides; Forward support: a support user sees the orgs it supports, a Forward admin all). It is NOT effective values:
// ConfigController.getConfigPropertyOverrides returns configService.getOrgOverrides(property), every stored org-level row
// (ObjType.CONFIG_PROPERTY_ORG), uncached, including rows equal to the default and nothing from the global level. Forward does not
// remove an org's config rows when the org is deleted, so the map can hold ORPHAN rows of orgs that no longer exist (measured on
// primary 1.0.0-261118: 332 PROD rows, all for ids whose GET /api/orgs/{id}/config is 404); intersect with the live org list before
// trusting a count. An org absent from the map has no org-level row and takes the global value (the global override if one is set,
// else the built-in default); an org with a row reads that row whatever the global value is. For what an org actually gets, use
// EffectiveOrganizationValue or OrganizationValue. One call per property, not one per org.
func (s *PropertiesService) OrganizationOverrides(ctx context.Context, property OrgProperty) (map[string]json.RawMessage, *Response, error) {
	name, err := propertyName(string(property))
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/global-config/"+name+"?view=overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]json.RawMessage{}
	resp, err := s.client.Do(markOperation(req, "Properties.OrganizationOverrides"), &out)
	if err != nil {
		return nil, resp, err
	}
	// A build without the view=overrides mapping answers the plain property read instead, {"<property>": <default>}: an "org" named after
	// the property. route-check cannot tell the views apart, so catch it here.
	if _, plain := out[strings.ToLower(strings.TrimSpace(string(property)))]; plain && len(out) == 1 {
		return nil, resp, fmt.Errorf("%w: this build answered the plain property read, not its per-org overrides", ErrEndpointNotServed)
	}
	return out, resp, nil
}

// NetworkValue reads one network property's effective value for a network (GET /api/networks/{networkId}/config/{property};
// EDIT_NETWORK_SETTINGS, or Forward support). The answer maps the property name to its value.
func (s *PropertiesService) NetworkValue(ctx context.Context, networkID string, property NetworkProperty) (map[string]json.RawMessage, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	name, err := propertyName(string(property))
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/config/"+name, nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]json.RawMessage{}
	resp, err := s.client.Do(markOperation(req, "Properties.NetworkValue"), &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// NetworkOverrides reads, for one network property, the value each network of the CALLER'S org overrides it to, keyed by network id (GET
// /api/config/{property}/overrides; only networks the caller may edit settings of). It reads the caller's own org; there is no cross-org form.
func (s *PropertiesService) NetworkOverrides(ctx context.Context, property NetworkProperty) (map[string]json.RawMessage, *Response, error) {
	name, err := propertyName(string(property))
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/config/"+name+"/overrides", nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]json.RawMessage{}
	resp, err := s.client.Do(markOperation(req, "Properties.NetworkOverrides"), &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// PatchNetworkOverrides sets or clears one network property's per-network overrides in a single call (PATCH /api/config/{property}/overrides,
// 204), keyed by network id. A non-nil value sets that network's override; a nil value deletes it, so the network falls back to the org's
// value. Every id must be a network of the caller's own org (400 otherwise) and one the caller may edit settings of (403 otherwise).
// Forward checks the ids, the permissions and every value before it writes anything, so a refused call wrote nothing; the one step that is not
// atomic is that the sets are stored first and the deletes after, so a failure in between keeps the sets. A value of the wrong type may come
// back as a 500 rather than a 400 (the parse throws IllegalArgumentException). Preview: ConfigController.patchNetworkPropertyOverrides at
// master fe0daf63c899; the same mapping exists at both pins. An empty map sends nothing and returns nil.
func (s *PropertiesService) PatchNetworkOverrides(ctx context.Context, property NetworkProperty, updates map[string]any) (*Response, error) {
	name, err := propertyName(string(property))
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}
	for id := range updates {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("forward: a network id is required for every override")
		}
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/config/"+name+"/overrides", updates)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Properties.PatchNetworkOverrides"), nil)
}

// SetNetwork sets one network's override of a network property and returns the value as Forward parsed it (PUT
// /api/networks/{networkId}/config/{property}?value=; ConfigController.setNetworkConfigOverride, EDIT_NETWORK_SETTINGS, or Forward support).
// value is the text form (for example AdvancedReachabilityAsync). An org admin's key can set the properties Forward defines per network
// (NetworkProperty); one defined only for orgs, such as LOCATION_CONNECTIVITY_DIFFS, is a 400 (ErrUnknownNetworkProperty) and only an org
// property write by Forward support can set it. A value the property's type rejects is a 400 that changes nothing; to set one property on
// many networks in a single call use PatchNetworkOverrides. Seen live on 26.10.6 by the AutoCon6 workshop (advanced_reachability_analysis
// to ASYNC on 67 networks). Preview: ConfigController, not in the published spec.
func (s *PropertiesService) SetNetwork(ctx context.Context, networkID string, property NetworkProperty, value string) (map[string]json.RawMessage, *Response, error) {
	path, err := networkConfigPath(s, networkID, property)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPut, path+"?"+url.Values{"value": []string{value}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]json.RawMessage{}
	resp, err := s.client.Do(markOperation(req, "Properties.SetNetwork"), &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// ClearNetwork removes one network's override of a network property, so the network takes the org's value again (DELETE
// /api/networks/{networkId}/config/{property}, 204; ConfigController.deleteNetworkConfigOverride). Preview, like SetNetwork.
func (s *PropertiesService) ClearNetwork(ctx context.Context, networkID string, property NetworkProperty) (*Response, error) {
	path, err := networkConfigPath(s, networkID, property)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(markOperation(req, "Properties.ClearNetwork"), nil)
}

func networkConfigPath(s *PropertiesService, networkID string, property NetworkProperty) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	name, err := propertyName(string(property))
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/config/" + name, nil
}

var orgPropertyNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// CurrentValue reads the effective value of ONE organization property for the caller's organization (GET /api/config/{property};
// ConfigController.getConfigValue, @RequiresUser: any signed-in user). Use it instead of Current when only a few properties are needed:
// Current returns the whole map, including the organization's ALLOWED_IPS and its password policy, which a restricted session may not read.
// Forward answers {"<property>": value}, with the key in lower case on a 26.11 build seen live (predict_modeling), so the value is found
// ignoring case. The name must be an OrgProperty name (letters, digits, underscore); it is sent upper-cased, as the enum spells it. A name
// Forward does not define is its 400 "No enum constant ...OrgProperty.X", which is ErrUnknownOrgProperty (IsUnknownOrgProperty). Preview:
// from Forward's source, not a live call to this method.
func (s *PropertiesService) CurrentValue(ctx context.Context, property OrgProperty) (json.RawMessage, *Response, error) {
	name := strings.ToUpper(strings.TrimSpace(string(property)))
	if !orgPropertyNamePattern.MatchString(name) {
		return nil, nil, fmt.Errorf("forward: %q is not an organization property name (letters, digits and underscore)", property)
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/config/"+name, nil)
	if err != nil {
		return nil, nil, err
	}
	values := PropertyValues{}
	resp, err := s.client.Do(markOperation(req, "Properties.CurrentValue"), &values)
	if err != nil {
		return nil, resp, err
	}
	raw := propertyRaw(values, OrgProperty(name))
	if raw == nil {
		return nil, resp, fmt.Errorf("forward: the answer for %s does not carry the property", name)
	}
	return raw, resp, nil
}

// PropertySource says which level an effective property value came from.
type PropertySource string

const (
	// PropertySourceOrganization: the org has its own override row.
	PropertySourceOrganization PropertySource = "organization"
	// PropertySourceGlobal: no org row, and a global override is set.
	PropertySourceGlobal PropertySource = "global"
	// PropertySourceDefault: neither, so the property's built-in default applies.
	PropertySourceDefault PropertySource = "default"
)

// EffectiveOrganizationValue is one org's effective value of a property and the level it comes from.
type EffectiveOrganizationValue struct {
	// Value is what the org gets (GET /api/orgs/{orgId}/config/{property}), so it is right even if Source were not.
	Value  json.RawMessage
	Source PropertySource
}

// EffectiveOrganizationValue reads an org's effective value of one property and which level it comes from, so a caller cannot
// mistake the per-property overrides list for effective values. Value is the org's own read; Source comes from two more reads, the
// org's configured rows (GET /api/orgs/{orgId}/config?filter=CONFIGURED) and the global overrides (GET
// /api/global-config?filter=CONFIGURED), following Forward's rule that an org row beats a global override, which beats the built-in
// default (ReadOnlyConfigService.getValue). Needs Forward support permission, as OrganizationValue does. Preview: composed from
// existing reads, untested against a live Forward.
func (s *PropertiesService) EffectiveOrganizationValue(ctx context.Context, orgID string, property OrgProperty) (*EffectiveOrganizationValue, *Response, error) {
	value, resp, err := s.OrganizationValue(ctx, orgID, property)
	if err != nil {
		return nil, resp, err
	}
	out := &EffectiveOrganizationValue{Value: propertyRaw(value, property)}
	own, resp, err := s.Organization(ctx, orgID, PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	if propertyRaw(own, property) != nil {
		out.Source = PropertySourceOrganization
		return out, resp, nil
	}
	global, resp, err := s.Global(ctx, PropertyFilterConfigured)
	if err != nil {
		return nil, resp, err
	}
	out.Source = PropertySourceDefault
	if propertyRaw(global, property) != nil {
		out.Source = PropertySourceGlobal
	}
	return out, resp, nil
}
