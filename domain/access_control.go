package domain

import (
	"errors"
	"fmt"
	"strings"
)

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
	// OrgAdmin is Forward's own flag for an org-admin group, when the build sends it. IsOrgAdmin keeps reading nil NetworkRoles,
	// which every build has.
	OrgAdmin    *bool      `json:"orgAdmin,omitempty"`
	CreatedAt   string     `json:"createdAt,omitempty"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	CreatedByID Identifier `json:"createdById,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
	UpdatedByID Identifier `json:"updatedById,omitempty"`
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

func (r DeviceAccessLabelRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("forward: device access label name is required")
	}
	if len(r.DeviceNames) == 0 && len(r.DeviceGlobs) == 0 {
		return fmt.Errorf("forward: device access label %q selects no devices (no names, no globs)", r.Name)
	}
	return nil
}
