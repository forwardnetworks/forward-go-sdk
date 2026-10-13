package forward

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// collectorUsernamePrefix is Forward's prefix for every collector username
// (CollectorAuthKeys.USERNAME_PREFIX); it is how a V2 key is told from a V3 one.
const collectorUsernamePrefix = "collector-"

// CollectorKey is the decoded collector authorization key.
//
// Forward serialises CollectorWithAuthKey.authorizationKey with the key's
// @JsonValue, so the field is the whole installation key, not the password:
// "collector-<8 hex>:<32 hex>" (V2, the default) or, when the org property
// COLLECTOR_AUTH_TOKEN_V3 is on, the base64 of a protobuf holding the username,
// the password and the app server URL (V3). Verified against
// CollectorAuthKeys.java and BaseCollectorAccountService.add (Preview: the route
// is unpublished).
type CollectorKey struct {
	Username string
	Password string
	// AppURL is the app server the key was generated for; empty for V2, which
	// does not carry one.
	AppURL string
}

// Token is "username:password", the form the Docker collector's TOKEN
// environment variable takes (its setup.sh splits on the first ':').
func (k CollectorKey) Token() string { return k.Username + ":" + k.Password }

// ParseCollectorAuthKey decodes an authorization key of either format.
func ParseCollectorAuthKey(key string) (CollectorKey, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return CollectorKey{}, errors.New("forward: collector authorization key is empty")
	}
	if strings.HasPrefix(key, collectorUsernamePrefix) {
		username, password, ok := strings.Cut(key, ":")
		if !ok || username == "" || password == "" || strings.Contains(password, ":") {
			return CollectorKey{}, errors.New("forward: malformed V2 collector authorization key")
		}
		return CollectorKey{Username: username, Password: password}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return CollectorKey{}, fmt.Errorf("forward: collector authorization key is neither V2 nor base64 V3: %w", err)
	}
	// CollectorAuthKeyPB: string username = 1; string password = 2; string app_server_url = 3.
	var out CollectorKey
	for len(raw) > 0 {
		tag, n := binary.Uvarint(raw)
		if n <= 0 {
			return CollectorKey{}, errors.New("forward: malformed V3 collector authorization key")
		}
		raw = raw[n:]
		if tag&7 != 2 {
			return CollectorKey{}, errors.New("forward: unexpected field type in V3 collector authorization key")
		}
		size, n := binary.Uvarint(raw)
		if n <= 0 || size > uint64(len(raw)-n) {
			return CollectorKey{}, errors.New("forward: truncated V3 collector authorization key")
		}
		value := string(raw[n : n+int(size)])
		raw = raw[n+int(size):]
		switch tag >> 3 {
		case 1:
			out.Username = value
		case 2:
			out.Password = value
		case 3:
			out.AppURL = value
		}
	}
	if out.Username == "" || out.Password == "" {
		return CollectorKey{}, errors.New("forward: V3 collector authorization key lacks a username or password")
	}
	return out, nil
}

// Key decodes AuthorizationKey.
func (r CollectorRegistration) Key() (CollectorKey, error) {
	return ParseCollectorAuthKey(r.AuthorizationKey)
}

// Token returns exactly what the collector container reads as TOKEN
// ("username:password"), decoding a V3 key to it.
func (r CollectorRegistration) Token() (string, error) {
	key, err := r.Key()
	if err != nil {
		return "", err
	}
	return key.Token(), nil
}

// AppServerURL is the app server a V3 key was generated for, for the container's
// APP_HOST (host part). It is empty for a V2 key, which carries none.
func (r CollectorRegistration) AppServerURL() (string, error) {
	key, err := r.Key()
	if err != nil {
		return "", err
	}
	return key.AppURL, nil
}

// CollectorCredentials is what a password reset returns: the same username with
// a new password. It carries no app URL, so it is never V3.
type CollectorCredentials struct {
	CollectorName string `json:"collectorName"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// Token is "username:password", the container's TOKEN form.
func (c CollectorCredentials) Token() string { return c.Username + ":" + c.Password }

// ResetKey issues a new password for the named collector, keeping its username,
// and invalidates the old one at once: a running collector stops authenticating
// until it is given the new token. POST /api/orgs/current/collectors/resetPassword
// (ClientController.resetCollectorPassword, @RequiresUser; the appserver then
// requires the caller to be allowed to modify that collector). Preview.
func (s *CollectorsService) ResetKey(ctx context.Context, collectorName string) (*CollectorCredentials, *Response, error) {
	collectorName = strings.TrimSpace(collectorName)
	if collectorName == "" {
		return nil, nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/orgs/current/collectors/resetPassword", CollectorCredentials{CollectorName: collectorName})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.ResetKey")
	out := new(CollectorCredentials)
	resp, err := s.client.doRequired(req, out)
	if err == nil && (strings.TrimSpace(out.Username) == "" || out.Password == "") {
		err = errors.New("forward: collector password reset returned no credentials")
	}
	return out, resp, err
}
