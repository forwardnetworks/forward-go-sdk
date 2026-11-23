package domain

// Device is a modeled device in a processed snapshot.
type Device struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName,omitempty"`
	SourceName      string   `json:"sourceName,omitempty"`
	Type            string   `json:"type,omitempty"`
	Vendor          string   `json:"vendor,omitempty"`
	Model           string   `json:"model,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	OSVersion       string   `json:"osVersion,omitempty"`
	ManagementIPs   []string `json:"managementIps,omitempty"`
	CollectionError string   `json:"collectionError,omitempty"`
	ProcessingError string   `json:"processingError,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	LocationID      string   `json:"locationId,omitempty"`
	// DuplicateOf and CollectedAt are set when collection failed and an earlier collection's data was reused;
	// Forward compares both in a device diff.
	DuplicateOf string `json:"duplicateOf,omitempty"`
	CollectedAt string `json:"collectedAt,omitempty"`
}

// DeviceFile is one raw file collected from a device.
type DeviceFile struct {
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes,omitempty"`
	Command string `json:"command,omitempty"`
}

// MissingDevice is a device Forward did not model but inferred from its
// modeled neighbors' CDP, LLDP, iBGP or OSPF data
// (com.forwardnetworks.cv.snapshot.MissingDevice). Name is one it could be
// added under; Type is the DeviceConnType it would be modeled as (UNKNOWN when
// undetermined); Vendor is empty when unknown; Neighbors names the modeled
// devices that reported it; DiscoveryMethod is empty when not recorded.
type MissingDevice struct {
	Name            string   `json:"name"`
	IPAddresses     []string `json:"ipAddresses,omitempty"`
	Type            string   `json:"type,omitempty"`
	Vendor          string   `json:"vendor,omitempty"`
	Neighbors       []string `json:"neighbors,omitempty"`
	DiscoveryMethod string   `json:"discoveryMethod,omitempty"`
}
