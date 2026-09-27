package forward

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// Forward's session filter chain enforces Spring Security CSRF on every
// state-changing request; the stateless Basic-auth chain does not. A
// browser-mode client (a login or an impersonated session) therefore needs the
// session's token on each write, or Forward answers 403. The token is read
// from GET /api/public/csrf once per authenticated session and dropped
// whenever the session's principal changes (login, impersonation, SAML
// sign-in), since Spring issues a new token on authentication.

const defaultBrowserCSRFHeader = "X-CSRF-TOKEN"

type browserCSRFState struct {
	mu    sync.Mutex
	token *BrowserCSRFToken
}

type browserCSRFSkipKey struct{}

// skipBrowserCSRF marks a request that must not carry the cached session
// token: the requests that establish a session carry their own proof.
func skipBrowserCSRF(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), browserCSRFSkipKey{}, true))
}

func browserRequestNeedsCSRF(req *http.Request, mode AuthMode) bool {
	if mode != AuthModeBrowser {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	if skip, _ := req.Context().Value(browserCSRFSkipKey{}).(bool); skip {
		return false
	}
	return req.Header.Get(defaultBrowserCSRFHeader) == ""
}

func (c *Client) attachBrowserCSRF(req *http.Request) error {
	token, err := c.browserCSRFToken(req.Context())
	if err != nil {
		return err
	}
	header := strings.TrimSpace(token.HeaderName)
	if header == "" {
		header = defaultBrowserCSRFHeader
	}
	req.Header.Set(header, strings.TrimSpace(token.Token))
	return nil
}

func (c *Client) browserCSRFToken(ctx context.Context) (*BrowserCSRFToken, error) {
	state := c.csrf
	if state != nil {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.token != nil {
			return state.token, nil
		}
	}
	token, _, err := c.fetchPublicCSRF(ctx, "/api/public/csrf", "Browser.SessionCSRF")
	if err != nil {
		return nil, err
	}
	if token == nil || strings.TrimSpace(token.Token) == "" {
		return nil, errors.New("forward: CSRF endpoint returned no token")
	}
	if state != nil {
		state.token = token
	}
	return token, nil
}

func (c *Client) resetBrowserCSRF() {
	if c == nil || c.csrf == nil {
		return
	}
	c.csrf.mu.Lock()
	c.csrf.token = nil
	c.csrf.mu.Unlock()
}
