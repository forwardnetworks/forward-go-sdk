package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Type                          string              `json:"type"`
	Name                          string              `json:"name"`
	Collect                       bool                `json:"collect"`
	ProxyServerID                 string              `json:"proxyServerId,omitempty"`
	Regions                       map[string]Region   `json:"regions,omitempty"`
	RegionToProxyServerID         map[string]string   `json:"regionToProxyServerId,omitempty"`
	AssumeRoleInfos               []AWSAssumeRoleInfo `json:"assumeRoleInfos,omitempty"`
	UseForwardAccountToAssumeRole *bool               `json:"useForwardAccountToAssumeRole,omitempty"`
	Concurrency                   *int64              `json:"concurrency,omitempty"`
	ConnectionTimeoutSeconds      *int64              `json:"connectionTimeoutSeconds,omitempty"`
	RequestTimeoutSeconds         *int64              `json:"requestTimeoutSeconds,omitempty"`
	// Raw carries fields this SDK version does not model, so an object read
	// from a newer appserver and written back does not silently lose them.
	Raw map[string]json.RawMessage `json:"-"`
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

// CloudAccountRequest is the create/update payload for a cloud setup.
//
// One struct spans the providers because one endpoint does: Forward
// discriminates on Type and reads the fields that apply to it. Splitting this
// per provider would model a distinction the API does not make, and would put
// the burden of picking the right type on a caller that already states it.
//
// Pointer fields distinguish "not stated" from "stated as zero". An update
// that sets Collect to false and one that leaves collection alone are
// different requests, and a plain bool cannot say which was meant.
type CloudAccountRequest struct {
	// Type is the discriminator: AWS, AZURE, GCP, IBM_CLOUD, ALKIRA.
	Type string `json:"type"`
	Name string `json:"name,omitempty"`

	Collect                  *bool             `json:"collect,omitempty"`
	ProxyServerID            *string           `json:"proxyServerId,omitempty"`
	RegionToProxyServerID    map[string]string `json:"regionToProxyServerId,omitempty"`
	Concurrency              *int64            `json:"concurrency,omitempty"`
	ConnectionTimeoutSeconds *int64            `json:"connectionTimeoutSeconds,omitempty"`
	RequestTimeoutSeconds    *int64            `json:"requestTimeoutSeconds,omitempty"`

	// Regions maps a region name to a last-test timestamp. A create states the
	// regions with zero timestamps; Forward fills them in.
	Regions map[string]int64 `json:"regions,omitempty"`

	// Username and Password are the AWS access key and secret, or the
	// equivalent pair for a provider that authenticates that way.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// AWS assume-role onboarding.
	AssumeRoleInfos               []AWSAssumeRoleInfo `json:"assumeRoleInfos,omitempty"`
	UseForwardAccountToAssumeRole *bool               `json:"useForwardAccountToAssumeRole,omitempty"`

	// Azure service principal.
	//
	// TestInstants is Azure's analogue of Regions: subscription id to a
	// last-test timestamp. Forward requires it on a create, so a request that
	// names subscriptions and omits this is refused outright.
	TestInstants    map[string]int64 `json:"testInstants,omitempty"`
	ClientID        string           `json:"clientId,omitempty"`
	ClientSecret    string           `json:"clientSecret,omitempty"`
	Tenant          string           `json:"tenant,omitempty"`
	Environment     string           `json:"environment,omitempty"`
	SubscriptionIDs []string         `json:"subscriptionIds,omitempty"`
}

// CloudAccountCredentialRequest replaces the stored credential of a setup
// without restating the rest of it.
type CloudAccountCredentialRequest struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Azure rotates a secret rather than a password.
	ClientSecret string `json:"clientSecret,omitempty"`
}

// AWSAssumeRoleExternalID is the external id Forward expects a customer role to
// require, which is what makes the trust policy specific to this instance.
type AWSAssumeRoleExternalIDResponse struct {
	ExternalID string `json:"externalId"`
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

func (s *CloudAccountsService) UpdateCredential(ctx context.Context, networkID, name string, request CloudAccountCredentialRequest) (*Response, error) {
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

// ErrCloudAccountNotFound is returned by Get when the network has no setup of
// that name. A Terraform read distinguishes this from a transport failure --
// the first means the resource is gone and should be removed from state, the
// second means try again -- so it has to be matchable rather than a string.
var ErrCloudAccountNotFound = errors.New("forward: cloud account not found")

// Get returns one cloud setup by name.
//
// Forward exposes no per-name read, so this filters the list. The name is the
// setup's identity for every other call -- update, credential rotation,
// delete -- so a caller holding only a name can still read it back.
func (s *CloudAccountsService) Get(ctx context.Context, networkID, name string) (*CloudAccount, *Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: cloud account name is required")
	}
	accounts, resp, err := s.List(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	for i := range accounts {
		if accounts[i].Name == name {
			return &accounts[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: %s", ErrCloudAccountNotFound, name)
}
