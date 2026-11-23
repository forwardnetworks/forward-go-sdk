package domain

// SaseSetupTypeZscaler is the only setup type Forward has.
const SaseSetupTypeZscaler = "ZSCALER"

// AppConnectorCollectionType says how Forward collects an App Connector.
const (
	// AppConnectorCollectionSSH collects the connector's CLI over SSH; Port
	// and CredentialID are then expected.
	AppConnectorCollectionSSH = "SSH"
	// AppConnectorCollectionManual records the connector without probing it
	// (assumed always reachable); Forward drops Port for it.
	AppConnectorCollectionManual = "MANUAL"
)

// ScimConfig is a SCIM endpoint whose users and groups Forward reads. The
// credential must be an API_KEY HTTP credential (Forward refuses others when
// it validates the body).
type ScimConfig struct {
	// URL is absolute http(s), shaped .../scim/<cloudId>/<idpId>/v<N>.
	URL string `json:"url"`
	// HTTPCredID is the id of an API_KEY HTTP credential.
	HTTPCredID string `json:"httpCredId"`
}

// ConnectivityResult is the outcome of the latest test of a setup or one App
// Connector. Error is Forward's ConnectivityTestError constant name, for
// example AUTHENTICATION_FAILED or PING_FAILED, and empty on success. It is
// an enum name, not a message; it is kept verbatim.
type ConnectivityResult struct {
	// SavedAt is when the result was stored (RFC 3339).
	SavedAt string `json:"savedAt,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DiscoveredAppConnector is a connector the ZPA API reported. Latitude and
// Longitude are both set or both absent; LocationName is ZPA's own.
type DiscoveredAppConnector struct {
	ConnectorID  string   `json:"connectorId"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	LocationName string   `json:"locationName,omitempty"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
}

// ZscalerTestResult is the latest tenant-level test. DiscoveredConnectors is
// only reported when the test succeeded and found any.
type ZscalerTestResult struct {
	ConnectivityResult
	DiscoveredConnectors []DiscoveredAppConnector `json:"discoveredConnectors,omitempty"`
}

// AppConnectorSetup is one App Connector inside a setup.
type AppConnectorSetup struct {
	// ConnectorID is ZPA's id for the connector. Required.
	ConnectorID string `json:"connectorId"`
	// Name becomes the device name; Forward lower-cases it. Required.
	Name string `json:"name"`
	// Host is a hostname or IP. Required.
	Host string `json:"host"`
	// CollectionType is AppConnectorCollectionSSH or AppConnectorCollectionManual. Required.
	CollectionType string `json:"collectionType"`
	// Port is 1-65535 for SSH; Forward discards it for MANUAL.
	Port *int `json:"port,omitempty"`
	// CredentialID is a CLI credential id (SSH).
	CredentialID string `json:"credentialId,omitempty"`
	JumpServerID string `json:"jumpServerId,omitempty"`
	LocationID   string `json:"locationId,omitempty"`
	// Collect: nil reads as true on the server; false parks the connector.
	Collect *bool `json:"collect,omitempty"`
	// TestResult is read-only on Create (cleared). Patch replaces the whole
	// connector list and stores each TestResult as sent, so round-trip the
	// connectors from Get when patching or the saved results are lost.
	TestResult *ConnectivityResult `json:"testResult,omitempty"`
}

// ZscalerSetup is a Zscaler SASE setup, as sent to Create and as read back.
type ZscalerSetup struct {
	// Type is always SaseSetupTypeZscaler; the SDK fills it on send.
	Type string `json:"type"`
	// Name is lower-cased by Forward. Required.
	Name string `json:"name"`
	// Collect: nil participates in collections; false parks the setup.
	Collect *bool `json:"collect,omitempty"`
	// CollectorID is the preferred collector. Forward writes it as "C<n>" and
	// reads either "C<n>" or "<n>". Empty means the network's default.
	CollectorID Identifier `json:"collectorId,omitempty"`
	// BaseURL is the ZPA API base; absolute http or https; only its host is
	// kept. Required.
	BaseURL string `json:"baseUrl"`
	// HTTPCredID is the id of the HTTP credential used for ZPA signin. Forward's
	// device type for it declares credential type LOGIN (client id as the
	// username, client secret as the password).
	HTTPCredID string `json:"httpCredId,omitempty"`
	// AppConnectorSetups are the connectors; empty is allowed.
	AppConnectorSetups []AppConnectorSetup `json:"appConnectorSetups"`
	// ScimConfigs are the SCIM endpoints; empty is allowed.
	ScimConfigs []ScimConfig `json:"scimConfigs,omitempty"`
	// TestResult and Attribution are read-only; Create clears them.
	TestResult  *ZscalerTestResult `json:"testResult,omitempty"`
	Attribution *SaseAttribution   `json:"attribution,omitempty"`
}

// SaseAttribution is who created or last changed a setup (AttributionWithNames);
// the names are absent when the user was deleted, and every field may be.
type SaseAttribution struct {
	CreatedBy   string `json:"createdBy,omitempty"`
	CreatedByID string `json:"createdById,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedBy   string `json:"updatedBy,omitempty"`
	UpdatedByID string `json:"updatedById,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

// ZscalerSetupPatch is the body of PATCH .../sase-setups/{name}. A nil field
// is left alone; the SDK cannot send the JSON null that Forward reads as "clear
// httpCredId / collectorId / collect". Changing Name renames the setup. The slices are replaced
// whole, so send the complete list; a non-nil pointer to an empty slice
// clears it.
type ZscalerSetupPatch struct {
	Name               *string              `json:"name,omitempty"`
	Collect            *bool                `json:"collect,omitempty"`
	CollectorID        *string              `json:"collectorId,omitempty"`
	BaseURL            *string              `json:"baseUrl,omitempty"`
	HTTPCredID         *string              `json:"httpCredId,omitempty"`
	AppConnectorSetups *[]AppConnectorSetup `json:"appConnectorSetups,omitempty"`
	ScimConfigs        *[]ScimConfig        `json:"scimConfigs,omitempty"`
}

// ZscalerConnectivityTest is the body of POST .../sase-setups?action=test: try
// a tenant before saving a setup. The result is not returned; it is saved only
// onto an existing setup.
type ZscalerConnectivityTest struct {
	// ID is the device name the probe runs under. Required.
	ID          string       `json:"id"`
	BaseURL     string       `json:"baseUrl"`
	HTTPCredID  string       `json:"httpCredId,omitempty"`
	ScimConfigs []ScimConfig `json:"scimConfigs,omitempty"`
	CollectorID string       `json:"collectorId,omitempty"`
}

// NewlyDiscoveredConnector is a connector the ZPA API reported that no
// setup's AppConnectorSetups lists yet, with the setup it was found under.
type NewlyDiscoveredConnector struct {
	SetupName string `json:"setupName"`
	DiscoveredAppConnector
}
