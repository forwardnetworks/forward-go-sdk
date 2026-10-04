package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestJumpServersCreateUsesTheLiveRouteAndNewJumpServerBody pins Create to
// the only jump-server create route Forward has --
// POST /api/networks/{networkId}/jumpServers (NetworkSetupController, both
// the primary 15398425a69 and stable 67e89c87124 builds) -- and to the
// NewJumpServer body (api/schemas/jump-servers/NewJumpServer.yaml): sshKey /
// sshCert / port, never privateKey / certificate.
//
// Create used to POST /jump-servers with privateKey/certificate. That route
// has never existed, so every key-authenticated jump server the task engine
// asked for failed with a 404 (and, had the path been right, Forward's
// Jackson would have silently dropped the unknown key fields and refused the
// server as having no credential).
func TestJumpServersCreateUsesTheLiveRouteAndNewJumpServerBody(t *testing.T) {
	t.Parallel()
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/networks/net-7/jumpServers" {
			http.Error(w, "No endpoint "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = io.WriteString(w, `{"id":"js-1","host":"10.0.0.9","port":2222,"username":"lab"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := client.JumpServers.Create(context.Background(), "net-7", JumpServerRequest{
		Host:     "10.0.0.9",
		Port:     2222,
		Username: "lab",
		SSHKey:   "-----BEGIN KEY-----",
		SSHCert:  "ssh-rsa-cert-v01@openssh.com AAAA",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != "js-1" {
		t.Fatalf("created id = %q, want js-1", created.ID)
	}
	want := map[string]any{
		"host":                   "10.0.0.9",
		"port":                   float64(2222),
		"username":               "lab",
		"sshKey":                 "-----BEGIN KEY-----",
		"sshCert":                "ssh-rsa-cert-v01@openssh.com AAAA",
		"supportsPortForwarding": true,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("body[%q] = %#v, want %#v", key, got[key], value)
		}
	}
	for _, dead := range []string{"privateKey", "certificate"} {
		if _, ok := got[dead]; ok {
			t.Errorf("body carries %q, which NewJumpServer does not define", dead)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected body key %q", key)
		}
	}
}

// TestJumpServersCreateOmitsOptionalFields: port defaults to 22 and sshCert
// only applies to certificate auth, so neither is sent when unset.
func TestJumpServersCreateOmitsOptionalFields(t *testing.T) {
	t.Parallel()
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"id":"js-2"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.JumpServers.Create(context.Background(), "n", JumpServerRequest{Host: "h", Username: "u", SSHKey: "k"}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"port", "sshCert"} {
		if _, ok := got[key]; ok {
			t.Errorf("unset optional %q was sent: %#v", key, got[key])
		}
	}
}

// TestJumpServersCreateRequiresKeyMaterial: Create is the key-authenticated
// constructor; an empty key is refused before any request is sent.
func TestJumpServersCreateRequiresKeyMaterial(t *testing.T) {
	t.Parallel()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: "https://forward.invalid", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]JumpServerRequest{
		"host":     {Username: "u", SSHKey: "k"},
		"username": {Host: "h", SSHKey: "k"},
		"sshKey":   {Host: "h", Username: "u"},
	} {
		if _, _, err := client.JumpServers.Create(context.Background(), "n", input); err == nil {
			t.Errorf("missing %s: Create returned no error", name)
		}
	}
}
