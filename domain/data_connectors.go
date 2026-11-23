package domain

import (
	"encoding/json"
	"time"
)

// DataConnector is one connector. CredentialID names an HTTP credential (never
// a secret); CredentialID, ProxyServerID and CollectorID are empty when
// unset, and an empty CollectorID means Forward picks an online Collector.
// Collect nil means true. TestResult and Status are present only when asked
// for (DataConnectorReadOptions), and even then only when Forward has one.
type DataConnector struct {
	Name                 string                   `json:"name"`
	BaseURL              string                   `json:"baseUrl"`
	DisableSSLValidation bool                     `json:"disableSslValidation,omitempty"`
	CredentialID         string                   `json:"credentialId,omitempty"`
	ProxyServerID        string                   `json:"proxyServerId,omitempty"`
	ExtraHeaders         map[string]string        `json:"extraHeaders,omitempty"`
	Endpoints            []HTTPEndpoint           `json:"endpoints"`
	Collect              *bool                    `json:"collect,omitempty"`
	CollectorID          string                   `json:"collectorId,omitempty"`
	TestResult           *DataConnectorTestResult `json:"testResult,omitempty"`
	Status               *DataConnectorStatus     `json:"status,omitempty"`
	CreatedAt            string                   `json:"createdAt,omitempty"`
	CreatedBy            string                   `json:"createdBy,omitempty"`
	CreatedByID          Identifier               `json:"createdById,omitempty"`
	UpdatedAt            string                   `json:"updatedAt,omitempty"`
	UpdatedBy            string                   `json:"updatedBy,omitempty"`
	UpdatedByID          Identifier               `json:"updatedById,omitempty"`
}

// Collects reports whether the connector is collected (Forward's default is
// true).
func (c DataConnector) Collects() bool { return c.Collect == nil || *c.Collect }

// DataConnectorTestResult is a connectivity test's outcome. Error is
// Forward's DeviceCollectionError name, empty when every endpoint answered
// (Forward never sends "NONE" here); ErrorDesc explains it. Times are
// ISO-8601.
type DataConnectorTestResult struct {
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
	Error     string `json:"error,omitempty"`
	ErrorDesc string `json:"errorDesc,omitempty"`
}

// DataConnectorStatus is how the connector's collection went in the network's
// latest processed snapshot (DataConnectors.SnapshotID). Error is empty on
// success. A nil Status, when asked for, means that snapshot has no record
// of the connector: never collected, or excluded.
type DataConnectorStatus struct {
	Error string `json:"error,omitempty"`
}

// DataConnectors is a network's connectors. SnapshotID is the latest
// processed snapshot every Status describes; it is set only when Status was
// asked for and such a snapshot exists.
type DataConnectors struct {
	Connectors []DataConnector `json:"connectors"`
	SnapshotID string          `json:"snapshotId,omitempty"`
}

// DataConnectorReadOptions adds the optional parts to List and Get
// (?with=status,testResult); both are left out by default.
type DataConnectorReadOptions struct {
	Status     bool
	TestResult bool
}

// NewDataConnector is a connector to add (NewHttpDataConnector). Name must be
// unique, ignoring case, among the network's collection sources; BaseURL is
// scheme, host and optional port with no trailing slash; Endpoints may not be
// empty. ExtraHeaders must not carry secrets -- authenticate with
// CredentialID. Collect nil means true.
type NewDataConnector struct {
	Name                 string            `json:"name"`
	BaseURL              string            `json:"baseUrl"`
	DisableSSLValidation *bool             `json:"disableSslValidation,omitempty"`
	CredentialID         string            `json:"credentialId,omitempty"`
	ProxyServerID        string            `json:"proxyServerId,omitempty"`
	ExtraHeaders         map[string]string `json:"extraHeaders,omitempty"`
	Endpoints            []HTTPEndpoint    `json:"endpoints"`
	Collect              *bool             `json:"collect,omitempty"`
	CollectorID          string            `json:"collectorId,omitempty"`
}

// DataConnectorPatch changes some of a connector (HttpDataConnectorPatch);
// nil fields are left alone. A Name renames it. ExtraHeaders and Endpoints
// replace the whole map or list. CredentialID, ProxyServerID and CollectorID
// are tri-state: nil leaves them, Ptr("") clears them (JSON null: no
// credential, no proxy, Forward picks the Collector), and Ptr(id) sets them.
type DataConnectorPatch struct {
	Name                 *string
	BaseURL              *string
	DisableSSLValidation *bool
	CredentialID         *string
	ProxyServerID        *string
	ExtraHeaders         map[string]string
	Endpoints            []HTTPEndpoint
	Collect              *bool
	CollectorID          *string
}

// MarshalJSON writes only the stated fields, and an explicit null for a
// cleared reference.
func (p DataConnectorPatch) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	setValue := func(key string, value *string) {
		if value != nil {
			body[key] = *value
		}
	}
	setNullable := func(key string, value *string) {
		if value == nil {
			return
		}
		if *value == "" {
			body[key] = nil
			return
		}
		body[key] = *value
	}
	setValue("name", p.Name)
	setValue("baseUrl", p.BaseURL)
	if p.DisableSSLValidation != nil {
		body["disableSslValidation"] = *p.DisableSSLValidation
	}
	setNullable("credentialId", p.CredentialID)
	setNullable("proxyServerId", p.ProxyServerID)
	if p.ExtraHeaders != nil {
		body["extraHeaders"] = p.ExtraHeaders
	}
	if p.Endpoints != nil {
		body["endpoints"] = p.Endpoints
	}
	if p.Collect != nil {
		body["collect"] = *p.Collect
	}
	setNullable("collectorId", p.CollectorID)
	return json.Marshal(body)
}

// DataConnectorTestTimeout is the HTTP timeout Test needs: Forward answers
// with a DeferredResult that blocks until the Collector finishes, with a
// 60-second server-side ceiling (DataConnectorTestResultTaskHandler.TIMEOUT).
const DataConnectorTestTimeout = 90 * time.Second
