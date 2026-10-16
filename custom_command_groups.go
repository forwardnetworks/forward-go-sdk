package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CustomCommandGroupsService manages a network's custom collection groups: extra CLI commands (or SNMP OIDs) the collector runs
// on devices of chosen types in addition to Forward's own collection (the UI's Custom Commands page). A command here is NOT one
// of Forward's built-in collection commands. The organization-wide disabledCommands setting (OrgCollectionSettingsPatch) is
// matched against every command sent through an INTERACTIVE CLI session on a device whose software version the collector could
// read, built-in or custom (CliDevice.setupDisabledCommands hands the entries for the device's connection type and version
// range to the agent; InteractiveCliHelper.runCommandAndGetOutcome checks each command, each part of a ":::" sequence, as a
// full-string regular expression match on the lowercased command, the pattern lowercased too). A custom command that matches is
// silently skipped: its "Command:" line is written to the output with nothing under it and the snapshot and API show no error
// (the collector only logs one). Non-interactive SSH agents and endpoint profiles ignore the setting, and SNMP groups are
// unaffected.
//
// Routes are in CustomCommandGroupController (ClientController for the type list); preview, unpublished. Permissions: List needs
// the network operation VIEW_CUSTOM_COLLECTION_GROUPS; Create, Update, Delete and Validate need EDIT_CUSTOM_COLLECTION_GROUPS. A
// refusal is the typed 403 (ErrPermissionDenied). A group's commands are NOT checked against the approved-CLI-commands list
// (Endpoints.ApprovedCLICommands, which governs endpoint profiles only); they are checked per device type against Forward's own
// allowed and banned command patterns (CustomCliConfig, CustomCommandGroupUtils), so Create and Update answer 400 for a command a
// type does not allow (among others, ";", "!" and line breaks are refused for most types); Validate asks the same question
// without writing.
type CustomCommandGroupsService service

// CustomCommandGroup is a stored group. Type is "CLI" (Commands set) or "SNMP" (OIDs set); a group is one or the other.
// DeviceConnTypes are connection type ids (see ClassicDevices.ConnTypes). Devices are device names or globs the group applies to;
// empty means every device of those types. Disabled keeps the group but stops collecting it. Group names are unique ignoring
// case, and Update and Delete match the stored name exactly.
type CustomCommandGroup struct {
	Name            string   `json:"name"`
	DeviceConnTypes []string `json:"deviceConnTypes,omitempty"`
	Type            string   `json:"type,omitempty"`
	Commands        []string `json:"commands,omitempty"`
	// OIDs are an SNMP group's CustomOID values (names are unique ignoring case within a group, and so are the OIDs).
	OIDs     []CustomOID `json:"oids,omitempty"`
	Devices  []string    `json:"devices,omitempty"`
	Disabled bool        `json:"disabled"`
}

// NewCustomCommandGroup creates a group. Name and DeviceConnTypes are required, and exactly one of Commands and OIDs.
// Disabled nil creates it enabled.
type NewCustomCommandGroup struct {
	Name            string      `json:"name"`
	DeviceConnTypes []string    `json:"deviceConnTypes"`
	Commands        []string    `json:"commands,omitempty"`
	OIDs            []CustomOID `json:"oids,omitempty"`
	Devices         []string    `json:"devices,omitempty"`
	Disabled        *bool       `json:"disabled,omitempty"`
}

// CustomCommandGroupUpdate is a partial update; nil fields are left alone. Devices points at a slice so an empty one ("every
// device of the types") can be sent: use &[]string{}, because a pointer to a nil slice sends null, which Forward reads as "leave
// alone". Commands or OIDs must match the group's existing kind (else 400), and changing DeviceConnTypes re-checks the stored
// commands against the new types; a ":::" sequence needs a collector that supports it here too; Commands and OIDs replace the whole list, and at most one may be set. Name renames the
// group, which must stay unique ignoring case.
type CustomCommandGroupUpdate struct {
	Name            *string     `json:"name,omitempty"`
	DeviceConnTypes []string    `json:"deviceConnTypes,omitempty"`
	Commands        []string    `json:"commands,omitempty"`
	OIDs            []CustomOID `json:"oids,omitempty"`
	Devices         *[]string   `json:"devices,omitempty"`
	Disabled        *bool       `json:"disabled,omitempty"`
}

// ErrCustomCommandGroupNotFound is returned by Get when the network has no group of that name.
var ErrCustomCommandGroupNotFound = errors.New("forward: custom command group not found")

func (s *CustomCommandGroupsService) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/custom-command-groups", nil
}

// List returns the network's custom command groups (GET .../custom-command-groups).
func (s *CustomCommandGroupsService) List(ctx context.Context, networkID string) ([]CustomCommandGroup, *Response, error) {
	return s.listFor(ctx, networkID, "CustomCommandGroups.List")
}

// Get returns one group by name, ignoring case. Forward has no per-name read, so this filters the list; a name that is not there
// is ErrCustomCommandGroupNotFound.
func (s *CustomCommandGroupsService) Get(ctx context.Context, networkID, name string) (*CustomCommandGroup, *Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: custom command group name is required")
	}
	groups, resp, err := s.listFor(ctx, networkID, "CustomCommandGroups.Get")
	if err != nil {
		return nil, resp, err
	}
	for i := range groups {
		if strings.EqualFold(groups[i].Name, name) {
			return &groups[i], resp, nil
		}
	}
	return nil, resp, ErrCustomCommandGroupNotFound
}

func (s *CustomCommandGroupsService) listFor(ctx context.Context, networkID, operation string) ([]CustomCommandGroup, *Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out []CustomCommandGroup
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Create adds a group (POST .../custom-command-groups; 201, no body). A name already used (ignoring case) is a 400. A command
// sequence (":::") needs a collector that supports sequences, else 400.
func (s *CustomCommandGroupsService) Create(ctx context.Context, networkID string, group NewCustomCommandGroup) (*Response, error) {
	if strings.TrimSpace(group.Name) == "" {
		return nil, errors.New("forward: custom command group name is required")
	}
	if len(group.DeviceConnTypes) == 0 {
		return nil, errors.New("forward: a custom command group needs at least one device connection type")
	}
	if (len(group.Commands) == 0) == (len(group.OIDs) == 0) {
		return nil, errors.New("forward: a custom command group needs exactly one of Commands and OIDs")
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, group)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CustomCommandGroups.Create")
	return s.client.Do(req, nil)
}

// Update changes a group (PATCH .../custom-command-groups/{name}; 204). A name that does not exist is a 404 "Custom command
// group ... not found". Disable or enable a group with Disabled.
func (s *CustomCommandGroupsService) Update(ctx context.Context, networkID, name string, update CustomCommandGroupUpdate) (*Response, error) {
	if len(update.Commands) != 0 && len(update.OIDs) != 0 {
		return nil, errors.New("forward: at most one of Commands and OIDs may be set")
	}
	path, err := s.itemPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CustomCommandGroups.Update")
	return s.client.Do(req, nil)
}

// Delete removes a group (DELETE .../custom-command-groups/{name}; 204), matching the stored name exactly. A group that is not
// there counts as deleted (Forward's 404 "Custom command group ... not found"); a missing network does not, and is returned as the
// error. The commands stop being collected from the next collection on.
func (s *CustomCommandGroupsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.itemPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CustomCommandGroups.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) && !errors.Is(err, ErrNetworkNotFound) {
		return resp, nil
	}
	return resp, err
}

func (s *CustomCommandGroupsService) itemPath(networkID, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("forward: custom command group name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(name), nil
}

// UnsupportedCustomCommand is a command some of the asked device types do not allow.
type UnsupportedCustomCommand struct {
	Command              string   `json:"command"`
	UnsupportedConnTypes []string `json:"unsupportedConnTypes,omitempty"`
}

// Validate asks Forward which of commands the device types do not allow, without creating anything (POST
// .../custom-command-groups?action=validate; EDIT_CUSTOM_COLLECTION_GROUPS). An empty result means every command is accepted.
func (s *CustomCommandGroupsService) Validate(ctx context.Context, networkID string, deviceConnTypes, commands []string) ([]UnsupportedCustomCommand, *Response, error) {
	if len(deviceConnTypes) == 0 || len(commands) == 0 {
		return nil, nil, errors.New("forward: device connection types and commands are required")
	}
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			return nil, nil, fmt.Errorf("forward: a command to validate is blank")
		}
	}
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=validate", map[string]any{"deviceConnTypes": deviceConnTypes, "commands": commands})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CustomCommandGroups.Validate")
	var out struct {
		Unsupported []UnsupportedCustomCommand `json:"unsupported"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.Unsupported, resp, err
}
