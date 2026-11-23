package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"sort"
)

// ConnTypes lists the device connection types that can be classic devices, including INTERNAL ones the UI needs the names of
// (GET /api/deviceConnTypes?for=ui, ClientController.getDeviceTypes; any signed-in user; without for=ui only the exposed types come back). Sorted by ID. Preview:
// unpublished. The UI's "devices need to be migrated" count joins a device's type (or its test result's DiscoveredType)
// against this list.
func (s *ClassicDevices) ConnTypes(ctx context.Context) ([]ports.ConnTypeInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/deviceConnTypes?for=ui", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ConnTypes")
	var byID map[string]ports.ConnTypeInfo
	response, err := s.client.doRequired(req, &byID)
	out := make([]ports.ConnTypeInfo, 0, len(byID))
	for id, info := range byID {
		info.ID = id
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, response, err
}

// ConnTypesExposed lists only the exposed types (GET /api/deviceConnTypes with no for=, ClientController.getDeviceTypes). This is
// the NARROWER list: ConnTypes (for=ui) already includes every type that can be a classic device, INTERNAL and UNDER_DEVELOPMENT
// ones too, so use ConnTypes to learn about a hidden type and this to see what the product offers. Sorted by ID. Preview.
func (s *ClassicDevices) ConnTypesExposed(ctx context.Context) ([]ports.ConnTypeInfo, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/deviceConnTypes", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ConnTypesExposed")
	var byID map[string]ports.ConnTypeInfo
	response, err := s.client.doRequired(req, &byID)
	out := make([]ports.ConnTypeInfo, 0, len(byID))
	for id, info := range byID {
		info.ID = id
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, response, err
}
