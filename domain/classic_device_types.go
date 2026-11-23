package domain

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

// ClassicDeviceWithCredential is what CreateWithAPIKey made. Credential is set as soon as the credential exists, so when the device
// create fails the caller still holds the id of the credential left behind and can delete it or retry with it.
type ClassicDeviceWithCredential struct {
	Credential *HTTPCredential
	Device     *ClassicDevice
}
