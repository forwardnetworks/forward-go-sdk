package domain

type InfobloxInstance struct {
	ID        Identifier `json:"id,omitempty"`
	Name      string     `json:"name"`
	IPAddress string     `json:"ipAddress"`
	Username  string     `json:"username,omitempty"`
}

type InfobloxInstanceRequest struct {
	Name      string `json:"name"`
	IPAddress string `json:"ipAddress"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type Rapid7Source struct {
	ID                   Identifier `json:"id,omitempty"`
	Name                 string     `json:"name"`
	Type                 string     `json:"type,omitempty"`
	BaseURL              string     `json:"baseUrl,omitempty"`
	CredentialID         Identifier `json:"credentialId,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	ReportNames          []string   `json:"reportNames,omitempty"`
}

type Rapid7SourceRequest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	DisableSSLValidation bool       `json:"disableSslValidation"`
	CredentialID         Identifier `json:"credentialId"`
	CollectionDisabled   bool       `json:"collectionDisabled"`
	ReportNames          []string   `json:"reportNames,omitempty"`
}

// ServiceNowIntegrationSettings is the org's ServiceNow incident connection (Forward's ServiceNowSettings). Forward never returns a secret:
// Password and Token come back as an empty string when one is stored and are absent otherwise, so a non-nil pointer means "set".
// AutoCreateImpact and AutoCreateUrgency are the numeric strings "1" (high), "2" and "3" (ServiceNowIncidentLevelHigh and so on).
type ServiceNowIntegrationSettings struct {
	InstanceURL       string  `json:"instanceUrl,omitempty"`
	Certificate       string  `json:"certificate,omitempty"`
	Username          string  `json:"username,omitempty"`
	Password          *string `json:"password,omitempty"`
	Token             *string `json:"token,omitempty"`
	Enabled           bool    `json:"enabled,omitempty"`
	AutoCreate        bool    `json:"autoCreate,omitempty"`
	AutoCreateImpact  string  `json:"autoCreateImpact,omitempty"`
	AutoCreateUrgency string  `json:"autoCreateUrgency,omitempty"`
	AutoUpdate        bool    `json:"autoUpdate,omitempty"`
}

// ServiceNowIntegrationRequest states every field of the connection, so a PATCH with it sets all the booleans, including the ones left
// false. Authenticate with Username and Password or with Token, not both (Forward answers 400): the unused ones are left out of the body.
// Certificate (a PEM CA certificate to trust) is accepted on premises only. InstanceURL is refused (400) once a connection exists. A level
// left empty is left out, so Forward keeps the stored one (LOW for a new connection); an empty level is a 400. For a change to a few fields use ServiceNowSettingsUpdate.
type ServiceNowIntegrationRequest struct {
	InstanceURL       string `json:"instanceUrl,omitempty"`
	Certificate       string `json:"certificate,omitempty"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password,omitempty"`
	Token             string `json:"token,omitempty"`
	Enabled           bool   `json:"enabled"`
	AutoCreate        bool   `json:"autoCreate"`
	AutoCreateImpact  string `json:"autoCreateImpact,omitempty"`
	AutoCreateUrgency string `json:"autoCreateUrgency,omitempty"`
	AutoUpdate        bool   `json:"autoUpdate"`
}

// ServiceNowSettingsUpdate is a partial change to the ServiceNow connection (Forward's UpdateServiceNowSettings): a nil field is left as it
// is. Setting a Token clears the stored username and password and the other way round; an empty Certificate removes the stored one.
// InstanceURL can only be given when no connection exists yet (BUG-14226).
type ServiceNowSettingsUpdate struct {
	InstanceURL       *string `json:"instanceUrl,omitempty"`
	Certificate       *string `json:"certificate,omitempty"`
	Username          *string `json:"username,omitempty"`
	Password          *string `json:"password,omitempty"`
	Token             *string `json:"token,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
	AutoCreate        *bool   `json:"autoCreate,omitempty"`
	AutoUpdate        *bool   `json:"autoUpdate,omitempty"`
	AutoCreateImpact  *string `json:"autoCreateImpact,omitempty"`
	AutoCreateUrgency *string `json:"autoCreateUrgency,omitempty"`
}

// ServiceNowConnectionDetails is how Forward reaches a ServiceNow instance for the CMDB (Forward's ServiceNowCmdbConnectionDetails).
// Forward returns every secret as an empty string, and an empty Password, ClientSecret or proxy password in a request means "keep the stored
// one" -- but only while the instance URL and username (or client ID, or proxy host, port and username) are unchanged, so changing the
// target means sending its secrets again (master fe0daf63c899, commit 626a2e9c8b2e; older builds echoed the secrets back). Forward compares
// the URLs as java.net.URI values, exactly: send InstanceURL (and the proxy Host) back as read, because "https://host" and "https://host/"
// differ and the stored secret is then not reused (400 "'password' is required").
type ServiceNowConnectionDetails struct {
	InstanceURL  string               `json:"instanceUrl"`
	Certificate  string               `json:"certificate,omitempty"`
	Username     string               `json:"username"`
	Password     string               `json:"password"`
	ClientID     string               `json:"clientId,omitempty"`
	ClientSecret string               `json:"clientSecret,omitempty"`
	ProxyConfig  *ServiceNowCMDBProxy `json:"proxyConfig,omitempty"`
}

// ServiceNowCMDBProxy is the HTTP proxy the CMDB connection goes through (Forward's ServiceNowCmdbProxyConfig). Username is required when
// Password or Realm is set.
type ServiceNowCMDBProxy struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Realm    string `json:"realm,omitempty"`
}

type ServiceNowCMDBField struct {
	Label     string `json:"label"`
	Element   string `json:"element"`
	DataType  string `json:"dataType"`
	Mandatory bool   `json:"mandatory"`
	MaxLength int    `json:"maxLength,omitempty"`
	Reference string `json:"reference,omitempty"`
}

type ServiceNowCMDBFieldRelation struct {
	CMDBField       ServiceNowCMDBField `json:"cmdbField"`
	QueryColumnName string              `json:"queryColumnName,omitempty"`
}

type ServiceNowCMDBSchema struct {
	Name   string                `json:"name"`
	Fields []ServiceNowCMDBField `json:"fields"`
}

type ServiceNowCMDBSchemaRequest struct {
	ConnectionDetails ServiceNowConnectionDetails `json:"connectionDetails"`
	ClassNames        []string                    `json:"classNames"`
}

type ServiceNowCMDBMapping struct {
	Name           string                        `json:"name"`
	QueryID        Identifier                    `json:"queryId"`
	FieldRelations []ServiceNowCMDBFieldRelation `json:"fieldRelations"`
}

type ServiceNowCMDBConfiguration struct {
	Settings   ServiceNowCMDBSettings   `json:"settings"`
	Mappings   []ServiceNowCMDBMapping  `json:"mappings"`
	SyncConfig ServiceNowCMDBSyncConfig `json:"syncConfig"`
	Enabled    bool                     `json:"enabled"`
}

type ServiceNowCMDBSettings struct {
	ConnectionName    string                      `json:"connectionName"`
	ConnectionDetails ServiceNowConnectionDetails `json:"connectionDetails"`
	SourceName        string                      `json:"sourceName"`
}

type ServiceNowCMDBSyncConfig struct {
	NetworkIDs []string `json:"networkIds"`
}

// InfobloxInstanceError is why an Infoblox instance failed its connection test; Type is Forward's InfobloxInstanceError.Type (for example
// CONNECTION_FAILED).
type InfobloxInstanceError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// InfobloxInstanceStatus is one instance's connection status: Error is nil when the instance answered.
type InfobloxInstanceStatus struct {
	ID    Identifier             `json:"id"`
	Error *InfobloxInstanceError `json:"error,omitempty"`
}

// InfobloxInstanceUpdate changes part of an Infoblox instance. Forward's InfobloxInstanceUpdate carries only these
// fields: the address of an instance cannot be edited. Password is write-only.
type InfobloxInstanceUpdate struct {
	Name     *string `json:"name,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}
