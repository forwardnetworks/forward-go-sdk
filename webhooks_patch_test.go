package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward's WebhookUpdate accepts a credential, a template, event parameters and the SSL switch on PATCH. The patch must
// send exactly the fields that were stated and none of the others, because an absent field means "leave it alone" there.
func TestWebhookPatchSendsOnlyStatedFields(t *testing.T) {
	t.Parallel()

	var got map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/webhooks/ops" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	off := true
	_, err := client.Webhooks.Update(context.Background(), "ops", WebhookPatch{
		DisableSSLValidation: &off,
		Credential:           &WebhookCredentialRequest{Type: "BASIC_AUTH", Username: "u", Password: "p"},
		Template:             json.RawMessage(`{"body":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("sent %v, want only disableSslValidation, credential and template", got)
	}
	if string(got["disableSslValidation"]) != "true" || string(got["template"]) != `{"body":"x"}` {
		t.Errorf("body = %v", got)
	}
	var cred WebhookCredentialRequest
	if err := json.Unmarshal(got["credential"], &cred); err != nil || cred.Username != "u" || cred.Password != "p" || cred.Type != "BASIC_AUTH" {
		t.Errorf("credential = %s (%v)", got["credential"], err)
	}
}
