package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
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

// tenableNameRE is Forward's DeviceName rule: word characters, dots and dashes, no spaces.
var tenableNameRE = regexp.MustCompile(`^[\w.-]+$`)

func checkTenableName(name string) error {
	if !tenableNameRE.MatchString(name) {
		return errors.New("forward: a Tenable source name is letters, digits, '_', '.' and '-' only")
	}
	return nil
}

func tenablePath(client *Client, networkID, name string) (string, error) {
	suffix := "/tenable-sources"
	if name != "" {
		suffix += "/" + url.PathEscape(name)
	}
	return integrationNetworkPath(client, networkID, suffix)
}

// ListTenableSC lists the network's Tenable Security Center sources (GET .../end-host-scanners, which also lists Rapid7 sources; only
// the rows of type TENABLE are returned). VIEW_END_HOST_SCANNERS. A network with none gives an empty list.
func (s *Integrations) ListTenableSC(ctx context.Context, networkID string) ([]ports.TenableSource, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := integrationNetworkPath(s.client, networkID, "/end-host-scanners")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.TenableSource]{Keys: []string{"sources", "scanners", "items", "data"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Integrations.ListTenableSC")
	resp, err := s.client.doRequired(req, &result)
	if err != nil {
		return nil, resp, err
	}
	out := []ports.TenableSource{}
	for _, source := range result.Items {
		if strings.EqualFold(source.Type, "TENABLE") {
			out = append(out, source)
		}
	}
	return out, resp, nil
}

// CreateTenableSC adds a Tenable Security Center source (POST .../tenable-sources; MANAGE_END_HOST_SCANNERS). Name, BaseURL (http or
// https) and CredentialID are required; see the type notes for the credential.
func (s *Integrations) CreateTenableSC(ctx context.Context, networkID string, input ports.TenableSourceRequest) (*ports.TenableSource, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
func (s *Integrations) UpdateTenableSC(ctx context.Context, networkID, name string, patch ports.TenableSourcePatch) (*ports.TenableSource, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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

func (s *Integrations) writeTenable(ctx context.Context, method, path string, body any, operation string) (*ports.TenableSource, *ports.Response, error) {
	req, err := s.client.newJSONRequest(ctx, method, path, body)
	if err != nil {
		return nil, nil, err
	}
	out := new(ports.TenableSource)
	resp, err := s.client.Do(markOperation(req, operation), out)
	if err == nil && out.Type == "" {
		out.Type = "TENABLE"
	}
	return out, resp, err
}

// DeleteTenableSC removes the named source; a source that is already gone counts as success (DELETE .../tenable-sources/{sourceName};
// DELETE_COLLECTION_SOURCES; 204). It does not delete the credential.
func (s *Integrations) DeleteTenableSC(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Integrations) TestTenableSC(ctx context.Context, networkID string, input ports.TenableSourceTest) (*ports.SourceTestResult, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	out := new(ports.SourceTestResult)
	// Only ever lengthen the client's timeout: an unlimited or longer one stays. A success is "{}".
	var timeout time.Duration
	if hc := s.client.httpClient; hc != nil && hc.Timeout > 0 && hc.Timeout < ports.TenableTestTimeout {
		timeout = ports.TenableTestTimeout
	}
	resp, err := s.client.doWithTimeout(markOperation(req, "Integrations.TestTenableSC"), out, true, nil, timeout)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
