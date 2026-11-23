package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// ClassicDevicesService manages device collection sources.
type ClassicDevices service

func (s *ClassicDevices) PutBatch(ctx context.Context, networkID string, devices []ports.ClassicDeviceBatchItem) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, nil
	}
	path, err := classicDevicesPath(s.client, networkID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=putBatch", devices)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ClassicDevices.PutBatch")
	return s.client.Do(req, nil)
}

func (s *ClassicDevices) ListTestStatuses(ctx context.Context, networkID string) ([]ports.SourceTestStatus, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicesPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.SourceTestStatus]{Keys: []string{"devices", "classicDevices", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?with=testResult&with=snmpCollectionStatus", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ListTestStatuses")
	response, err := s.client.doRequired(req, &result)
	for i := range result.Items {
		result.Items[i].SourceKind = "classic"
	}
	return result.Items, response, err
}

// ResolveGlobs says which of the network's classic devices of the given connection types each glob matches (POST
// ...classic-devices?action=resolveGlobs, ClassicDeviceController.resolveGlobs; VIEW_COLLECTION_SOURCES; a read, nothing changes).
// A custom command group's Devices are such globs, so this answers "which devices would this group's commands run on". The
// result maps each glob, as sent, to the matching device names; a glob that matches nothing may be missing from the map (Forward
// sends {} when no classic device of those types exists), so read it with a lookup. Globs match ignoring case. Globs and connTypes must be non-empty,
// and no glob blank.
func (s *ClassicDevices) ResolveGlobs(ctx context.Context, networkID string, globs, connTypes []string) (map[string][]string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if len(globs) == 0 || len(connTypes) == 0 {
		return nil, nil, errors.New("forward: globs and connection types are required")
	}
	for _, glob := range globs {
		if strings.TrimSpace(glob) == "" {
			return nil, nil, errors.New("forward: a glob is blank")
		}
	}
	path, err := classicDevicesPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=resolveGlobs", map[string]any{"globs": globs, "connTypes": connTypes})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ResolveGlobs")
	var out struct {
		GlobToResolvedDevices map[string][]string `json:"globToResolvedDevices"`
	}
	resp, err := s.client.doRequired(req, &out)
	return out.GlobToResolvedDevices, resp, err
}

// List reads every classic device of the network in one request: GET
// /api/networks/{networkId}/classic-devices takes no offset or limit
// (ClassicDeviceController.getClassicDevices), so there is no paging to be
// unstable. For a subset by name use GetMany.
func (s *ClassicDevices) List(ctx context.Context, networkID string, with ...string) ([]ports.ClassicDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicesPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	if len(with) != 0 {
		path += "?" + url.Values{"with": with}.Encode()
	}
	// TOLERATE EVERY ENVELOPE FORWARD HAS ACTUALLY RETURNED, not just the one
	// this route returned when it was written.
	//
	// A reader that knows one shape does not fail on another -- it reports an
	// EMPTY list. Skyforge prunes classic devices by diffing this list against
	// the topology, so an empty answer reads as "nothing to prune" and the
	// drift it is meant to remove survives silently. Measured on cs-lab
	// (network 3150) 2026-09-05: a renamed device left a stale record, and the
	// SNMP contract then failed every sync with "12/13 devices observed"
	// against a lab that was healthy.
	//
	// So this accepts a bare array, a single object, and the wrapped keys
	// Forward uses across its list routes -- the same set Endpoints.List
	// accepts, for the same reason.
	result := listResponse[ports.ClassicDevice]{
		Keys:        []string{"devices", "classicDevices", "items", "data", "results"},
		AllowSingle: true,
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.List")
	resp, err := s.client.Do(req, &result)
	return result.Items, resp, err
}

func (s *ClassicDevices) Create(ctx context.Context, networkID string, input ports.ClassicDeviceRequest) (*ports.ClassicDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicesPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.Create")
	device := new(ports.ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevices) Get(ctx context.Context, networkID, deviceName string, with ...string) (*ports.ClassicDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicePath(s.client, networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	if len(with) != 0 {
		path += "?" + url.Values{"with": with}.Encode()
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.Get")
	device := new(ports.ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevices) Put(ctx context.Context, networkID, deviceName string, input ports.ClassicDeviceRequest) (*ports.ClassicDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicePath(s.client, networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.Put")
	device := new(ports.ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

// Patch updates only the fields the patch states.
func (s *ClassicDevices) Patch(ctx context.Context, networkID, deviceName string, patch ports.ClassicDevicePatch) (*ports.ClassicDevice, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := classicDevicePath(s.client, networkID, deviceName)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.Patch")
	device := new(ports.ClassicDevice)
	resp, err := s.client.Do(req, device)
	return device, resp, err
}

func (s *ClassicDevices) Delete(ctx context.Context, networkID, deviceName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := classicDevicePath(s.client, networkID, deviceName)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "ClassicDevices.Delete")
	return s.client.Do(req, nil)
}

// classicDevicesPath resolves an empty network ID from the client's bound network, as the other network-scoped services do.
func classicDevicesPath(c *Client, networkID string) (string, error) {
	return c.networkSubPath(networkID, "classic-devices")
}

func classicDevicePath(c *Client, networkID, deviceName string) (string, error) {
	path, err := classicDevicesPath(c, networkID)
	if err != nil {
		return "", err
	}
	if deviceName = strings.TrimSpace(deviceName); deviceName == "" {
		return "", errors.New("forward: device name is required")
	}
	return path + "/" + url.PathEscape(deviceName), nil
}
