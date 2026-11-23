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
type CustomCommandGroups service

func (s *CustomCommandGroups) base(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/custom-command-groups", nil
}

// List returns the network's custom command groups (GET .../custom-command-groups).
func (s *CustomCommandGroups) List(ctx context.Context, networkID string) ([]ports.CustomCommandGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.listFor(ctx, networkID, "CustomCommandGroups.List")
}

// Get returns one group by name, ignoring case. Forward has no per-name read, so this filters the list; a name that is not there
// is ErrCustomCommandGroupNotFound.
func (s *CustomCommandGroups) Get(ctx context.Context, networkID, name string) (*ports.CustomCommandGroup, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	return nil, resp, ports.ErrCustomCommandGroupNotFound
}

func (s *CustomCommandGroups) listFor(ctx context.Context, networkID, operation string) ([]ports.CustomCommandGroup, *ports.Response, error) {
	path, err := s.base(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	var out []ports.CustomCommandGroup
	resp, err := s.client.doRequired(req, &out)
	return out, resp, err
}

// Create adds a group (POST .../custom-command-groups; 201, no body). A name already used (ignoring case) is a 400. A command
// sequence (":::") needs a collector that supports sequences, else 400.
func (s *CustomCommandGroups) Create(ctx context.Context, networkID string, group ports.NewCustomCommandGroup) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *CustomCommandGroups) Update(ctx context.Context, networkID, name string, update ports.CustomCommandGroupUpdate) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *CustomCommandGroups) Delete(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) && !errors.Is(err, ports.ErrNetworkNotFound) {
		return resp, nil
	}
	return resp, err
}

func (s *CustomCommandGroups) itemPath(networkID, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("forward: custom command group name is required")
	}
	path, err := s.base(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(name), nil
}

// Validate asks Forward which of commands the device types do not allow, without creating anything (POST
// .../custom-command-groups?action=validate; EDIT_CUSTOM_COLLECTION_GROUPS). An empty result means every command is accepted.
func (s *CustomCommandGroups) Validate(ctx context.Context, networkID string, deviceConnTypes, commands []string) ([]ports.UnsupportedCustomCommand, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
		Unsupported []ports.UnsupportedCustomCommand `json:"unsupported"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.Unsupported, resp, err
}
