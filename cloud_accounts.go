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

// CloudAccountRequest is what Forward accepts when creating or updating a
// cloud collection source. It was map[string]any, which meant a misspelled
// key was a silent no-op against the API and every caller had to know the
// wire format this SDK exists to hide.
//
// The provider decides which fields apply, and the shapes are NOT symmetric
// with the CloudAccount that comes back:
//
//   - Regions on the REQUEST is {"us-east-1": 0} -- a set encoded as a map to
//     zero. The RESPONSE returns {"us-east-1": {"testInstant": ...}}. Do not
//     assume one can be fed back as the other.
//   - Username/Password are the access key and secret for AWS, IBM and GCP.
//   - ClientID, Tenant, Environment and SubscriptionIDs are Azure's, and
//     Environment is the literal "AZURE".
//   - DiscoveredSubscriptionIDs and TestInstants are Forward-populated; they
//     are here so a read-modify-write round-trips instead of erasing them.
//
// Extra carries anything a newer Forward build accepts that this struct does
// not name yet, so a version-specific property is still reachable without
// giving up typing for every caller.
type CloudAccountRequest struct {
	Type          string         `json:"type"`
	Name          string         `json:"name"`
	Collect       bool           `json:"collect"`
	ProxyServerID string         `json:"proxyServerId,omitempty"`
	Regions       map[string]int `json:"regions,omitempty"`

	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	ClientID                  string         `json:"clientId,omitempty"`
	Tenant                    string         `json:"tenant,omitempty"`
	Environment               string         `json:"environment,omitempty"`
	SubscriptionIDs           []string       `json:"subscriptionIds,omitempty"`
	DiscoveredSubscriptionIDs []string       `json:"discoveredSubscriptionIds,omitempty"`
	TestInstants              map[string]int `json:"testInstants,omitempty"`

	Extra map[string]any `json:"-"`
}

// MarshalJSON folds Extra in beside the named fields. A named field always
// wins: Extra is an escape hatch for properties this struct does not know,
// never a way to quietly override one it does.
func (r CloudAccountRequest) MarshalJSON() ([]byte, error) {
	type plain CloudAccountRequest
	base, err := json.Marshal(plain(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		if _, taken := merged[k]; taken {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		merged[k] = raw
	}
	return json.Marshal(merged)
}

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
