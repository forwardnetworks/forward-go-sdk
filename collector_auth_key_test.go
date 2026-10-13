package forward

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// v3Key builds the base64 CollectorAuthKeyPB (string fields 1, 2, 3) the way the
// appserver's CollectorAuthKeyV3.format does.
func v3Key(username, password, appURL string) string {
	var raw []byte
	for i, v := range []string{username, password, appURL} {
		raw = append(raw, byte((i+1)<<3|2), byte(len(v)))
		raw = append(raw, v...)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// The collector container reads TOKEN as "username:password" and splits on the
// first ':'. authorizationKey is the V2 key itself (already that string) or, with
// COLLECTOR_AUTH_TOKEN_V3, a base64 proto with no ':' at all, which would leave
// the container with the whole blob as both user and password. Token must return
// the container form either way.
func TestCollectorRegistrationTokenIsContainerFormForV2AndV3(t *testing.T) {
	v2 := CollectorRegistration{Username: "collector-f7fc099f", AuthorizationKey: "collector-f7fc099f:41c36fcb9bc348a1be38751ca1dd1b81"}
	token, err := v2.Token()
	if err != nil || token != "collector-f7fc099f:41c36fcb9bc348a1be38751ca1dd1b81" {
		t.Fatalf("V2 Token() = %q, %v", token, err)
	}
	if url, err := v2.AppServerURL(); err != nil || url != "" {
		t.Fatalf("V2 AppServerURL() = %q, %v; a V2 key carries none", url, err)
	}

	v3 := CollectorRegistration{AuthorizationKey: v3Key("collector-0a1b2c3d", "deadbeef", "https://fwd.app")}
	token, err = v3.Token()
	if err != nil || token != "collector-0a1b2c3d:deadbeef" {
		t.Fatalf("V3 Token() = %q, %v", token, err)
	}
	if url, err := v3.AppServerURL(); err != nil || url != "https://fwd.app" {
		t.Fatalf("V3 AppServerURL() = %q, %v", url, err)
	}

	for _, bad := range []string{"", "collector-onlyuser", "not base64 !!"} {
		if _, err := (CollectorRegistration{AuthorizationKey: bad}).Token(); err == nil {
			t.Fatalf("Token() accepted malformed key %q", bad)
		}
	}
}

// Performance uploads authenticate as the collector with its PASSWORD. Handing
// the whole "collector-x:password" key over as the password can never match the
// stored hash, so Identity must decode it.
func TestCollectorRegistrationIdentityCarriesThePassword(t *testing.T) {
	for _, key := range []string{"collector-aa:secret", v3Key("collector-aa", "secret", "https://fwd.app")} {
		id := CollectorRegistration{Username: "collector-aa", AuthorizationKey: key}.Identity()
		if id.AuthorizationKey != "secret" {
			t.Fatalf("Identity() for %q = %#v, want password secret", key, id)
		}
	}
}

// A rotation keeps the username and returns only the new password; the request
// names the collector and nothing else.
func TestCollectorsResetKey(t *testing.T) {
	var method, path string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectorName":"edge","username":"collector-aa","password":"newpass"}`))
	}))
	defer srv.Close()

	creds, _, err := newTestClient(t, srv.URL).Collectors.ResetKey(context.Background(), " edge ")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/orgs/current/collectors/resetPassword" {
		t.Fatalf("sent %s %s", method, path)
	}
	var sent map[string]any
	_ = json.Unmarshal(body, &sent)
	if sent["collectorName"] != "edge" || sent["password"] != "" {
		t.Fatalf("body = %s", body)
	}
	if creds.Token() != "collector-aa:newpass" {
		t.Fatalf("Token() = %q", creds.Token())
	}
	if _, _, err := newTestClient(t, srv.URL).Collectors.ResetKey(context.Background(), " "); err == nil {
		t.Fatal("ResetKey accepted an empty name")
	}
}
