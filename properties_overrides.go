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

// NetworkProperty is a property a single NETWORK can override for itself (Forward's NetworkProperty: a subset of the org properties).
type NetworkProperty string

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

// OrganizationOverrides reads, for ONE property, the value every org overrides it to, keyed by org id (GET /api/global-config/{property}?
// view=overrides; Forward support: a support user sees the orgs it supports, a Forward admin all). An org absent from the map has no
// override and takes the default. This is the efficient read across all orgs: one call per property, not one per org.
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
