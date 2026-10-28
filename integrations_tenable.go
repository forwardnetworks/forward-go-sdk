package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Tenable Security Center as an end-host scanner source (EndHostScannerController: .../tenable-sources, scanner type TENABLE). Forward
// reads the scanned assets and vulnerabilities from the Security Center at BaseURL. Preview: not in the published spec; the routes
// are mapped at both pinned builds. This is NOT the org-level /vulnerability-scanners API, which only implements Tenable Vulnerability
// Management (cloud.tenable.com).
//
// The credential must already exist in the network as an HTTP credential of type LOGIN, holding the Security Center access key and
// secret key as its username and password (Forward answers 400 "HTTP Login credential expected" for any other id or type).

// TenableSource is a Tenable Security Center source as Forward stores it (StoredEndHostScanner). MaxPageSize is nil when Forward's
// default (10000) applies, and CollectorID empty when no collector is named. Type is TENABLE in the list; Create and Update set it
// themselves because Forward leaves it out of those answers. CollectionError is Forward's error name from the last attempt, if it failed.
type TenableSource struct {
	Name                 string     `json:"name"`
	Type                 string     `json:"type,omitempty"`
	BaseURL              string     `json:"baseUrl,omitempty"`
	CredentialID         Identifier `json:"credentialId,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	MaxPageSize          *int       `json:"maxPageSize,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
	CollectionError      string     `json:"collectionError,omitempty"`
	LastAttempted        string     `json:"lastAttempted,omitempty"`
}

// TenableSourceRequest creates a source (NewTenableSource). MaxPageSize nil sends nothing, so Forward uses 10000; CollectorID empty
// sends nothing.
type TenableSourceRequest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	CredentialID         Identifier `json:"credentialId"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	CollectionDisabled   bool       `json:"collectionDisabled,omitempty"`
	MaxPageSize          *int       `json:"maxPageSize,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
}

// TenableSourcePatch changes a source; nil fields stay as they are (TenableSourcePatch). ClearMaxPageSize and ClearCollector send an
// explicit null, which Forward reads as "back to the default" and "no collector" (they win over the value fields).
type TenableSourcePatch struct {
	Name                 *string
	BaseURL              *string
	CredentialID         *Identifier
	DisableSSLValidation *bool
	CollectionDisabled   *bool
	MaxPageSize          *int
	CollectorID          *Identifier
	ClearMaxPageSize     bool
	ClearCollector       bool
}

// MarshalJSON sends only what is set, with explicit nulls for the two Clear flags.
func (p TenableSourcePatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.BaseURL != nil {
		m["baseUrl"] = *p.BaseURL
	}
	if p.CredentialID != nil {
		m["credentialId"] = *p.CredentialID
	}
	if p.DisableSSLValidation != nil {
		m["disableSslValidation"] = *p.DisableSSLValidation
	}
	if p.CollectionDisabled != nil {
		m["collectionDisabled"] = *p.CollectionDisabled
	}
	switch {
	case p.ClearMaxPageSize:
		m["maxPageSize"] = nil
	case p.MaxPageSize != nil:
		m["maxPageSize"] = *p.MaxPageSize
	}
	switch {
	case p.ClearCollector:
		m["collectorId"] = nil
	case p.CollectorID != nil:
		m["collectorId"] = *p.CollectorID
	}
	return json.Marshal(m)
}

// TenableSourceTest is what a connection test needs (EndHostScannerTestConfig): the same fields as a source, none of them stored.
type TenableSourceTest struct {
	Name                 string     `json:"name"`
	BaseURL              string     `json:"baseUrl"`
	CredentialID         Identifier `json:"credentialId"`
	DisableSSLValidation bool       `json:"disableSslValidation,omitempty"`
	CollectorID          Identifier `json:"collectorId,omitempty"`
}

// SourceTestResult is the verdict of a connection test (EndHostScannerTestResult). Forward leaves Error out on success.
type SourceTestResult struct {
	Error string `json:"error,omitempty"`
}

// OK reports whether the test found nothing wrong.
func (r SourceTestResult) OK() bool { return r.Error == "" }

// tenableNameRE is Forward's DeviceName rule: word characters, dots and dashes, no spaces.
var tenableNameRE = regexp.MustCompile(`^[\w.-]+$`)

func checkTenableName(name string) error {
	if !tenableNameRE.MatchString(name) {
		return errors.New("forward: a Tenable source name is letters, digits, '_', '.' and '-' only")
	}
	return nil
}

// TenableTestTimeout is the HTTP timeout TestTenableSC needs: Forward holds the request until the collector finishes, which can take
// its full 60 seconds (TestEndHostScannerTaskHandler.TEST_END_HOST_SCANNER_OP_TIMEOUT) plus a margin.
const TenableTestTimeout = 90 * time.Second

func tenablePath(client *Client, networkID, name string) (string, error) {
	suffix := "/tenable-sources"
	if name != "" {
		suffix += "/" + url.PathEscape(name)
	}
	return integrationNetworkPath(client, networkID, suffix)
}

// ListTenableSC lists the network's Tenable Security Center sources (GET .../end-host-scanners, which also lists Rapid7 sources; only
// the rows of type TENABLE are returned). VIEW_END_HOST_SCANNERS. A network with none gives an empty list.
func (s *IntegrationsService) ListTenableSC(ctx context.Context, networkID string) ([]TenableSource, *Response, error) {
	path, err := integrationNetworkPath(s.client, networkID, "/end-host-scanners")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[TenableSource]{Keys: []string{"sources", "scanners", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ListTenableSC")
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	out := []TenableSource{}
	for _, source := range result.Items {
		if strings.EqualFold(source.Type, "TENABLE") {
			out = append(out, source)
		}
	}
	return out, resp, nil
}

// CreateTenableSC adds a Tenable Security Center source (POST .../tenable-sources; MANAGE_END_HOST_SCANNERS). Name, BaseURL (http or
// https) and CredentialID are required; see the type notes for the credential.
func (s *IntegrationsService) CreateTenableSC(ctx context.Context, networkID string, input TenableSourceRequest) (*TenableSource, *Response, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" || input.CredentialID == "" {
		return nil, nil, errors.New("forward: a Tenable source needs a name, a base URL and a credential id")
	}
	if err := checkTenableName(strings.TrimSpace(input.Name)); err != nil {
		return nil, nil, err
	}
	if input.MaxPageSize != nil && *input.MaxPageSize <= 0 {
		return nil, nil, errors.New("forward: maxPageSize must be positive")
	}
	path, err := tenablePath(s.client, networkID, "")
	if err != nil {
		return nil, nil, err
	}
	return s.writeTenable(ctx, http.MethodPost, path, input, "Integrations.CreateTenableSC")
}

// UpdateTenableSC changes the named source (PATCH .../tenable-sources/{sourceName}; MANAGE_END_HOST_SCANNERS) and answers it as stored.
func (s *IntegrationsService) UpdateTenableSC(ctx context.Context, networkID, name string, patch TenableSourcePatch) (*TenableSource, *Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: Tenable source name is required")
	}
	if err := checkTenableName(name); err != nil {
		return nil, nil, err
	}
	if patch.Name != nil {
		if err := checkTenableName(strings.TrimSpace(*patch.Name)); err != nil {
			return nil, nil, err
		}
	}
	if patch.MaxPageSize != nil && *patch.MaxPageSize <= 0 {
		return nil, nil, errors.New("forward: maxPageSize must be positive")
	}
	path, err := tenablePath(s.client, networkID, name)
	if err != nil {
		return nil, nil, err
	}
	return s.writeTenable(ctx, http.MethodPatch, path, patch, "Integrations.UpdateTenableSC")
}

func (s *IntegrationsService) writeTenable(ctx context.Context, method, path string, body any, operation string) (*TenableSource, *Response, error) {
	req, err := s.client.newJSONRequest(ctx, method, path, body)
	if err != nil {
		return nil, nil, err
	}
	out := new(TenableSource)
	resp, err := s.client.Do(markOperation(req, operation), out)
	if err == nil && out.Type == "" {
		out.Type = "TENABLE"
	}
	return out, resp, err
}

// DeleteTenableSC removes the named source; a source that is already gone counts as success (DELETE .../tenable-sources/{sourceName};
// DELETE_COLLECTION_SOURCES; 204). It does not delete the credential.
func (s *IntegrationsService) DeleteTenableSC(ctx context.Context, networkID, name string) (*Response, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, errors.New("forward: Tenable source name is required")
	}
	if err := checkTenableName(name); err != nil {
		return nil, err
	}
	path, err := tenablePath(s.client, networkID, name)
	if err != nil {
		return nil, err
	}
	return s.deleteTolerant(ctx, path, "Integrations.DeleteTenableSC")
}

// TestTenableSC tests a connection without storing anything (POST .../tenable-sources?action=test; TEST_END_HOST_SCANNERS). Forward runs it
// on a collector (the named one, else the network's default; 400 if none is online) and answers when it finishes, up to about 60 seconds,
// so a shorter client timeout is raised to TenableTestTimeout (the ctx deadline still applies). A failed test is a 200 with Error set,
// not an error return.
func (s *IntegrationsService) TestTenableSC(ctx context.Context, networkID string, input TenableSourceTest) (*SourceTestResult, *Response, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" || input.CredentialID == "" {
		return nil, nil, errors.New("forward: a Tenable test needs a name, a base URL and a credential id")
	}
	if err := checkTenableName(strings.TrimSpace(input.Name)); err != nil {
		return nil, nil, err
	}
	path, err := tenablePath(s.client, networkID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path+"?action=test", input)
	if err != nil {
		return nil, nil, err
	}
	out := new(SourceTestResult)
	// Only ever lengthen the client's timeout: an unlimited or longer one stays. A success is "{}".
	var timeout time.Duration
	if hc := s.client.httpClient; hc != nil && hc.Timeout > 0 && hc.Timeout < TenableTestTimeout {
		timeout = TenableTestTimeout
	}
	resp, err := s.client.doWithTimeout(markOperation(req, "Integrations.TestTenableSC"), out, true, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
