package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// CredentialsService manages collector-side device credentials. Forward
// substitutes opaque password identifiers in read responses.
type Credentials service

// validateSNMPCredential refuses only what Forward refuses, and refuses it
// here so the message names the field instead of arriving as an opaque 400.
func validateSNMPCredential(c ports.SNMPCredentialRequest) error {
	switch c.Version {
	case ports.SNMPVersionV2C:
		if c.AuthSettings != nil {
			return errors.New("forward: authSettings is not supported for SNMP V2C")
		}
	case ports.SNMPVersionV3:
		if c.CommunityString != "" {
			return errors.New("forward: communityString is not supported for SNMP V3")
		}
		if c.AuthSettings == nil {
			return errors.New("forward: authSettings is required for SNMP V3")
		}
		if strings.TrimSpace(c.AuthSettings.Username) == "" {
			return errors.New("forward: SNMP V3 authSettings.username is required")
		}
	case "":
		return errors.New("forward: SNMP credential version is required (V2C or V3)")
	default:
		return errors.New("forward: SNMP credential version must be V2C or V3")
	}
	return nil
}

func (s *Credentials) ListCLI(ctx context.Context, networkID string) ([]ports.CLICredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "cli-credentials")
	if err != nil {
		return nil, nil, err
	}
	var values []ports.CLICredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

func (s *Credentials) CreateCLI(ctx context.Context, networkID string, credential ports.CLICredentialRequest) (*ports.CLICredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	created := new(ports.CLICredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

func (s *Credentials) GetCLI(ctx context.Context, networkID, credentialID string) (*ports.CLICredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialPath(networkID, "cli-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	value := new(ports.CLICredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

// UpdateCLI patches version-specific credential fields. A nil map value emits
// JSON null; password values are never exposed to client hooks.
func (s *Credentials) UpdateCLI(ctx context.Context, networkID, credentialID string, patch ports.CLICredentialPatch) (*ports.CLICredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialPath(networkID, "cli-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	value := new(ports.CLICredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

func (s *Credentials) DeleteCLI(ctx context.Context, networkID, credentialID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.delete(ctx, networkID, "cli-credentials", credentialID)
}

func (s *Credentials) ListHTTP(ctx context.Context, networkID string) ([]ports.HTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "http-credentials")
	if err != nil {
		return nil, nil, err
	}
	var values []ports.HTTPCredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

func (s *Credentials) CreateHTTP(ctx context.Context, networkID string, credential ports.HTTPCredentialRequest) (*ports.HTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	created := new(ports.HTTPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

func (s *Credentials) GetHTTP(ctx context.Context, networkID, credentialID string) (*ports.HTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialPath(networkID, "http-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	value := new(ports.HTTPCredential)
	resp, err := s.client.Do(req, value)
	return value, resp, err
}

func (s *Credentials) UpdateHTTP(ctx context.Context, networkID, credentialID string, patch ports.HTTPCredentialPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *Credentials) UpdateHTTPWithResult(ctx context.Context, networkID, credentialID string, patch ports.HTTPCredentialRequest) (*ports.HTTPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialPath(networkID, "http-credentials", credentialID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.UpdateHTTPWithResult")
	out := new(ports.HTTPCredential)
	response, err := s.client.Do(req, out)
	if err == nil && out.ID == "" && out.Name == "" {
		out.Name, out.Type, out.Username, out.LoginType, out.AutoAssociate = patch.Name, patch.Type, patch.Username, patch.LoginType, patch.AutoAssociate
	}
	return out, response, err
}

func (s *Credentials) DeleteHTTP(ctx context.Context, networkID, credentialID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.delete(ctx, networkID, "http-credentials", credentialID)
}

// ListSNMP returns the unpublished, version-specific SNMP credential objects.
func (s *Credentials) ListSNMP(ctx context.Context, networkID string) ([]ports.SNMPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	var values []ports.SNMPCredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

// CreateSNMP creates an SNMP credential using a version-specific payload.
// Preview: this route is not in the published OpenAPI contract.
func (s *Credentials) CreateSNMP(ctx context.Context, networkID string, credential ports.SNMPCredentialRequest) (*ports.SNMPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	created := new(ports.SNMPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

// CreateSNMPCredential is the typed form of CreateSNMP. Prefer it: the map
// form cannot tell V2C's communityString from V3's authSettings, and sending
// the wrong half is a 400 that names nothing.
func (s *Credentials) CreateSNMPCredential(ctx context.Context, networkID string, credential ports.SNMPCredentialRequest) (*ports.SNMPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := validateSNMPCredential(credential); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.CreateSNMP")
	created := new(ports.SNMPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

// ListSNMPCredentials is the typed form of ListSNMP. It accepts the wrapped
// envelopes Forward uses across list routes as well as a bare array.
func (s *Credentials) ListSNMPCredentials(ctx context.Context, networkID string) ([]ports.SNMPCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.SNMPCredential]{
		Keys:        []string{"snmpCredentials", "credentials", "items", "data", "results"},
		AllowSingle: true,
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Credentials.ListSNMP")
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

func (s *Credentials) UpdateSNMP(ctx context.Context, networkID, credentialID string, patch map[string]any) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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

func (s *Credentials) DeleteSNMP(ctx context.Context, networkID, credentialID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	return s.delete(ctx, networkID, "snmpCredentials", credentialID)
}

func (s *Credentials) delete(ctx context.Context, networkID, kind, credentialID string) (*ports.Response, error) {
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

// validateCredential refuses only what Forward itself refuses.
//
// It used to require a non-empty type. Forward does NOT: an absent type is
// defaulted to LOGIN, in two places and for both credential kinds --
// NewCliCredential.getType() and the CliCredential constructor
// (`this.type = type != null ? type : CliCredentialType.LOGIN`), and the same
// pair for HTTP. Skyforge has been creating CLI credentials without a type in
// production for months on exactly that behaviour.
//
// A validator stricter than the server is worse than none: it refuses a
// request the API would have accepted, and it did so in the client where the
// error names nothing about Forward. Name and password are still required
// because the server enforces them -- name cannot be empty, and password is
// required for every type except SSH_KEY, which this request shape does not
// carry.
func validateCredential(kind, name, password string) error {
	_ = kind
	if strings.TrimSpace(name) == "" || password == "" {
		return errors.New("forward: credential name and password are required")
	}
	return nil
}
