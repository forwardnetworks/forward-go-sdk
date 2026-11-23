package domain

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
