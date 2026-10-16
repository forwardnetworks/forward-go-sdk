package forward

import (
	"context"
	"net/http"
	"sort"
)

// ConnTypeInfo describes one device connection type (Forward's DeviceConnType) as the UI sees it.
type ConnTypeInfo struct {
	// ID is the type's key in the response (cisco_apic_api, cisco_ios_xe_ssh, ...), the value a classic device's Type carries.
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Vendor string `json:"vendor,omitempty"`
	// Status is PRODUCTION, TECH_PREVIEW, INTERNAL or UNDER_DEVELOPMENT.
	Status                      string   `json:"status,omitempty"`
	Protocol                    string   `json:"protocol,omitempty"`
	HTTPCredentialType          string   `json:"httpCredentialType,omitempty"`
	SecondaryCLICredentialTypes []string `json:"secondaryCliCredentialTypes,omitempty"`
	// CollectionSourceType is CLASSIC or CONTROLLER_MANAGED.
	CollectionSourceType string `json:"collectionSourceType,omitempty"`
	// RelatedTypes are the type IDs a CONTROLLER_MANAGED type manages (a controller and its devices); empty for any other type.
	RelatedTypes []string `json:"relatedTypes,omitempty"`
}

// ConnTypes lists the device connection types that can be classic devices, including INTERNAL ones the UI needs the names of
// (GET /api/deviceConnTypes?for=ui, ClientController.getDeviceTypes; any signed-in user; without for=ui only the exposed types come back). Sorted by ID. Preview:
// unpublished. The UI's "devices need to be migrated" count joins a device's type (or its test result's DiscoveredType)
// against this list.
func (s *ClassicDevicesService) ConnTypes(ctx context.Context) ([]ConnTypeInfo, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/deviceConnTypes?for=ui", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ConnTypes")
	var byID map[string]ConnTypeInfo
	response, err := s.client.doRequired(req, &byID)
	out := make([]ConnTypeInfo, 0, len(byID))
	for id, info := range byID {
		info.ID = id
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, response, err
}
