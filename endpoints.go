package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type EndpointsService service

type Endpoint struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port,omitempty"`
	Protocol     string `json:"protocol"`
	CredentialID string `json:"credentialId,omitempty"`
	ProfileID    string `json:"profileId,omitempty"`
	JumpServerID string `json:"jumpServerId,omitempty"`
	FullCollect  bool   `json:"fullCollectionLog,omitempty"`
	LargeRTT     bool   `json:"largeRtt,omitempty"`
	Collect      *bool  `json:"collect,omitempty"`
	Note         string `json:"note,omitempty"`
}

type EndpointPatch struct {
	Host         *string `json:"host,omitempty"`
	Protocol     *string `json:"protocol,omitempty"`
	CredentialID *string `json:"credentialId,omitempty"`
	ProfileID    *string `json:"profileId,omitempty"`
	JumpServerID *string `json:"jumpServerId,omitempty"`
	Collect      *bool   `json:"collect,omitempty"`
}

type EndpointProfile struct {
	ID             Identifier `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	CommandSets    []string   `json:"commandSets"`
	CustomCommands []string   `json:"customCommands"`
}

type EndpointProfileRequest struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	CustomCommands []string `json:"customCommands"`
	CommandSets    []string `json:"commandSets"`
}

type SourceTestStatus struct {
	Name                 string `json:"name,omitempty"`
	SourceKind           string `json:"sourceKind,omitempty"`
	TestResultPresent    bool   `json:"testResultPresent,omitempty"`
	ConnectivityError    string `json:"connectivityError,omitempty"`
	ConnectivityErrorRaw string `json:"connectivityErrorRaw,omitempty"`
	ErrorPhase           string `json:"errorPhase,omitempty"`
	SNMPCollectionStatus string `json:"snmpCollectionStatus,omitempty"`
}

func (s *EndpointsService) List(ctx context.Context, networkID string) ([]Endpoint, *Response, error) {
	path, err := s.networkPath(networkID, "/endpoints")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[Endpoint]{Keys: []string{"items", "endpoints", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *EndpointsService) AddBatch(ctx context.Context, networkID, endpointType string, endpoints []Endpoint) (*Response, error) {
	if len(endpoints) == 0 {
		return nil, nil
	}
	path, err := s.networkPath(networkID, "/endpoints")
	if err != nil {
		return nil, err
	}
	endpointType = strings.TrimSpace(endpointType)
	if endpointType == "" {
		endpointType = "CLI"
	}
	query := url.Values{"action": []string{"addBatch"}, "type": []string{endpointType}}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?"+query.Encode(), endpoints)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.AddBatch")
	return s.client.Do(req, nil)
}

func (s *EndpointsService) Patch(ctx context.Context, networkID, name, endpointType string, patch EndpointPatch) (*Response, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: endpoint name is required")
	}
	path, err := s.networkPath(networkID, "/endpoints/"+url.PathEscape(name))
	if err != nil {
		return nil, err
	}
	endpointType = strings.TrimSpace(endpointType)
	if endpointType == "" {
		endpointType = "CLI"
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path+"?type="+url.QueryEscape(endpointType), patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Endpoints.Patch")
	return s.client.Do(req, nil)
}

func (s *EndpointsService) ListTestStatuses(ctx context.Context, networkID string) ([]SourceTestStatus, *Response, error) {
	path, err := s.networkPath(networkID, "/endpoints?with=testResult")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[SourceTestStatus]{Keys: []string{"endpoints", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ListTestStatuses")
	response, err := s.client.doRequired(req, &result)
	for i := range result.Items {
		result.Items[i].SourceKind = "endpoint"
	}
	return result.Items, response, err
}

func (s *EndpointsService) ListProfiles(ctx context.Context, profileType string) ([]EndpointProfile, *Response, error) {
	path := "/api/endpoint-profiles"
	if profileType = strings.TrimSpace(profileType); profileType != "" {
		path += "?type=" + url.QueryEscape(profileType)
	}
	result := listResponse[EndpointProfile]{Keys: []string{"items", "profiles", "endpointProfiles", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.ListProfiles")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

func (s *EndpointsService) CreateProfile(ctx context.Context, input EndpointProfileRequest) (*EndpointProfile, *Response, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.TrimSpace(input.Type)
	if input.Name == "" || input.Type == "" {
		return nil, nil, errors.New("forward: endpoint profile name and type are required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/endpoint-profiles?type="+url.QueryEscape(input.Type), input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Endpoints.CreateProfile")
	out := new(EndpointProfile)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: endpoint profile create returned no ID")
	}
	return out, response, err
}

func (s *EndpointsService) networkPath(networkID, suffix string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	return "/api/networks/" + url.PathEscape(networkID) + suffix, nil
}
