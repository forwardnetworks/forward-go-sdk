package forward

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

// Connection type support statuses (DeviceConnType.SupportStatus in Forward's client-structs). Only PRODUCTION and TECH_PREVIEW
// types are "exposed": the default list without for=ui carries just those. The Kubernetes types (k8s_cni_*_api) are
// UNDER_DEVELOPMENT: Forward accepts and collects them, but only the DEV parser (org property PARSER=DEV) turns the data into devices.
const (
	ConnTypeStatusProduction       = "PRODUCTION"
	ConnTypeStatusTechPreview      = "TECH_PREVIEW"
	ConnTypeStatusInternal         = "INTERNAL"
	ConnTypeStatusUnderDevelopment = "UNDER_DEVELOPMENT"
	ConnTypeStatusNotSupported     = "NOT_SUPPORTED"
)

// Kubernetes CNI connection types (Forward's DeviceConnType K8S_CNI_*_API, vendorOs K8S_CNI_*; Preview: appserver
// client-structs DeviceConnType.java). The control-plane type takes an API_KEY HTTP credential (a bearer token); the node and pod
// types are LOGIN. Forward generates devices named <source>_<node> and <source>_<ns>_<pod> from a control-plane source.
const (
	ConnTypeK8sCniControlPlaneAPI = "k8s_cni_control_plane_api"
	ConnTypeK8sCniNodeAPI         = "k8s_cni_node_api"
	ConnTypeK8sCniPodAPI          = "k8s_cni_pod_api"
)

// Exposed reports whether Forward lists the type to end users without for=ui, which is what its own rule isExposed() says:
// PRODUCTION and TECH_PREVIEW only. An unknown or empty status is not exposed.
func (c ConnTypeInfo) Exposed() bool {
	return c.Status == ConnTypeStatusProduction || c.Status == ConnTypeStatusTechPreview
}

// ConnTypesExposed lists only the exposed types (GET /api/deviceConnTypes with no for=, ClientController.getDeviceTypes). This is
// the NARROWER list: ConnTypes (for=ui) already includes every type that can be a classic device, INTERNAL and UNDER_DEVELOPMENT
// ones too, so use ConnTypes to learn about a hidden type and this to see what the product offers. Sorted by ID. Preview.
func (s *ClassicDevicesService) ConnTypesExposed(ctx context.Context) ([]ConnTypeInfo, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/deviceConnTypes", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "ClassicDevices.ConnTypesExposed")
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

// ClassicDeviceWithCredential is what CreateWithAPIKey made. Credential is set as soon as the credential exists, so when the device
// create fails the caller still holds the id of the credential left behind and can delete it or retry with it.
type ClassicDeviceWithCredential struct {
	Credential *HTTPCredential
	Device     *ClassicDevice
}

// CreateWithAPIKey creates an API_KEY HTTP credential and then a classic device that uses it: the recipe for the types whose HTTP
// credential is an API key (httpCredentialType API_KEY, such as k8s_cni_control_plane_api, where the key is the cluster's bearer
// token). The credential's Type is forced to API_KEY (it takes no username) and the device's HTTPCredentialID is the new
// credential's id; set neither yourself. It is not atomic: if the device create fails the credential stays, and the result still
// carries it with the error. Preview: composed from CreateHTTP and Create, and untested against a live Forward.
func (s *ClassicDevicesService) CreateWithAPIKey(ctx context.Context, networkID string, credential HTTPCredentialRequest, device ClassicDeviceRequest) (*ClassicDeviceWithCredential, *Response, error) {
	if credential.Type != "" && !strings.EqualFold(credential.Type, "API_KEY") {
		return nil, nil, fmt.Errorf("forward: CreateWithAPIKey makes an API_KEY credential, not %q", credential.Type)
	}
	if device.HTTPCredentialID != "" {
		return nil, nil, errors.New("forward: CreateWithAPIKey sets the device's HTTP credential itself; leave HTTPCredentialID empty")
	}
	if err := validateClassicDevice(device); err != nil {
		return nil, nil, err
	}
	credential.Type = "API_KEY"
	created, resp, err := s.client.Credentials.CreateHTTP(ctx, networkID, credential)
	if err != nil {
		return nil, resp, err
	}
	out := &ClassicDeviceWithCredential{Credential: created}
	device.HTTPCredentialID = created.ID
	out.Device, resp, err = s.Create(ctx, networkID, device)
	return out, resp, err
}
