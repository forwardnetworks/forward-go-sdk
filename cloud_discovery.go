package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DiscoveredCloudAccount is one account Forward found during collection that
// no cloud setup pins yet (DiscoveredAccount in the appserver).
type DiscoveredCloudAccount struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// NewlyDiscovered lists, per cloud setup name, the accounts collection found
// that the setup does not collect from yet. GET
// /api/networks/{networkId}/cloudAccounts?view=newlyDiscovered
// (CloudAccountController.getNewlyDiscoveredAccounts, Preview;
// VIEW_COLLECTION_SOURCES). It reads what the last snapshot recorded; it does
// not ask the cloud. It covers only multi-account setups (an AWS setup with
// assumeRoleInfos, Azure, GCP). It does NOT cover AWS C2S Access Portal (CAP)
// accounts a filter leaves out: those never reach a snapshot, and a CAP setup
// in "collect all" mode (no assumeRoleInfos) is not in the result at all. Use
// Discover for the portal's accounts.
func (s *CloudAccountsService) NewlyDiscovered(ctx context.Context, networkID string) (map[string][]DiscoveredCloudAccount, *Response, error) {
	path, err := cloudAccountsPath(s.client, networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"?"+url.Values{"view": []string{"newlyDiscovered"}}.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CloudAccounts.NewlyDiscovered")
	var out map[string][]DiscoveredCloudAccount
	resp, err := s.client.doRequired(req, &out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// CloudDiscoveredEntity is one account, subscription or project a discovery
// found. ID is the account id (AWS), subscription id (Azure) or project id
// (GCP). Name is set for Azure and GCP. Agency, Mission and Roles are set only
// for an AWS C2S Access Portal (CAP) setup, where each (ID, role) pair is one
// value a CloudAccount's AWSC2SAccountInfos entry can name (AccountID = ID,
// AccountRole = a role). ErrorMsg is why Forward could not use this entity.
type CloudDiscoveredEntity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Agency   string   `json:"agency,omitempty"`
	Mission  string   `json:"mission,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	ErrorMsg string   `json:"errorMsg,omitempty"`
}

// CloudDiscovery is the outcome of Discover. Error is a DeviceCollectionError
// name ("NONE" when the run worked); Entities is everything the cloud offered,
// pinned or not.
type CloudDiscovery struct {
	Error    string                  `json:"error"`
	Entities []CloudDiscoveredEntity `json:"discoveredEntities"`
}

// CloudDiscoveryTimeout is the HTTP timeout Discover needs. Forward holds the
// response until the collector finishes, up to its own 20-minute ceiling
// (CloudAccountDiscoveryTaskHandler.DISCOVERY_TIMEOUT); a shorter client
// timeout reads a slow success as a failure.
const CloudDiscoveryTimeout = 21 * time.Minute

// Discover asks the setup's collector to list the accounts, subscriptions or
// projects its stored credentials can see. POST
// /api/networks/{networkId}/cloudAccounts/{name}/discovery
// (getExistingCloudAccountDiscoveryResult, Preview; MANAGE_COLLECTION_SOURCES)
// with no body, so it uses the stored setup unchanged. It changes no setup, but
// it records a collector task, needs the collector online, and can take
// minutes. For a CAP setup it lists ALL portal accounts, whether the setup's
// filter pins them or not (ID is the account id), so a filter can be checked
// against pairs the portal really offers: a wrong id or role matches nothing,
// with no error. Azure and GCP return subscriptions and projects. An AWS setup
// without CAP is not supported and reports Error "UNSUPPORTED_VENDOR". A failed
// discovery is an HTTP 200 whose Error is not "NONE", not a Go error. A 503
// from Forward's own timeout is retried like any 503 if a RetryPolicy is set,
// which starts another discovery; leave retries off for this call if that
// matters.
func (s *CloudAccountsService) Discover(ctx context.Context, networkID, name string) (*CloudDiscovery, *Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: cloud account name is required")
	}
	path, err := cloudAccountPath(s.client, networkID, name)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path+"/discovery", nil)
	if err != nil {
		return nil, nil, err
	}
	// The endpoint consumes JSON with an optional body; naming the type keeps a
	// bodyless request from being refused for its media type.
	req.Header.Set("Content-Type", "application/json")
	req = markOperation(req, "CloudAccounts.Discover")
	out := new(CloudDiscovery)
	resp, err := s.client.withCallTimeout(CloudDiscoveryTimeout).doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
