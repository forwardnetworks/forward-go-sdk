package forward

import (
	"context"
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
	// Type is OMITTED when empty, deliberately. Absent means LOGIN, which is
	// what Forward defaults to; an empty STRING is a different thing entirely
	// and fails enum deserialization with a 400.
	Type                     string `json:"type,omitempty"`
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

// SNMP credentials, typed from Forward's own model (SnmpCredential.java).
//
// The version decides which of the two credential halves is legal, and the
// server enforces it rather than ignoring the wrong one:
//
//	V2C -> communityString applies; authSettings is NOT supported
//	V3  -> authSettings is REQUIRED; communityString is NOT supported
//
// There is no V1. Version itself is required -- the constructor does
// checkNotNull on it -- so it is sent even when empty would have been
// tempting, and the zero value is not silently meaningful.
type SNMPVersion string

const (
	SNMPVersionV2C SNMPVersion = "V2C"
	SNMPVersionV3  SNMPVersion = "V3"
)

// SNMPAuthType is the v3 authentication digest. MD5 and SHA are SHA-1 era;
// the SHA_2xx family is what current devices expect.
type SNMPAuthType string

const (
	SNMPAuthMD5    SNMPAuthType = "MD5"
	SNMPAuthSHA    SNMPAuthType = "SHA"
	SNMPAuthSHA256 SNMPAuthType = "SHA_256"
	SNMPAuthSHA384 SNMPAuthType = "SHA_384"
	SNMPAuthSHA512 SNMPAuthType = "SHA_512"
)

// SNMPPrivacyProtocol is the v3 privacy cipher. AES_192 and AES_256 are the
// extended-key variants some vendors ship, not RFC 3826 standard.
type SNMPPrivacyProtocol string

const (
	SNMPPrivacyDES    SNMPPrivacyProtocol = "DES"
	SNMPPrivacyAES128 SNMPPrivacyProtocol = "AES_128"
	SNMPPrivacyAES192 SNMPPrivacyProtocol = "AES_192"
	SNMPPrivacyAES256 SNMPPrivacyProtocol = "AES_256"
)

// SNMPAuthSettings is the v3 half. The fields are conditionally dependent, in
// Forward's words: password applies iff authType is set, privacyProtocol
// applies iff privacyPassword is set AND requires authType. Username is the
// only unconditional field and cannot be blank.
type SNMPAuthSettings struct {
	Username        string              `json:"username"`
	Password        string              `json:"password,omitempty"`
	AuthType        SNMPAuthType        `json:"authType,omitempty"`
	PrivacyProtocol SNMPPrivacyProtocol `json:"privacyProtocol,omitempty"`
	PrivacyPassword string              `json:"privacyPassword,omitempty"`
}

// SNMPCredentialRequest creates an SNMP credential.
type SNMPCredentialRequest struct {
	Name            string            `json:"name,omitempty"`
	Version         SNMPVersion       `json:"version"`
	Port            *int              `json:"port,omitempty"`
	TimeoutSec      *int              `json:"timeoutSec,omitempty"`
	CommunityString string            `json:"communityString,omitempty"`
	AuthSettings    *SNMPAuthSettings `json:"authSettings,omitempty"`
	AutoAssociate   *bool             `json:"autoAssociate,omitempty"`
}

// SNMPCredential is a stored SNMP credential. The secret material is not
// returned; what comes back identifies the credential.
type SNMPCredential struct {
	ID            string      `json:"id,omitempty"`
	Name          string      `json:"name,omitempty"`
	Version       SNMPVersion `json:"version,omitempty"`
	Port          *int        `json:"port,omitempty"`
	TimeoutSec    *int        `json:"timeoutSec,omitempty"`
	AutoAssociate *bool       `json:"autoAssociate,omitempty"`
}

// validateSNMPCredential refuses only what Forward refuses, and refuses it
// here so the message names the field instead of arriving as an opaque 400.
func validateSNMPCredential(c SNMPCredentialRequest) error {
	switch c.Version {
	case SNMPVersionV2C:
		if c.AuthSettings != nil {
			return errors.New("forward: authSettings is not supported for SNMP V2C")
		}
	case SNMPVersionV3:
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

// HTTPCredentialRequest creates an HTTP login or API-key credential.
type HTTPCredentialRequest struct {
	// Omitted when empty, for the same reason as the CLI request: Forward
	// defaults an absent type to LOGIN (NewHttpCredential.getType()), while an
	// empty STRING fails enum deserialization.
	Type          string `json:"type,omitempty"`
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
func (s *CredentialsService) UpdateCLI(ctx context.Context, networkID, credentialID string, patch CLICredentialPatch) (*CLICredential, *Response, error) {
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

func (s *CredentialsService) UpdateHTTP(ctx context.Context, networkID, credentialID string, patch HTTPCredentialPatch) (*Response, error) {
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

// CLICredentialPatch changes part of a stored CLI credential. Every field is a
// pointer so that "not stated" and "stated as empty" stay distinguishable: a
// patch that clears a username and one that leaves it alone are different
// requests.
type CLICredentialPatch struct {
	Name                     *string `json:"name,omitempty"`
	Username                 *string `json:"username,omitempty"`
	Password                 *string `json:"password,omitempty"`
	PrivilegedModePasswordID *string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32  `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool   `json:"autoAssociate,omitempty"`
}

// HTTPCredentialPatch changes part of a stored HTTP credential.
type HTTPCredentialPatch struct {
	Name          *string `json:"name,omitempty"`
	Username      *string `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	LoginType     *string `json:"loginType,omitempty"`
	AutoAssociate *bool   `json:"autoAssociate,omitempty"`
}

// SNMPCredentialPatch changes part of a stored SNMP credential.
type SNMPCredentialPatch struct {
	Name               *string `json:"name,omitempty"`
	Community          *string `json:"community,omitempty"`
	User               *string `json:"user,omitempty"`
	AuthenticationType *string `json:"authenticationType,omitempty"`
	AuthenticationKey  *string `json:"authenticationKey,omitempty"`
	PrivacyType        *string `json:"privacyType,omitempty"`
	PrivacyKey         *string `json:"privacyKey,omitempty"`
	ContextName        *string `json:"contextName,omitempty"`
	AutoAssociate      *bool   `json:"autoAssociate,omitempty"`
}

// ListSNMP returns the unpublished, version-specific SNMP credential objects.
func (s *CredentialsService) ListSNMP(ctx context.Context, networkID string) ([]SNMPCredential, *Response, error) {
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	var values []SNMPCredential
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req, &values)
	return values, resp, err
}

// CreateSNMP creates an SNMP credential using a version-specific payload.
// Preview: this route is not in the published OpenAPI contract.
func (s *CredentialsService) CreateSNMP(ctx context.Context, networkID string, credential SNMPCredentialRequest) (*SNMPCredential, *Response, error) {
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, credential)
	if err != nil {
		return nil, nil, err
	}
	created := new(SNMPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

// CreateSNMPCredential is the typed form of CreateSNMP. Prefer it: the map
// form cannot tell V2C's communityString from V3's authSettings, and sending
// the wrong half is a 400 that names nothing.
func (s *CredentialsService) CreateSNMPCredential(ctx context.Context, networkID string, credential SNMPCredentialRequest) (*SNMPCredential, *Response, error) {
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
	created := new(SNMPCredential)
	resp, err := s.client.Do(req, created)
	return created, resp, err
}

// ListSNMPCredentials is the typed form of ListSNMP. It accepts the wrapped
// envelopes Forward uses across list routes as well as a bare array.
func (s *CredentialsService) ListSNMPCredentials(ctx context.Context, networkID string) ([]SNMPCredential, *Response, error) {
	path, err := credentialBasePath(networkID, "snmpCredentials")
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[SNMPCredential]{
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
