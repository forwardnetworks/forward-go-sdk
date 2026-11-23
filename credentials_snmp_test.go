package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// The version decides which half of an SNMP credential is legal, and Forward
// ENFORCES it rather than ignoring the wrong one (SnmpCredential.java):
// V2C takes communityString and rejects authSettings; V3 requires
// authSettings and rejects communityString. The map-based API could not tell
// those apart, which is why this exists.
func TestSNMPCredentialVersionDecidesTheLegalHalf(t *testing.T) {
	var body map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"s1","name":"n","version":"V2C"}`))
	})
	ctx := context.Background()

	got, _, err := c.Credentials.CreateSNMPCredential(ctx, "net-1", domain.SNMPCredentialRequest{
		Name: "Skyforge SNMPv2", Version: domain.SNMPVersionV2C, CommunityString: "skyforge",
	})
	if err != nil {
		t.Fatalf("V2C with a community string must be accepted: %v", err)
	}
	if got == nil || got.ID != "s1" {
		t.Fatalf("created credential not decoded: %+v", got)
	}
	if body["version"] != "V2C" || body["communityString"] != "skyforge" {
		t.Fatalf("wire form wrong: %v", body)
	}
	if _, present := body["authSettings"]; present {
		t.Fatal("authSettings must be omitted for V2C")
	}

	for name, req := range map[string]domain.SNMPCredentialRequest{
		"V2C with authSettings":    {Version: domain.SNMPVersionV2C, AuthSettings: &domain.SNMPAuthSettings{Username: "u"}},
		"V3 with communityString":  {Version: domain.SNMPVersionV3, CommunityString: "x", AuthSettings: &domain.SNMPAuthSettings{Username: "u"}},
		"V3 without authSettings":  {Version: domain.SNMPVersionV3},
		"V3 with a blank username": {Version: domain.SNMPVersionV3, AuthSettings: &domain.SNMPAuthSettings{Username: "  "}},
		"no version at all":        {Name: "n"},
		"a version Forward lacks":  {Version: "V1"},
	} {
		if _, _, err := c.Credentials.CreateSNMPCredential(ctx, "net-1", req); err == nil {
			t.Errorf("%s must be refused before it reaches Forward", name)
		}
	}

	// V3 done properly still travels, privacy fields included.
	body = nil
	if _, _, err := c.Credentials.CreateSNMPCredential(ctx, "net-1", domain.SNMPCredentialRequest{
		Version: domain.SNMPVersionV3,
		AuthSettings: &domain.SNMPAuthSettings{
			Username: "netops", Password: "p", AuthType: domain.SNMPAuthSHA256,
			PrivacyProtocol: domain.SNMPPrivacyAES256, PrivacyPassword: "pp",
		},
	}); err != nil {
		t.Fatalf("valid V3: %v", err)
	}
	auth, ok := body["authSettings"].(map[string]any)
	if !ok || auth["username"] != "netops" || auth["authType"] != "SHA_256" || auth["privacyProtocol"] != "AES_256" {
		t.Fatalf("V3 auth settings wire form wrong: %v", body["authSettings"])
	}
	if _, present := body["communityString"]; present {
		t.Fatal("communityString must be omitted for V3")
	}
}
