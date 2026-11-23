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

// AccessControlService covers device access labels and access control groups.
//
// These routes post-date the Skyforge inventory this SDK was first cut from;
// Skyforge's own client grew them in September 2026 and the SDK did not, which
// left ACG reconciliation as the last hand-rolled Forward surface. The
// semantics below were carried over from that client because two of them are
// easy to get wrong and one of them was, in production.
type AccessControl service

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// --- device access labels ----------------------------------------------------

func (s *AccessControl) ListDeviceAccessLabels(ctx context.Context) ([]ports.DeviceAccessLabel, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.DeviceAccessLabel]{Keys: []string{"labels", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/device-access-labels", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.ListDeviceAccessLabels")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AccessControl) CreateDeviceAccessLabel(ctx context.Context, input ports.DeviceAccessLabelRequest) (*ports.DeviceAccessLabel, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	input.DeviceNames, input.DeviceGlobs = nonNil(input.DeviceNames), nonNil(input.DeviceGlobs)
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/device-access-labels", input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.CreateDeviceAccessLabel")
	out := new(ports.DeviceAccessLabel)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

func (s *AccessControl) UpdateDeviceAccessLabel(ctx context.Context, labelID string, input ports.DeviceAccessLabelRequest) (*ports.DeviceAccessLabel, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	labelID = strings.TrimSpace(labelID)
	if labelID == "" {
		return nil, nil, errors.New("forward: device access label ID is required")
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	input.DeviceNames, input.DeviceGlobs = nonNil(input.DeviceNames), nonNil(input.DeviceGlobs)
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/device-access-labels/"+url.PathEscape(labelID), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.UpdateDeviceAccessLabel")
	out := new(ports.DeviceAccessLabel)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// DeleteDeviceAccessLabel is idempotent: a 404 is success, because the desired
// state -- the label does not exist -- already holds.
func (s *AccessControl) DeleteDeviceAccessLabel(ctx context.Context, labelID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *AccessControl) ListGroups(ctx context.Context) ([]ports.AccessControlGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.AccessControlGroup]{Keys: []string{"groups", "items"}}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/access-control-groups", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.ListGroups")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *AccessControl) CreateGroup(ctx context.Context, input ports.AccessControlGroupRequest) (*ports.AccessControlGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.AccessControlGroup)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// UpdateGroup is a POST to the group's URL, not a PATCH -- that is the route
// Forward serves. The nil-networkRoles org-admin signal means exactly what it
// means on create.
func (s *AccessControl) UpdateGroup(ctx context.Context, groupID string, input ports.AccessControlGroupRequest) (*ports.AccessControlGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.AccessControlGroup)
	response, err := s.client.doRequired(req, out)
	return out, response, err
}

// DeleteGroup is idempotent: a 404 is success.
func (s *AccessControl) DeleteGroup(ctx context.Context, groupID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

// GroupNames maps each access control group's ID to its name. Unlike ListGroups it needs no permission beyond being signed in. GET
// /api/access-control-groups?view=names (AccessController; on primary 15398425a69 and stable 67e89c87124). Preview.
func (s *AccessControl) GroupNames(ctx context.Context) (map[string]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.names(ctx, "/api/access-control-groups?view=names", "AccessControl.GroupNames")
}

// DeviceAccessLabelNames maps each device access label's ID to its name. GET /api/device-access-labels?view=names (VIEW_USER_ACCOUNTS).
// Preview.
func (s *AccessControl) DeviceAccessLabelNames(ctx context.Context) (map[string]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.names(ctx, "/api/device-access-labels?view=names", "AccessControl.DeviceAccessLabelNames")
}

func (s *AccessControl) names(ctx context.Context, path, operation string) (map[string]string, *ports.Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := map[string]string{}
	response, err := s.client.doRequired(req, &out)
	return out, response, err
}

// SetGroupNetworkRole gives an access control group a role on a WORKSPACE network and returns the group. POST
// /api/access-control-groups/{id}/network-roles/{networkId}?role= (ASSIGN_ROLES on that network). Forward refuses a network that is not
// a workspace, and a role above the caller's own on it. Preview.
func (s *AccessControl) SetGroupNetworkRole(ctx context.Context, groupID, networkID string, role ports.NetworkRole) (*ports.AccessControlGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	groupID, networkID = strings.TrimSpace(groupID), strings.TrimSpace(networkID)
	if groupID == "" || networkID == "" {
		return nil, nil, errors.New("forward: access control group ID and network ID are required")
	}
	if !role.Valid() {
		return nil, nil, fmt.Errorf("forward: invalid network role %q", role)
	}
	path := "/api/access-control-groups/" + url.PathEscape(groupID) + "/network-roles/" + url.PathEscape(networkID) + "?" +
		url.Values{"role": []string{string(role)}}.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "AccessControl.SetGroupNetworkRole")
	out := new(ports.AccessControlGroup)
	response, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, response, err
	}
	return out, response, nil
}

// AddLabels adds device access labels to access control groups. PATCH /api/access-control-groups?action=addLabels {groupIds, labelIds}
// (MANAGE_USER_ACCOUNTS). Preview.
func (s *AccessControl) AddLabels(ctx context.Context, groupIDs, labelIDs []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.editLabels(ctx, "addLabels", "AccessControl.AddLabels", groupIDs, labelIDs)
}

// RemoveLabels removes device access labels from access control groups, narrowing which devices their members can see. PATCH
// /api/access-control-groups?action=removeLabels {groupIds, labelIds} (MANAGE_USER_ACCOUNTS). Preview.
func (s *AccessControl) RemoveLabels(ctx context.Context, groupIDs, labelIDs []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.editLabels(ctx, "removeLabels", "AccessControl.RemoveLabels", groupIDs, labelIDs)
}

func (s *AccessControl) editLabels(ctx context.Context, action, operation string, groupIDs, labelIDs []string) (*ports.Response, error) {
	groupIDs, labelIDs = nonEmptyStrings(groupIDs), nonEmptyStrings(labelIDs)
	if len(groupIDs) == 0 || len(labelIDs) == 0 {
		return nil, errors.New("forward: group IDs and label IDs are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, "/api/access-control-groups?"+url.Values{"action": []string{action}}.Encode(),
		map[string][]string{"groupIds": groupIDs, "labelIds": labelIDs})
	if err != nil {
		return nil, err
	}
	req = markOperation(req, operation)
	return s.client.Do(req, nil)
}
