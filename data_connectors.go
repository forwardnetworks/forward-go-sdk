package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DataConnectorsService manages a network's data connectors: HTTP services a
// Collector fetches from on every collection, each endpoint's response stored
// in the snapshot under its own name. Unlike a data file (DataFilesService),
// which an org uploads once, a connector is a per-network collection source.
// The published Data Connectors API (fwd/api/apis/data-connectors.yaml,
// DataConnectorController), served on primary 15398425a69 and stable
// 67e89c87124.
type DataConnectorsService service

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

func (o DataConnectorReadOptions) query() string {
	var with []string
	if o.Status {
		with = append(with, "status")
	}
	if o.TestResult {
		with = append(with, "testResult")
	}
	if len(with) == 0 {
		return ""
	}
	return "?" + url.Values{"with": []string{strings.Join(with, ",")}}.Encode()
}

// List returns the network's connectors. GET .../data-connectors
// (getDataConnectors; VIEW_COLLECTION_SOURCES).
func (s *DataConnectorsService) List(ctx context.Context, networkID string, options DataConnectorReadOptions) (*DataConnectors, *Response, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+options.query(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.List")
	out := new(DataConnectors)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Get returns one connector, or (nil, nil) when it does not exist. GET
// .../data-connectors/{name} (getDataConnector).
func (s *DataConnectorsService) Get(ctx context.Context, networkID, name string, options DataConnectorReadOptions) (*DataConnector, *Response, error) {
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+options.query(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Get")
	out := new(DataConnector)
	resp, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
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

// Add adds a connector and returns it. POST .../data-connectors
// (addDataConnector; MANAGE_COLLECTION_SOURCES; 201).
func (s *DataConnectorsService) Add(ctx context.Context, networkID string, connector NewDataConnector) (*DataConnector, *Response, error) {
	if strings.TrimSpace(connector.Name) == "" || strings.TrimSpace(connector.BaseURL) == "" {
		return nil, nil, errors.New("forward: data connector name and base URL are required")
	}
	if len(connector.Endpoints) == 0 {
		return nil, nil, errors.New("forward: a data connector needs at least one endpoint")
	}
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, connector)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Add")
	out := new(DataConnector)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
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

// Update changes the stated parts of a connector and returns it. PATCH
// .../data-connectors/{name} (updateDataConnector; MANAGE_COLLECTION_SOURCES).
// An empty patch is refused, as is an empty Endpoints list.
func (s *DataConnectorsService) Update(ctx context.Context, networkID, name string, patch DataConnectorPatch) (*DataConnector, *Response, error) {
	if patch.Endpoints != nil && len(patch.Endpoints) == 0 {
		return nil, nil, errors.New("forward: a data connector needs at least one endpoint")
	}
	body, err := patch.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	if string(body) == "{}" {
		return nil, nil, errors.New("forward: a data connector patch must change something")
	}
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Update")
	out := new(DataConnector)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Delete removes a connector. A 404 is success. DELETE
// .../data-connectors/{name} (deleteDataConnector; DELETE_COLLECTION_SOURCES).
func (s *DataConnectorsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "DataConnectors.Delete")
	resp, err := s.client.Do(req, nil)
	if isGone(err) {
		return resp, nil
	}
	return resp, err
}

// DataConnectorTestTimeout is the HTTP timeout Test needs: Forward answers
// with a DeferredResult that blocks until the Collector finishes, with a
// 60-second server-side ceiling (DataConnectorTestResultTaskHandler.TIMEOUT).
const DataConnectorTestTimeout = 90 * time.Second

// Test runs a connectivity test of the stored connector from its Collector
// and returns the result, which Forward also stores as the connector's
// TestResult. The call blocks until the test ends, raising a shorter client
// timeout to DataConnectorTestTimeout (the ctx deadline still applies);
// a failed test is a result with Error set, not an error. POST
// .../data-connectors/{name}?action=test (testDataConnector;
// TEST_COLLECTION_SOURCES).
func (s *DataConnectorsService) Test(ctx context.Context, networkID, name string) (*DataConnectorTestResult, *Response, error) {
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=test", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Test")
	out := new(DataConnectorTestResult)
	// Only ever lengthen the client's timeout: an unlimited or longer one stays.
	var timeout time.Duration
	if hc := s.client.httpClient; hc != nil && hc.Timeout > 0 && hc.Timeout < DataConnectorTestTimeout {
		timeout = DataConnectorTestTimeout
	}
	resp, err := s.client.doWithTimeout(req, out, false, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *DataConnectorsService) collectionPath(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/data-connectors", nil
}

func (s *DataConnectorsService) connectorPath(networkID, name string) (string, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: data connector name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}
