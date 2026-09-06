package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// AccessControlService covers device access labels and access control groups.
//
// These routes post-date the Skyforge inventory this SDK was first cut from;
// Skyforge's own client grew them in September 2026 and the SDK did not, which
// left ACG reconciliation as the last hand-rolled Forward surface. The
// semantics below were carried over from that client because two of them are
// easy to get wrong and one of them was, in production.
type AccessControlService service

// DeviceAccessLabel selects devices by exact name or glob. A label narrows a
// network-scoped access control group to a subset of devices.
type DeviceAccessLabel struct {
	ID          Identifier `json:"id"`
	Name        string     `json:"name"`
	DeviceNames []string   `json:"deviceNames,omitempty"`
	DeviceGlobs []string   `json:"deviceGlobs,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
}

// DeviceAccessLabelRequest is the exact POST/PATCH body (measured: these keys,
// always present, never omitted).
type DeviceAccessLabelRequest struct {
	Name        string   `json:"name"`
	DeviceNames []string `json:"deviceNames"`
	DeviceGlobs []string `json:"deviceGlobs"`
}

// AccessControlGroup is a stored group.
//
// NETWORK ROLES CARRY THE ORG-ADMIN SIGNAL, AND NIL IS NOT EMPTY. Forward
// serialises an org-admin group with networkRoles NULL (omitted), and a
// network-scoped group with a map, even an empty one. Skyforge's first client
// coerced nil to {} on decode and every org-admin grant vanished from view.
// This type does not coerce: a nil NetworkRoles means org admin, an empty map
// means "no network roles". IsOrgAdmin is the only reader anyone should use.
type AccessControlGroup struct {
	ID                   Identifier        `json:"id"`
	Name                 string            `json:"name"`
	ExternalGroupNames   []string          `json:"externalGroupNames"`
	NetworkRoles         map[string]string `json:"networkRoles"`
	DeviceAccessLabelIDs []string          `json:"deviceAccessLabelIds"`
}

// IsOrgAdmin reports whether the group grants org-wide admin. The
// discriminator is nil-ness of NetworkRoles, not its length.
func (g AccessControlGroup) IsOrgAdmin() bool { return g.NetworkRoles == nil }

// AccessControlGroupRequest is the POST body for create and update.
//
// Leaving NetworkRoles nil marshals to JSON null, which IS the request for an
// org-admin group. Use OrgAdminGroup / NetworkGroup rather than building this
// by hand so the two shapes cannot be confused.
type AccessControlGroupRequest struct {
	Name                 string            `json:"name"`
	ExternalGroupNames   []string          `json:"externalGroupNames"`
	NetworkRoles         map[string]string `json:"networkRoles"`
	DeviceAccessLabelIDs []string          `json:"deviceAccessLabelIds"`
}

// OrgAdminGroup builds a request for an org-wide admin group. It cannot carry
// device access labels: Forward forbids narrowing org-wide admin to a device
// subset, and Validate refuses the combination before any request is sent.
func OrgAdminGroup(name string, externalGroupNames []string) AccessControlGroupRequest {
	return AccessControlGroupRequest{
		Name:               strings.TrimSpace(name),
		ExternalGroupNames: nonNil(externalGroupNames),
		NetworkRoles:       nil,
	}
}

// NetworkGroup builds a request for a network-scoped group. A nil networkRoles
// is promoted to an empty map so it cannot accidentally mean org admin.
func NetworkGroup(name string, externalGroupNames []string, networkRoles map[string]string, deviceAccessLabelIDs []string) AccessControlGroupRequest {
	if networkRoles == nil {
		networkRoles = map[string]string{}
	}
	return AccessControlGroupRequest{
		Name:                 strings.TrimSpace(name),
		ExternalGroupNames:   nonNil(externalGroupNames),
		NetworkRoles:         networkRoles,
		DeviceAccessLabelIDs: nonNil(deviceAccessLabelIDs),
	}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// Validate applies Forward's own constraints before a request is sent, so the
// error names the rule rather than arriving as a 400 with a body to parse.
func (r AccessControlGroupRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("forward: access control group name is required")
	}
	if r.NetworkRoles == nil && len(r.DeviceAccessLabelIDs) > 0 {
		return fmt.Errorf("forward: access control group %q asks for org admin (nil networkRoles) and %d device access label(s); Forward forbids that combination because org-wide admin cannot be narrowed to a device subset", r.Name, len(r.DeviceAccessLabelIDs))
	}
	return nil
}

func (r DeviceAccessLabelRequest) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("forward: device access label name is required")
	}
	if len(r.DeviceNames) == 0 && len(r.DeviceGlobs) == 0 {
		return fmt.Errorf("forward: device access label %q selects no devices (no names, no globs)", r.Name)
	}
	return nil
}

// --- device access labels ----------------------------------------------------

func (s *AccessControlService) ListDeviceAccessLabels(ctx context.Context) ([]DeviceAccessLabel, *Response, error) {
	result := listResponse[DeviceAccessLabel]{Keys: []string{"labels", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/device-access-labels", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.ListDeviceAccessLabels")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AccessControlService) CreateDeviceAccessLabel(ctx context.Context, input DeviceAccessLabelRequest) (*DeviceAccessLabel, *Response, error) {
	if err := input.validate(); err != nil {
		return nil, nil, err
	}
	input.DeviceNames, input.DeviceGlobs = nonNil(input.DeviceNames), nonNil(input.DeviceGlobs)
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/device-access-labels", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.CreateDeviceAccessLabel")
	out := new(DeviceAccessLabel)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *AccessControlService) UpdateDeviceAccessLabel(ctx context.Context, labelID string, input DeviceAccessLabelRequest) (*DeviceAccessLabel, *Response, error) {
	labelID = strings.TrimSpace(labelID)
	if labelID == "" {
		return nil, nil, errors.New("forward: device access label ID is required")
	}
	if err := input.validate(); err != nil {
		return nil, nil, err
	}
	input.DeviceNames, input.DeviceGlobs = nonNil(input.DeviceNames), nonNil(input.DeviceGlobs)
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/device-access-labels/"+url.PathEscape(labelID), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.UpdateDeviceAccessLabel")
	out := new(DeviceAccessLabel)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// DeleteDeviceAccessLabel is idempotent: a 404 is success, because the desired
// state -- the label does not exist -- already holds.
func (s *AccessControlService) DeleteDeviceAccessLabel(ctx context.Context, labelID string) (*Response, error) {
	labelID = strings.TrimSpace(labelID)
	if labelID == "" {
		return nil, errors.New("forward: device access label ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/device-access-labels/"+url.PathEscape(labelID), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "AccessControl.DeleteDeviceAccessLabel")
	return s.client.doAccepted(req, nil, true, func(code int) bool { return code == http.StatusNotFound || (code >= 200 && code < 300) })
}

// --- access control groups ----------------------------------------------------

func (s *AccessControlService) ListGroups(ctx context.Context) ([]AccessControlGroup, *Response, error) {
	result := listResponse[AccessControlGroup]{Keys: []string{"groups", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/access-control-groups", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.ListGroups")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AccessControlService) CreateGroup(ctx context.Context, input AccessControlGroupRequest) (*AccessControlGroup, *Response, error) {
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	input.ExternalGroupNames = nonNil(input.ExternalGroupNames)
	if input.NetworkRoles != nil {
		input.DeviceAccessLabelIDs = nonNil(input.DeviceAccessLabelIDs)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/access-control-groups", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.CreateGroup")
	out := new(AccessControlGroup)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// UpdateGroup is a POST to the group's URL, not a PATCH -- that is the route
// Forward serves. The nil-networkRoles org-admin signal means exactly what it
// means on create.
func (s *AccessControlService) UpdateGroup(ctx context.Context, groupID string, input AccessControlGroupRequest) (*AccessControlGroup, *Response, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, nil, errors.New("forward: access control group ID is required")
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	input.ExternalGroupNames = nonNil(input.ExternalGroupNames)
	if input.NetworkRoles != nil {
		input.DeviceAccessLabelIDs = nonNil(input.DeviceAccessLabelIDs)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/access-control-groups/"+url.PathEscape(groupID), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.UpdateGroup")
	out := new(AccessControlGroup)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// DeleteGroup is idempotent: a 404 is success.
func (s *AccessControlService) DeleteGroup(ctx context.Context, groupID string) (*Response, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, errors.New("forward: access control group ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, "/api/access-control-groups/"+url.PathEscape(groupID), nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "AccessControl.DeleteGroup")
	return s.client.doAccepted(req, nil, true, func(code int) bool { return code == http.StatusNotFound || (code >= 200 && code < 300) })
}
