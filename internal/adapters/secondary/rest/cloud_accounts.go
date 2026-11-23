package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// CloudAccountsService manages cloud collection sources.
//
// Preview: cloud account models and routes evolve independently across
// Forward versions and are not part of the published OpenAPI set.
type CloudAccounts service

func (s *CloudAccounts) List(ctx context.Context, networkID string) ([]ports.CloudAccount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := cloudAccountsPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	var accounts []ports.CloudAccount
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &accounts)
	return accounts, resp, err
}

func (s *CloudAccounts) Create(ctx context.Context, networkID string, request ports.CloudAccountRequest) (*ports.CloudAccount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := cloudAccountsPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}
	account := new(ports.CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}

func (s *CloudAccounts) Update(ctx context.Context, networkID, name string, request ports.CloudAccountRequest) (*ports.CloudAccount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := cloudAccountPath(s.client, networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, request)
	if err != nil {
		return nil, nil, err
	}
	account := new(ports.CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}

func (s *CloudAccounts) UpdateCredential(ctx context.Context, networkID, name string, request ports.CloudAccountCredentialRequest) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := cloudAccountPath(s.client, networkID, name)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"/credential", request)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *CloudAccounts) Delete(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	path, err := cloudAccountPath(s.client, networkID, name)
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
func (s *CloudAccounts) Test(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	base, err := cloudAccountsPath(s.client, networkID)
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

func (s *CloudAccounts) AWSAssumeRoleExternalID(ctx context.Context, networkID string) (string, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return "", nil, err
	}
	path, err := cloudAccountsPath(s.client, networkID)
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

// cloudAccountsPath resolves an empty networkID from the client's bound network, like the other network-scoped services.
func cloudAccountsPath(client *Client, networkID string) (string, error) {
	networkID, err := client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/cloudAccounts", nil
}

func cloudAccountPath(client *Client, networkID, name string) (string, error) {
	path, err := cloudAccountsPath(client, networkID)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		return "", errors.New("forward: cloud account name is required")
	}
	return path + "/" + url.PathEscape(name), nil
}

// Patch updates part of a cloud account and returns it. PATCH /api/networks/{networkId}/cloudAccounts/{accountName}.
// Only the properties set on patch are sent; see CloudAccountPatch.
func (s *CloudAccounts) Patch(ctx context.Context, networkID, name string, patch ports.CloudAccountPatch) (*ports.CloudAccount, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(patch.Type) == "" {
		return nil, nil, errors.New("forward: cloud account patch type is required")
	}
	path, err := cloudAccountPath(s.client, networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudAccounts.Patch")
	account := new(ports.CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}
