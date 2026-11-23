package rest

import (
	"bytes"
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// SAMLAuthenticationRequest starts SP-initiated SAML sign-in to the org that
// owns registrationID (GET /saml2/authenticate/{registrationId}, served by
// Forward's SAML filter chain). Forward answers with the POST-binding form
// addressed to the IdP. The request runs on this client's cookie jar, so the
// same client must carry the assertion back through SAMLAssertionConsumer.
func (s *Browser) SAMLAuthenticationRequest(ctx context.Context, registrationID string) (*ports.BrowserSAMLForm, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	if err := s.requireBrowser(); err != nil {
		return nil, nil, err
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return nil, nil, errors.New("forward: SAML registration ID is required")
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodGet, "/saml2/authenticate/"+url.PathEscape(registrationID), nil, pathScopeBrowser, &requestAuth{mode: ports.AuthModeBrowser})
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req = markOperation(req, "Browser.SAMLAuthenticationRequest")
	var body bytes.Buffer
	response, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, response, err
	}
	form, err := ports.ParseBrowserSAMLForm(body.String())
	if err != nil {
		return nil, response, err
	}
	if form.Fields.Get("SAMLRequest") == "" {
		return nil, response, errors.New("forward: SAML authentication form carries no SAMLRequest")
	}
	return form, response, nil
}

// SAMLAssertionConsumer posts the IdP's SAML Response to Forward's assertion
// consumer service for registrationID (POST /login/saml2/sso/{registrationId})
// on THIS client's Forward origin -- the same origin the AuthnRequest came
// from, since Forward keeps the request in the session and checks
// InResponseTo against it. The IdP's form addresses Forward's public URL,
// which an in-cluster client may not share; checking that the IdP aimed at
// the expected consumer is the caller's job, before calling this.
//
// Forward JIT-creates the user on the first sign-in to an org and refreshes
// its external groups on every sign-in. Success is a redirect that does not
// land on /login.
func (s *Browser) SAMLAssertionConsumer(ctx context.Context, registrationID string, fields url.Values) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := s.requireBrowser(); err != nil {
		return nil, err
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return nil, errors.New("forward: SAML registration ID is required")
	}
	if fields.Get("SAMLResponse") == "" {
		return nil, errors.New("forward: SAML assertion form carries no SAMLResponse")
	}
	req, err := s.client.newScopedRequest(ctx, http.MethodPost, "/login/saml2/sso/"+url.PathEscape(registrationID), strings.NewReader(fields.Encode()), pathScopeBrowser, &requestAuth{mode: ports.AuthModeBrowser})
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	// The SAML chain has no CSRF protection (the assertion is the proof); a
	// token fetched now would belong to the pre-authentication session.
	req = markOperation(skipBrowserCSRF(req), "Browser.SAMLAssertionConsumer")
	response, err := s.client.doAccepted(req, nil, true, func(status int) bool { return status >= 200 && status < 400 })
	s.client.resetBrowserCSRF()
	if err != nil {
		return response, err
	}
	if response.Request != nil && response.Request.URL != nil && strings.EqualFold(strings.Trim(response.Request.URL.Path, "/"), "login") {
		return response, errors.New("forward: SAML sign-in was rejected (redirected to login)")
	}
	if len(s.Cookies()) == 0 {
		return response, errors.New("forward: SAML sign-in returned no session cookies")
	}
	return response, nil
}
