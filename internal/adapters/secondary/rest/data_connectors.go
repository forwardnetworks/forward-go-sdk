package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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
type DataConnectors service

// List returns the network's connectors. GET .../data-connectors
// (getDataConnectors; VIEW_COLLECTION_SOURCES).
func (s *DataConnectors) List(ctx context.Context, networkID string, options ports.DataConnectorReadOptions) (*ports.DataConnectors, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.collectionPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+dataConnectorReadOptionsQuery(options), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.List")
	out := new(ports.DataConnectors)
	resp, err := s.client.doRequired(req, out)
	return out, resp, err
}

// Get returns one connector, or (nil, nil) when it does not exist. GET
// .../data-connectors/{name} (getDataConnector).
func (s *DataConnectors) Get(ctx context.Context, networkID, name string, options ports.DataConnectorReadOptions) (*ports.DataConnector, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+dataConnectorReadOptionsQuery(options), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Get")
	out := new(ports.DataConnector)
	resp, err := s.client.doRequired(req, out)
	if ports.IsStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Add adds a connector and returns it. POST .../data-connectors
// (addDataConnector; MANAGE_COLLECTION_SOURCES; 201).
func (s *DataConnectors) Add(ctx context.Context, networkID string, connector ports.NewDataConnector) (*ports.DataConnector, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.DataConnector)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Update changes the stated parts of a connector and returns it. PATCH
// .../data-connectors/{name} (updateDataConnector; MANAGE_COLLECTION_SOURCES).
// An empty patch is refused, as is an empty Endpoints list.
func (s *DataConnectors) Update(ctx context.Context, networkID, name string, patch ports.DataConnectorPatch) (*ports.DataConnector, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.DataConnector)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Delete removes a connector. A 404 is success. DELETE
// .../data-connectors/{name} (deleteDataConnector; DELETE_COLLECTION_SOURCES).
func (s *DataConnectors) Delete(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return resp, nil
	}
	return resp, err
}

// Test runs a connectivity test of the stored connector from its Collector
// and returns the result, which Forward also stores as the connector's
// TestResult. The call blocks until the test ends, raising a shorter client
// timeout to DataConnectorTestTimeout (the ctx deadline still applies);
// a failed test is a result with Error set, not an error. POST
// .../data-connectors/{name}?action=test (testDataConnector;
// TEST_COLLECTION_SOURCES).
func (s *DataConnectors) Test(ctx context.Context, networkID, name string) (*ports.DataConnectorTestResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.connectorPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"?action=test", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "DataConnectors.Test")
	out := new(ports.DataConnectorTestResult)
	// Only ever lengthen the client's timeout: an unlimited or longer one stays.
	var timeout time.Duration
	if hc := s.client.httpClient; hc != nil && hc.Timeout > 0 && hc.Timeout < ports.DataConnectorTestTimeout {
		timeout = ports.DataConnectorTestTimeout
	}
	resp, err := s.client.doWithTimeout(req, out, false, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

func (s *DataConnectors) collectionPath(networkID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/data-connectors", nil
}

func (s *DataConnectors) connectorPath(networkID, name string) (string, error) {
	path, err := s.collectionPath(networkID)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: data connector name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}

func dataConnectorReadOptionsQuery(o ports.DataConnectorReadOptions) string {
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
