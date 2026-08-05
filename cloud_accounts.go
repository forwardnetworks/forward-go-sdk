package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// CloudAccountsService manages cloud collection sources.
//
// Preview: cloud account models and routes evolve independently across
// Forward versions and are not part of the published OpenAPI set.
type CloudAccountsService service

type CloudAccount struct {
	Type                          string                     `json:"type"`
	Name                          string                     `json:"name"`
	Collect                       bool                       `json:"collect"`
	ProxyServerID                 string                     `json:"proxyServerId,omitempty"`
	Regions                       map[string]Region          `json:"regions,omitempty"`
	RegionToProxyServerID         map[string]string          `json:"regionToProxyServerId,omitempty"`
	AssumeRoleInfos               []AWSAssumeRoleInfo        `json:"assumeRoleInfos,omitempty"`
	UseForwardAccountToAssumeRole *bool                      `json:"useForwardAccountToAssumeRole,omitempty"`
	Concurrency                   *int64                     `json:"concurrency,omitempty"`
	ConnectionTimeoutSeconds      *int64                     `json:"connectionTimeoutSeconds,omitempty"`
	RequestTimeoutSeconds         *int64                     `json:"requestTimeoutSeconds,omitempty"`
	Raw                           map[string]json.RawMessage `json:"-"`
}

func (a *CloudAccount) UnmarshalJSON(data []byte) error {
	type plain CloudAccount
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = CloudAccount(value)
	a.Raw = raw
	return nil
}

type Region struct {
	TestInstant int64 `json:"testInstant,omitempty"`
}

type AWSAssumeRoleInfo struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName,omitempty"`
	RoleARN     string `json:"roleArn,omitempty"`
	ExternalID  string `json:"externalId,omitempty"`
	Enabled     bool   `json:"enabled"`
	ErrorMsg    string `json:"errorMsg,omitempty"`
}

// CloudAccountRequest contains the common AWS fields. Fields carries
// provider- and version-specific properties for Azure, GCP, and newer sources.
type CloudAccountRequest map[string]any

func (s *CloudAccountsService) List(ctx context.Context, networkID string) ([]CloudAccount, *Response, error) {
	path, err := cloudAccountsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	var accounts []CloudAccount
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &accounts)
	return accounts, resp, err
}

func (s *CloudAccountsService) Create(ctx context.Context, networkID string, request CloudAccountRequest) (*CloudAccount, *Response, error) {
	path, err := cloudAccountsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}
	account := new(CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}

func (s *CloudAccountsService) Update(ctx context.Context, networkID, name string, request CloudAccountRequest) (*CloudAccount, *Response, error) {
	path, err := cloudAccountPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, request)
	if err != nil {
		return nil, nil, err
	}
	account := new(CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}

func (s *CloudAccountsService) UpdateCredential(ctx context.Context, networkID, name string, request map[string]any) (*Response, error) {
	path, err := cloudAccountPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/credential", request)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *CloudAccountsService) Delete(ctx context.Context, networkID, name string) (*Response, error) {
	path, err := cloudAccountPath(networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// Test triggers the account connectivity probe.
func (s *CloudAccountsService) Test(ctx context.Context, networkID, name string) (*Response, error) {
	base, err := cloudAccountsPath(networkID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("forward: cloud account name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, base+"/"+url.PathEscape(name)+"/test", nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CloudAccounts.Test")
	return s.client.Do(req, nil)
}

func (s *CloudAccountsService) AWSAssumeRoleExternalID(ctx context.Context, networkID string) (string, *Response, error) {
	path, err := cloudAccountsPath(networkID)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		ExternalID string `json:"externalId"`
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/aws/assumeRole/externalId", nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := s.client.Do(req, &result)
	return result.ExternalID, resp, err
}

func cloudAccountsPath(networkID string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/cloudAccounts", nil
}

func cloudAccountPath(networkID, name string) (string, error) {
	path, err := cloudAccountsPath(networkID)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: cloud account name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}
