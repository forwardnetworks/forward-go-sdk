package rest

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// PropertiesService accesses Forward organization and global properties.
//
// Preview: this configuration surface is unpublished, permission-sensitive,
// and expands as Forward features are developed.
type Properties service

// Current returns property values for the authenticated user's organization.
func (s *Properties) Current(ctx context.Context, filter ports.PropertyFilter) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.get(ctx, "Properties.Current", propertyFilterPath("/api/config", filter))
}

// Organization returns property values for orgID. Forward support permission
// is required.
func (s *Properties) Organization(
	ctx context.Context,
	orgID string,
	filter ports.PropertyFilter,
) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	path := fmt.Sprintf("/api/orgs/%s/config", url.PathEscape(orgID))
	return s.get(ctx, "Properties.Organization", propertyFilterPath(path, filter))
}

// Global returns default property values. Global writes require Forward-admin
// permission, while visibility depends on the requested filter.
func (s *Properties) Global(ctx context.Context, filter ports.PropertyFilter) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.get(ctx, "Properties.Global", propertyFilterPath("/api/global-config", filter))
}

// SetCurrent sets an override for the authenticated user's organization.
func (s *Properties) SetCurrent(
	ctx context.Context,
	property ports.OrgProperty,
	value string,
) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.set(ctx, "Properties.SetCurrent", "/api/config", property, value)
}

// ClearCurrent deletes an override for the authenticated user's organization.
func (s *Properties) ClearCurrent(ctx context.Context, property ports.OrgProperty) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.clear(ctx, "Properties.ClearCurrent", "/api/config", property)
}

// SetOrganization sets an organization override. Forward support permission is required. This is the only way to set a property whose
// Level is ORG: Forward answers a global write to one (SetGlobal) with a 403 (IsGlobalDefaultNotAllowed), so such a property is always
// per organization and has to be set on each org you want it on. Most properties are ORG-level (for example
// EDGE_ASSETS_IN_SOURCES and THREAT_EXPOSURE); Forward exposes no list of which, so the 403 is how you find out.
func (s *Properties) SetOrganization(
	ctx context.Context,
	orgID string,
	property ports.OrgProperty,
	value string,
) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	return s.set(ctx, "Properties.SetOrganization", "/api/orgs/"+url.PathEscape(orgID)+"/config", property, value)
}

// ClearOrganization deletes an organization override. Forward support permission is required.
func (s *Properties) ClearOrganization(
	ctx context.Context,
	orgID string,
	property ports.OrgProperty,
) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	return s.clear(ctx, "Properties.ClearOrganization", "/api/orgs/"+url.PathEscape(orgID)+"/config", property)
}

// SetGlobal sets a global default. Forward-admin permission is required. A property whose Level is ORG (not BOTH) has no global default to
// change: Forward answers 403 "Permission denied. Global default value cannot be changed for <property>", which is
// IsGlobalDefaultNotAllowed (ErrorResponse.GateProperty names it); use SetOrganization for those, one org at a time.
func (s *Properties) SetGlobal(
	ctx context.Context,
	property ports.OrgProperty,
	value string,
) (ports.PropertyValues, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.set(ctx, "Properties.SetGlobal", "/api/global-config", property, value)
}

// ClearGlobal deletes a global default override. Forward-admin permission is required.
func (s *Properties) ClearGlobal(ctx context.Context, property ports.OrgProperty) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.clear(ctx, "Properties.ClearGlobal", "/api/global-config", property)
}

func (s *Properties) get(ctx context.Context, operation, path string) (ports.PropertyValues, *ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	values := ports.PropertyValues{}
	resp, err := s.client.Do(req, &values)
	if err != nil {
		return nil, resp, err
	}
	return values, resp, nil
}

func (s *Properties) set(
	ctx context.Context,
	operation string,
	basePath string,
	property ports.OrgProperty,
	value string,
) (ports.PropertyValues, *ports.Response, error) {
	path, err := propertyPath(basePath, property)
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"value": []string{value}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	values := ports.PropertyValues{}
	resp, err := s.client.Do(req, &values)
	if err != nil {
		return nil, resp, err
	}
	return values, resp, nil
}

func (s *Properties) clear(ctx context.Context, operation, basePath string, property ports.OrgProperty) (*ports.Response, error) {
	path, err := propertyPath(basePath, property)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}

func propertyPath(basePath string, property ports.OrgProperty) (string, error) {
	value := strings.TrimSpace(string(property))
	if value == "" {
		return "", errors.New("forward: organization property is required")
	}
	return strings.TrimRight(basePath, "/") + "/" + url.PathEscape(value), nil
}

func propertyFilterPath(path string, filter ports.PropertyFilter) string {
	if filter == "" {
		return path
	}
	return path + "?" + url.Values{"filter": []string{string(filter)}}.Encode()
}
