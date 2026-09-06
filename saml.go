package forward

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// SAMLService reads and writes the appliance's SAML settings.
//
// Forward keys SAML by a per-registration CustomName -- the registration id
// -- which is what lets SamlAuthManager resolve an org and do per-org ACG
// lookups. LDAP has no such key and is pinned platform-wide to one org,
// which is why Skyforge federates every org over SAML and never LDAP.
type SAMLService service

// SAMLAuthSettings describes the IdP. EntityID and SSORedirectURL are the
// IdP's (Authentik's), not Forward's.
type SAMLAuthSettings struct {
	Enabled bool `json:"enabled"`
	// Name is a display label only.
	Name           string `json:"name,omitempty"`
	EntityID       string `json:"entityId"`
	SSORedirectURL string `json:"ssoRedirectUrl"`
	// VerificationCert is the IdP signing certificate, PEM.
	VerificationCert string `json:"verificationCert"`
	// DisableAuthNRequestSigning=false keeps Forward signing its AuthnRequests.
	DisableAuthNRequestSigning bool `json:"disableAuthNRequestSigning"`
}

// SAMLSettings is the exact body GET/PUT /api/auth/saml-settings speaks.
// CustomName IS the registration id.
type SAMLSettings struct {
	CustomName       string            `json:"customName"`
	SAMLAuthSettings *SAMLAuthSettings `json:"samlAuthSettings,omitempty"`
}

// GetSettings returns nil, nil when the appliance has no SAML configured --
// Forward answers an empty body or a JSON null, and both mean "none", not an
// error.
func (s *SAMLService) GetSettings(ctx context.Context) (*SAMLSettings, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/auth/saml-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SAML.GetSettings")
	var out optionalValue[SAMLSettings]
	response, err := s.client.Do(req, &out)
	if err != nil {
		return nil, response, err
	}
	if !out.Present {
		return nil, response, nil
	}
	return &out.Value, response, nil
}

func (s *SAMLService) PutSettings(ctx context.Context, input SAMLSettings) (*Response, error) {
	input.CustomName = strings.TrimSpace(input.CustomName)
	if input.CustomName == "" {
		return nil, errors.New("forward: saml customName (the registration id) is required")
	}
	if input.SAMLAuthSettings == nil {
		return nil, errors.New("forward: saml settings are required")
	}
	a := input.SAMLAuthSettings
	if a.Enabled && (strings.TrimSpace(a.EntityID) == "" || strings.TrimSpace(a.SSORedirectURL) == "" || strings.TrimSpace(a.VerificationCert) == "") {
		return nil, errors.New("forward: enabled saml settings need entityId, ssoRedirectUrl and verificationCert")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, "/api/auth/saml-settings", input)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "SAML.PutSettings")
	return s.client.Do(req, nil)
}
