package domain

type DeviceCollectionStatus struct {
	Name             string `json:"name,omitempty"`
	DeviceName       string `json:"deviceName,omitempty"`
	CollectionStatus string `json:"collectionStatus,omitempty"`
	Status           string `json:"status,omitempty"`
	State            string `json:"state,omitempty"`
	CollectionFailed bool   `json:"collectionFailed,omitempty"`
	CollectionError  string `json:"collectionError,omitempty"`
}
