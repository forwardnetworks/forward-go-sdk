package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// OrganizationsService manages the current organization and the unpublished
// support/admin organization surface.
//
// Preview: /api/admin/orgs is permission-sensitive and version-dependent.
type OrganizationsService service

// Organization is a Forward tenant. Raw retains version-specific response
// fields returned by administrative builds.
type Organization struct {
	ID            Identifier `json:"id"`
	Name          string     `json:"name"`
	Disabled      bool       `json:"disabled,omitempty"`
	SFDCAccountID string     `json:"sfdcAccountId,omitempty"`
	SFDCContactID string     `json:"sfdcContactId,omitempty"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
}

func (o *Organization) UnmarshalJSON(data []byte) error {
	type plain Organization
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*o = Organization(value)
	o.Raw = raw
	return nil
}

// OrganizationCreateRequest contains known administrative query fields.
// ExtraQuery carries fields introduced by a particular Forward version.
type OrganizationCreateRequest struct {
	Name          string
	Type          string
	OnPrem        *bool
	SFDCAccountID string
	SFDCContactID string
	ExtraQuery    url.Values
}

func (s *OrganizationsService) Current(ctx context.Context) (*Organization, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/orgs/current", nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// List returns organizations visible to a Forward support principal.
func (s *OrganizationsService) List(ctx context.Context) ([]Organization, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/admin/orgs", nil)
	if err != nil {
		return nil, nil, err
	}
	var orgs []Organization
	resp, err := s.client.Do(req, &orgs)
	return orgs, resp, err
}

// Create creates an organization using the support-admin API.
func (s *OrganizationsService) Create(ctx context.Context, input OrganizationCreateRequest) (*Organization, *Response, error) {
	if input.Name = strings.TrimSpace(input.Name); input.Name == "" {
		return nil, nil, errors.New("forward: organization name is required")
	}
	query := cloneValues(input.ExtraQuery)
	query.Set("name", input.Name)
	setString(query, "type", input.Type)
	setBool(query, "onPrem", input.OnPrem)
	setString(query, "sfdcAccountId", input.SFDCAccountID)
	setString(query, "sfdcContactId", input.SFDCContactID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, "/api/admin/orgs?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}

// Update renames an organization. Current updates the authenticated tenant;
// an explicit ID uses the Forward-admin route.
func (s *OrganizationsService) Update(ctx context.Context, orgID, name string) (*Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, errors.New("forward: organization name is required")
	}
	path := "/api/orgs/current"
	if orgID = strings.TrimSpace(orgID); orgID != "" {
		path = "/api/admin/orgs/" + url.PathEscape(orgID)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// SetEnabled enables or disables an organization.
func (s *OrganizationsService) SetEnabled(ctx context.Context, orgID string, enabled bool) (*Response, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, errors.New("forward: organization ID is required")
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	path := "/api/admin/orgs/" + url.PathEscape(orgID) + "?" + url.Values{"action": []string{action}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *OrganizationsService) Delete(ctx context.Context, orgID string) (*Organization, *Response, error) {
	if orgID = strings.TrimSpace(orgID); orgID == "" {
		return nil, nil, errors.New("forward: organization ID is required")
	}
	path := "/api/admin/orgs/" + url.PathEscape(orgID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	org := new(Organization)
	resp, err := s.client.Do(req, org)
	return org, resp, err
}
