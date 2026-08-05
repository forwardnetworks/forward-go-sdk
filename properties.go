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

// PropertiesService accesses Forward organization and global properties.
//
// Preview: this configuration surface is unpublished, permission-sensitive,
// and expands as Forward features are developed.
type PropertiesService service

// OrgProperty is a Forward organization property key. Property names are
// intentionally not enumerated: the available set is version- and
// deployment-dependent.
type OrgProperty string

// PropertyFilter selects effective, configured, or overridden values.
type PropertyFilter string

const (
	PropertyFilterOff          PropertyFilter = "OFF"
	PropertyFilterConfigurable PropertyFilter = "CONFIGURABLE"
	PropertyFilterConfigured   PropertyFilter = "CONFIGURED"
	PropertyFilterOverridden   PropertyFilter = "OVERRIDDEN"
	PropertyFilterNondefault   PropertyFilter = "NONDEFAULT"
)

// PropertyValues retains dynamic bool, number, string, list, and object values
// without lossy conversions.
type PropertyValues map[OrgProperty]json.RawMessage

// Current returns property values for the authenticated user's organization.
func (s *PropertiesService) Current(ctx context.Context, filter PropertyFilter) (PropertyValues, *Response, error) {
	return s.get(ctx, propertyFilterPath("/api/config", filter))
}

// Organization returns property values for orgID. Forward support permission
// is required.
func (s *PropertiesService) Organization(
	ctx context.Context,
	orgID string,
	filter PropertyFilter,
) (PropertyValues, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	path := fmt.Sprintf("/api/orgs/%s/config", url.PathEscape(orgID))
	return s.get(ctx, propertyFilterPath(path, filter))
}

// Global returns default property values. Global writes require Forward-admin
// permission, while visibility depends on the requested filter.
func (s *PropertiesService) Global(ctx context.Context, filter PropertyFilter) (PropertyValues, *Response, error) {
	return s.get(ctx, propertyFilterPath("/api/global-config", filter))
}

// SetCurrent sets an override for the authenticated user's organization.
func (s *PropertiesService) SetCurrent(
	ctx context.Context,
	property OrgProperty,
	value string,
) (PropertyValues, *Response, error) {
	return s.set(ctx, "/api/config", property, value)
}

// ClearCurrent deletes an override for the authenticated user's organization.
func (s *PropertiesService) ClearCurrent(ctx context.Context, property OrgProperty) (*Response, error) {
	return s.clear(ctx, "/api/config", property)
}

// SetOrganization sets an organization override. Forward support permission is required.
func (s *PropertiesService) SetOrganization(
	ctx context.Context,
	orgID string,
	property OrgProperty,
	value string,
) (PropertyValues, *Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	return s.set(ctx, "/api/orgs/"+url.PathEscape(orgID)+"/config", property, value)
}

// ClearOrganization deletes an organization override. Forward support permission is required.
func (s *PropertiesService) ClearOrganization(
	ctx context.Context,
	orgID string,
	property OrgProperty,
) (*Response, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	return s.clear(ctx, "/api/orgs/"+url.PathEscape(orgID)+"/config", property)
}

// SetGlobal sets a global default. Forward-admin permission is required.
func (s *PropertiesService) SetGlobal(
	ctx context.Context,
	property OrgProperty,
	value string,
) (PropertyValues, *Response, error) {
	return s.set(ctx, "/api/global-config", property, value)
}

// ClearGlobal deletes a global default override. Forward-admin permission is required.
func (s *PropertiesService) ClearGlobal(ctx context.Context, property OrgProperty) (*Response, error) {
	return s.clear(ctx, "/api/global-config", property)
}

func (s *PropertiesService) get(ctx context.Context, path string) (PropertyValues, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	values := PropertyValues{}
	resp, err := s.client.Do(req, &values)
	if err != nil {
		return nil, resp, err
	}
	return values, resp, nil
}

func (s *PropertiesService) set(
	ctx context.Context,
	basePath string,
	property OrgProperty,
	value string,
) (PropertyValues, *Response, error) {
	path, err := propertyPath(basePath, property)
	if err != nil {
		return nil, nil, err
	}
	path += "?" + url.Values{"value": []string{value}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return nil, nil, err
	}
	values := PropertyValues{}
	resp, err := s.client.Do(req, &values)
	if err != nil {
		return nil, resp, err
	}
	return values, resp, nil
}

func (s *PropertiesService) clear(ctx context.Context, basePath string, property OrgProperty) (*Response, error) {
	path, err := propertyPath(basePath, property)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func propertyPath(basePath string, property OrgProperty) (string, error) {
	value := strings.TrimSpace(string(property))
	if value == "" {
		return "", errors.New("forward: organization property is required")
	}
	return strings.TrimRight(basePath, "/") + "/" + url.PathEscape(value), nil
}

func propertyFilterPath(path string, filter PropertyFilter) string {
	if filter == "" {
		return path
	}
	return path + "?" + url.Values{"filter": []string{string(filter)}}.Encode()
}
