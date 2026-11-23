package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"strings"
)

// SAMLService reads and writes the appliance's SAML settings.
//
// Forward keys SAML by a per-registration CustomName -- the registration id
// -- which is what lets SamlAuthManager resolve an org and do per-org ACG
// lookups. LDAP has no such key and is pinned platform-wide to one org,
// which is why Skyforge federates every org over SAML and never LDAP.
type SAML service

// GetSettings returns nil, nil when the appliance has no SAML configured --
// Forward answers an empty body or a JSON null, and both mean "none", not an
// error.
func (s *SAML) GetSettings(ctx context.Context) (*ports.SAMLSettings, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/auth/saml-settings", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "SAML.GetSettings")
	var out optionalValue[ports.SAMLSettings]
	response, err := s.client.Do(req, &out)
	if err != nil {
		return nil, response, err
	}
	if !out.Present {
		return nil, response, nil
	}
	return &out.Value, response, nil
}

func (s *SAML) PutSettings(ctx context.Context, input ports.SAMLSettings) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
