package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CloudAccountsService manages cloud collection sources.
//
// Preview: cloud account models and routes evolve independently across
// Forward versions and are not part of the published OpenAPI set.
type CloudAccountsService service

type CloudAccount struct {
	Type          string            `json:"type"`
	Name          string            `json:"name"`
	Collect       bool              `json:"collect"`
	ProxyServerID string            `json:"proxyServerId,omitempty"`
	Regions       map[string]Region `json:"regions,omitempty"`
	// TestResults is Azure's per-subscription connectivity-test result; AWS,
	// GCP and IBM carry theirs per region in Regions.
	TestResults                   map[string]Region   `json:"testResults,omitempty"`
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

// Region is one region's most recent connectivity-test result, as Forward
// stores it on the account (CloudAccount.TestResult in the appserver). A
// region never tested since its proxy changed comes back as JSON null, which
// decodes to the zero Region: TestInstant 0 and no Error.
//
// TestInstant is epoch milliseconds whatever the build sends: builds before
// fwd 075aa01 ("Serialize cloud test times as ISO-8601", in neither primary
// 15398425a69 nor stable 67e89c87124) send epoch millis, later ones an
// ISO-8601 UTC string such as "2026-10-01T12:34:56.789Z". TestedAt reads it
// as a time.
type Region struct {
	TestInstant int64 `json:"testInstant,omitempty"`
	// Error is Forward's DeviceCollectionError name for the test: "NONE" on
	// success, otherwise the failure (UNKNOWN, AUTHENTICATION_FAILED,
	// PROJECT_VIEW_PERMISSION_MISSING, ...). Empty when never tested.
	Error string `json:"error,omitempty"`
}

// UnmarshalJSON accepts testInstant as epoch milliseconds (a number, or a
// string of digits) or as an ISO-8601 instant.
func (r *Region) UnmarshalJSON(data []byte) error {
	var wire struct {
		TestInstant json.RawMessage `json:"testInstant"`
		Error       string          `json:"error"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	millis, err := decodeEpochMillisOrInstant(wire.TestInstant)
	if err != nil {
		return fmt.Errorf("forward: region testInstant: %w", err)
	}
	*r = Region{TestInstant: millis, Error: wire.Error}
	return nil
}

// TestedAt returns when the region was last tested, and false if never.
func (r Region) TestedAt() (time.Time, bool) {
	if r.TestInstant == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(r.TestInstant).UTC(), true
}

// decodeEpochMillisOrInstant reads a JSON time that Forward has sent both as
// epoch milliseconds and, after Jackson's Instant default, as an ISO-8601
// string. Absent or null is zero.
func decodeEpochMillisOrInstant(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var millis int64
		if err := json.Unmarshal(raw, &millis); err != nil {
			return 0, fmt.Errorf("want epoch milliseconds or an ISO-8601 instant, got %s", raw)
		}
		return millis, nil
	}
	if text = strings.TrimSpace(text); text == "" {
		return 0, nil
	}
	if millis, err := strconv.ParseInt(text, 10, 64); err == nil {
		return millis, nil
	}
	instant, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return 0, fmt.Errorf("want epoch milliseconds or an ISO-8601 instant, got %q", text)
	}
	return instant.UnixMilli(), nil
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

	// Per-region proxies and collection tuning; omitted when unstated.
	RegionToProxyServerID    map[string]string `json:"regionToProxyServerId,omitempty"`
	Concurrency              *int64            `json:"concurrency,omitempty"`
	ConnectionTimeoutSeconds *int64            `json:"connectionTimeoutSeconds,omitempty"`
	RequestTimeoutSeconds    *int64            `json:"requestTimeoutSeconds,omitempty"`

	// AWS assume-role onboarding.
	AssumeRoleInfos               []AWSAssumeRoleInfo `json:"assumeRoleInfos,omitempty"`
	UseForwardAccountToAssumeRole *bool               `json:"useForwardAccountToAssumeRole,omitempty"`

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

// CloudAccountCredentialRequest replaces the stored credential of a setup
// without restating the rest of it (POST .../cloudAccounts/{name}/credential,
// CloudAccountController#updateCloudAccountCredential). The account's update
// (PATCH) body carries no credentials at all, so this is the ONLY way to
// rotate a secret on an existing account.
//
// The `type` discriminator selects the shape Forward reads:
//   - AWS: username (access key id) and password (secret).
//   - AZURE: clientId, tenant (directory) and password (the client secret).
//   - GCP: the service-account key -- clientId, clientEmail, privateKeyId and
//     privateKey (PEM). Forward requires all four together.
//   - IBM_CLOUD (the discriminator is IBM_CLOUD here, not IBM): apiKey.
type CloudAccountCredentialRequest struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	ClientID string `json:"clientId,omitempty"`
	Tenant   string `json:"tenant,omitempty"`

	ClientEmail  string `json:"clientEmail,omitempty"`
	PrivateKeyID string `json:"privateKeyId,omitempty"`
	PrivateKey   string `json:"privateKey,omitempty"`

	APIKey string `json:"apiKey,omitempty"`
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

// CloudAccountPatch is a partial update of a cloud account, with presence semantics: a nil pointer, nil map or nil slice
// pointer is left out of the body, and Forward leaves that property unchanged (UpdateCloudAccountRequest takes every
// property as a JsonProp, absent meaning unchanged). Use it instead of CloudAccountRequest for PATCH, whose Collect
// always serialises and so would switch collection off.
//
// Type is required: Forward picks the update class from it ("AWS", "AZURE", "GCP", "ALKIRA", "IBM_CLOUD"). The AWS
// fields are the ones modelled; Regions maps a region to its connectivity test instant in epoch milliseconds, and
// Forward rejects a null instant and an empty regions map, so an empty Regions is left out rather than sent.
// AssumeRoleInfos is a pointer so that a non-nil empty list is sent (it clears the roles) while nil is omitted; account
// ids in it must be unique. Verified against UpdateCloudAccountRequest and UpdateAwsAccountRequest in the appserver
// (CloudAccountController.updateCloudAccount; MANAGE_COLLECTION_SOURCES).
type CloudAccountPatch struct {
	Type          string  `json:"type"`
	Name          *string `json:"name,omitempty"`
	Collect       *bool   `json:"collect,omitempty"`
	CollectorID   *string `json:"collectorId,omitempty"`
	ProxyServerID *string `json:"proxyServerId,omitempty"`

	Concurrency              *int64 `json:"concurrency,omitempty"`
	ConnectionTimeoutSeconds *int64 `json:"connectionTimeoutSeconds,omitempty"`
	RequestTimeoutSeconds    *int64 `json:"requestTimeoutSeconds,omitempty"`

	Regions               map[string]int64     `json:"regions,omitempty"`
	RegionToProxyServerID map[string]string    `json:"regionToProxyServerId,omitempty"`
	AssumeRoleInfos       *[]AWSAssumeRoleInfo `json:"assumeRoleInfos,omitempty"`
}

// Patch updates part of a cloud account and returns it. PATCH /api/networks/{networkId}/cloudAccounts/{accountName}.
// Only the properties set on patch are sent; see CloudAccountPatch.
func (s *CloudAccountsService) Patch(ctx context.Context, networkID, name string, patch CloudAccountPatch) (*CloudAccount, *Response, error) {
	if strings.TrimSpace(patch.Type) == "" {
		return nil, nil, errors.New("forward: cloud account patch type is required")
	}
	path, err := cloudAccountPath(networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudAccounts.Patch")
	account := new(CloudAccount)
	resp, err := s.client.Do(req, account)
	return account, resp, err
}
