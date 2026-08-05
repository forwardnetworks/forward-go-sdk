package forward

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// CredentialsService manages collector-side device credentials. Forward
// substitutes opaque password identifiers in read responses.
type CredentialsService service

// CLICredentialRequest creates a CLI login, privilege escalation, expert, or
// shell credential. Password is write-only secret material.
type CLICredentialRequest struct {
	Type                     string `json:"type"`
	Name                     string `json:"name"`
	Username                 string `json:"username,omitempty"`
	Password                 string `json:"password"`
	PrivilegedModePasswordID string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32 `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool  `json:"autoAssociate,omitempty"`
}

// CLICredential is a stored CLI credential. PasswordID is an opaque identifier
// substituted by Forward and is not secret material usable for authentication.
type CLICredential struct {
	ID                       string `json:"id,omitempty"`
	Type                     string `json:"type"`
	Name                     string `json:"name"`
	Username                 string `json:"username,omitempty"`
	PasswordID               string `json:"password"`
	PrivilegedModePasswordID string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32 `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool  `json:"autoAssociate,omitempty"`
	CreatedBy                string `json:"createdBy,omitempty"`
	CreatedAt                string `json:"createdAt,omitempty"`
}

// HTTPCredentialRequest creates an HTTP login or API-key credential.
type HTTPCredentialRequest struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password"`
	LoginType     string `json:"loginType,omitempty"`
	AutoAssociate *bool  `json:"autoAssociate,omitempty"`
}

// HTTPCredential is a stored HTTP credential.
type HTTPCredential struct {
	ID            string `json:"id,omitempty"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Username      string `json:"username,omitempty"`
	PasswordID    string `json:"password"`
	LoginType     string `json:"loginType,omitempty"`
	AutoAssociate *bool  `json:"autoAssociate,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

func (s *CredentialsService) ListCLI(ctx context.Context, networkID string) ([]CLICredential, *Response, error) {
	path, err := credentialBasePath(networkID, "cli-credentials")
	if err != nil {
		return nil, nil, err
	}
	var values []CLICredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

func (s *CredentialsService) CreateCLI(ctx context.Context, networkID string, credential CLICredentialRequest) (*CLICredential, *Response, error) {
	path, err := credentialBasePath(networkID, "cli-credentials")
	if err != nil {
		return nil, nil, err
	}
	if err := validateCredential(credential.Type, credential.Name, credential.Password); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	created := new(CLICredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

func (s *CredentialsService) GetCLI(ctx context.Context, networkID, credentialID string) (*CLICredential, *Response, error) {
	path, err := credentialPath(networkID, "cli-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	value := new(CLICredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

// UpdateCLI patches version-specific credential fields. A nil map value emits
// JSON null; password values are never exposed to client hooks.
func (s *CredentialsService) UpdateCLI(ctx context.Context, networkID, credentialID string, patch map[string]any) (*CLICredential, *Response, error) {
	path, err := credentialPath(networkID, "cli-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	value := new(CLICredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

func (s *CredentialsService) DeleteCLI(ctx context.Context, networkID, credentialID string) (*Response, error) {
	return s.delete(ctx, networkID, "cli-credentials", credentialID)
}

func (s *CredentialsService) ListHTTP(ctx context.Context, networkID string) ([]HTTPCredential, *Response, error) {
	path, err := credentialBasePath(networkID, "http-credentials")
	if err != nil {
		return nil, nil, err
	}
	var values []HTTPCredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

func (s *CredentialsService) CreateHTTP(ctx context.Context, networkID string, credential HTTPCredentialRequest) (*HTTPCredential, *Response, error) {
	path, err := credentialBasePath(networkID, "http-credentials")
	if err != nil {
		return nil, nil, err
	}
	if err := validateCredential(credential.Type, credential.Name, credential.Password); err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	created := new(HTTPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

func (s *CredentialsService) GetHTTP(ctx context.Context, networkID, credentialID string) (*HTTPCredential, *Response, error) {
	path, err := credentialPath(networkID, "http-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	value := new(HTTPCredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

func (s *CredentialsService) UpdateHTTP(ctx context.Context, networkID, credentialID string, patch map[string]any) (*Response, error) {
	path, err := credentialPath(networkID, "http-credentials", credentialID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

// UpdateHTTPWithResult decodes the updated credential when the endpoint
// returns one and falls back to the typed request fields for an empty 2xx body.
func (s *CredentialsService) UpdateHTTPWithResult(ctx context.Context, networkID, credentialID string, patch HTTPCredentialRequest) (*HTTPCredential, *Response, error) {
	path, err := credentialPath(networkID, "http-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.UpdateHTTPWithResult")
	out := new(HTTPCredential)
	response, err := s.client.Do(req, out)
	if err == nil && out.ID == "" && out.Name == "" {
		out.Name, out.Type, out.Username, out.LoginType, out.AutoAssociate = patch.Name, patch.Type, patch.Username, patch.LoginType, patch.AutoAssociate
	}
	return out, response, err
}

func (s *CredentialsService) DeleteHTTP(ctx context.Context, networkID, credentialID string) (*Response, error) {
	return s.delete(ctx, networkID, "http-credentials", credentialID)
}

// ListSNMP returns the unpublished, version-specific SNMP credential objects.
func (s *CredentialsService) ListSNMP(ctx context.Context, networkID string) ([]map[string]json.RawMessage, *Response, error) {
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	var values []map[string]json.RawMessage
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

// CreateSNMP creates an SNMP credential using a version-specific payload.
// Preview: this route is not in the published OpenAPI contract.
func (s *CredentialsService) CreateSNMP(ctx context.Context, networkID string, credential map[string]any) (map[string]json.RawMessage, *Response, error) {
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	created := map[string]json.RawMessage{}
	resp, err := s.client.Do(req, &created)
	return created, resp, err
}

func (s *CredentialsService) UpdateSNMP(ctx context.Context, networkID, credentialID string, patch map[string]any) (*Response, error) {
	path, err := credentialPath(networkID, "snmpCredentials", credentialID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func (s *CredentialsService) DeleteSNMP(ctx context.Context, networkID, credentialID string) (*Response, error) {
	return s.delete(ctx, networkID, "snmpCredentials", credentialID)
}

func (s *CredentialsService) delete(ctx context.Context, networkID, kind, credentialID string) (*Response, error) {
	path, err := credentialPath(networkID, kind, credentialID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(req, nil)
}

func credentialBasePath(networkID, kind string) (string, error) {
	path, err := networkPath(networkID)
	if err != nil {
		return "", err
	}
	return path + "/" + kind, nil
}

func credentialPath(networkID, kind, credentialID string) (string, error) {
	path, err := credentialBasePath(networkID, kind)
	if err != nil {
		return "", err
	}
	if credentialID = strings.TrimSpace(credentialID); credentialID == "" {
		return "", errors.New("forward: credential ID is required")
	}
	return path + "/" + url.PathEscape(credentialID), nil
}

func validateCredential(kind, name, password string) error {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" || password == "" {
		return errors.New("forward: credential type, name, and password are required")
	}
	return nil
}
