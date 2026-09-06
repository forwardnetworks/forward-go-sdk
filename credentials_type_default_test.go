package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// Forward defaults an ABSENT credential type to LOGIN -- in
// NewCliCredential.getType() and again in the CliCredential constructor, and
// the same pair for HTTP. Skyforge has created CLI credentials with no type
// in production for months on exactly that behaviour.
//
// So two things must hold. The client must not refuse the request, and it
// must send NO type key rather than an empty one: "" is not absent, and
// Forward answers a 400 when it cannot map "" onto the enum.
func TestCredentialTypeIsOmittedNotEmptied(t *testing.T) {
	var body map[string]any
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"c1","name":"n"}`))
	})

	got, _, err := c.Credentials.CreateCLI(context.Background(), "net-1", CLICredentialRequest{
		Name: "Skyforge CLI", Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatalf("a credential with no type must be accepted, Forward defaults it to LOGIN: %v", err)
	}
	if got == nil || got.ID != "c1" {
		t.Fatalf("created credential not decoded: %+v", got)
	}
	if _, present := body["type"]; present {
		t.Fatalf(`type must be OMITTED when unset, not sent as "": %v`, body["type"])
	}

	// An explicit type still travels.
	body = nil
	if _, _, err := c.Credentials.CreateCLI(context.Background(), "net-1", CLICredentialRequest{
		Type: "PRIVILEGED_MODE", Name: "n", Password: "p",
	}); err != nil {
		t.Fatalf("explicit type: %v", err)
	}
	if body["type"] != "PRIVILEGED_MODE" {
		t.Fatalf("explicit type did not reach the wire: %v", body["type"])
	}

	// What Forward DOES require is still refused here, where the message can
	// name the field, rather than as an opaque 400.
	if _, _, err := c.Credentials.CreateCLI(context.Background(), "net-1", CLICredentialRequest{Password: "p"}); err == nil {
		t.Fatal("an empty name must be refused")
	}
	if _, _, err := c.Credentials.CreateCLI(context.Background(), "net-1", CLICredentialRequest{Name: "n"}); err == nil {
		t.Fatal("an empty password must be refused")
	}

	// HTTP credentials default the same way.
	body = nil
	if _, _, err := c.Credentials.CreateHTTP(context.Background(), "net-1", HTTPCredentialRequest{
		Name: "h", Password: "p",
	}); err != nil {
		t.Fatalf("http credential with no type must be accepted: %v", err)
	}
	if _, present := body["type"]; present {
		t.Fatalf("http type must be omitted when unset: %v", body["type"])
	}
}
