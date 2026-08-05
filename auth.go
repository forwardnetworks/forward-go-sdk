package forward

import (
	"errors"
	"net/http"
	"strings"
)

// AuthMode identifies the principal used for a Forward operation. User and
// service principals both use HTTP Basic authentication on the wire, but stay
// distinct so factories, policy, and telemetry cannot silently mix them.
type AuthMode string

const (
	AuthModeUser      AuthMode = "user"
	AuthModeService   AuthMode = "service"
	AuthModeCollector AuthMode = "collector"
	AuthModeBrowser   AuthMode = "browser"
	AuthModeNone      AuthMode = "none"
)

// CollectorIdentity is the authorization identity returned by collector
// registration. AuthorizationKey is a collector secret; it is not the API
// user's password or token secret.
type CollectorIdentity struct {
	Username         string `json:"username"`
	AuthorizationKey string `json:"authorizationKey"`
}

func normalizeAuthMode(mode AuthMode) (AuthMode, error) {
	if mode == "" {
		return AuthModeUser, nil
	}
	switch mode {
	case AuthModeUser, AuthModeService, AuthModeCollector, AuthModeBrowser, AuthModeNone:
		return mode, nil
	default:
		return "", errors.New("forward: invalid authentication mode")
	}
}

func authCredentials(cfg Config, mode AuthMode) (string, string, error) {
	hasToken := strings.TrimSpace(cfg.APIToken) != ""
	hasUserPassword := strings.TrimSpace(cfg.Username) != "" || cfg.Password != ""
	if hasToken && hasUserPassword {
		return "", "", errors.New("forward: configure either API token or username/password, not both")
	}
	if hasToken {
		accessKey, secret, ok := strings.Cut(strings.TrimSpace(cfg.APIToken), ":")
		if !ok || strings.TrimSpace(accessKey) == "" || secret == "" {
			return "", "", errors.New("forward: API token must have the form accessKey:secret")
		}
		return strings.TrimSpace(accessKey), secret, nil
	}

	username := strings.TrimSpace(cfg.Username)
	password := cfg.Password
	switch mode {
	case AuthModeNone:
		if username != "" || password != "" {
			return "", "", errors.New("forward: unauthenticated clients must not configure credentials")
		}
		return "", "", nil
	case AuthModeBrowser:
		// A browser client may start from cookies alone or retain credentials for
		// an explicit Login/Impersonate bootstrap. Browser requests never apply
		// Basic auth unless the typed operation explicitly asks for it.
		if (username == "") != (password == "") {
			return "", "", errors.New("forward: browser username and password must be configured together")
		}
		return username, password, nil
	default:
		if username == "" || password == "" {
			return "", "", errors.New("forward: username and password are required")
		}
		return username, password, nil
	}
}

func copyCookies(cookies []*http.Cookie) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" || cookie.Value == "" {
			continue
		}
		clone := *cookie
		out = append(out, &clone)
	}
	return out
}
