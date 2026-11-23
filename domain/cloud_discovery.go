package domain

import (
	"time"
)

// DiscoveredCloudAccount is one account Forward found during collection that
// no cloud setup pins yet (DiscoveredAccount in the appserver).
type DiscoveredCloudAccount struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// CloudDiscoveredEntity is one account, subscription or project a discovery
// found. ID is the account id (AWS), subscription id (Azure) or project id
// (GCP). Name is set for Azure and GCP. Agency, Mission and Roles are set only
// for an AWS C2S Access Portal (CAP) setup, where each (ID, role) pair is one
// value a CloudAccount's AWSC2SAccountInfos entry can name (AccountID = ID,
// AccountRole = a role). ErrorMsg is why Forward could not use this entity.
type CloudDiscoveredEntity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Agency   string   `json:"agency,omitempty"`
	Mission  string   `json:"mission,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	ErrorMsg string   `json:"errorMsg,omitempty"`
}

// CloudDiscovery is the outcome of Discover. Error is a DeviceCollectionError
// name ("NONE" when the run worked); Entities is everything the cloud offered,
// pinned or not.
type CloudDiscovery struct {
	Error    string                  `json:"error"`
	Entities []CloudDiscoveredEntity `json:"discoveredEntities"`
}

// CloudDiscoveryTimeout is the HTTP timeout Discover needs. Forward holds the
// response until the collector finishes, up to its own 20-minute ceiling
// (CloudAccountDiscoveryTaskHandler.DISCOVERY_TIMEOUT); a shorter client
// timeout reads a slow success as a failure.
const CloudDiscoveryTimeout = 21 * time.Minute
