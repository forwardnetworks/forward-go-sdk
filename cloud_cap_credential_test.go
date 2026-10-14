package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const capSecret = "TUlJS3NlY3JldC1wMTItYnl0ZXM="

// The read must reach the wire route, report the portal URL and whether a CA
// is set, and never let the client's .p12 certificate (which Forward returns
// on this same read) reach the result: not as a field, and not through any
// formatting or JSON of it.
func TestGetAWSCapCredentialDropsTheClientCertificate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/networks/net-1/aws-cap-credentials/cred%2F7" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"id":"cred/7","baseCapServerUrl":"https://cap.example.mil","clientCertificateBase64":"`+capSecret+`","certificateAuthorityBase64":"Q0EtYnl0ZXM="}`)
	}))
	defer server.Close()

	cred, _, err := newTestClient(t, server.URL).CloudAccounts.GetAWSCapCredential(context.Background(), "net-1", "cred/7")
	if err != nil {
		t.Fatal(err)
	}
	if cred.ID != "cred/7" || cred.BaseCapServerURL != "https://cap.example.mil" || !cred.HasCertificateAuthority {
		t.Fatalf("credential = %+v", cred)
	}
	encoded, _ := json.Marshal(cred)
	for name, text := range map[string]string{
		"%v": fmt.Sprintf("%v", cred), "%+v": fmt.Sprintf("%+v", cred), "%#v": fmt.Sprintf("%#v", cred), "json": string(encoded),
	} {
		if strings.Contains(text, capSecret) || strings.Contains(text, "Q0EtYnl0ZXM=") {
			t.Fatalf("%s of the credential leaks certificate bytes: %s", name, text)
		}
	}
}

func TestGetAWSCapCredentialWithoutCAAndOnRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/viewer-only") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"Missing permission: NetworkOperation.VIEW_DEVICE_CREDENTIALS"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"c","baseCapServerUrl":"https://cap.example.mil","clientCertificateBase64":"x"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	cred, _, err := client.CloudAccounts.GetAWSCapCredential(ctx, "net-1", "c")
	if err != nil || cred.HasCertificateAuthority {
		t.Fatalf("a credential with no CA must say so: %+v, %v", cred, err)
	}
	_, _, err = client.CloudAccounts.GetAWSCapCredential(ctx, "net-1", "viewer-only")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a caller without VIEW_DEVICE_CREDENTIALS must get the typed 403: %v", err)
	}
	if op, ok := MissingPermission(err); !ok || op != "NetworkOperation.VIEW_DEVICE_CREDENTIALS" {
		t.Fatalf("MissingPermission = %q, %v", op, ok)
	}
	if _, _, err := client.CloudAccounts.GetAWSCapCredential(ctx, "net-1", " "); err == nil {
		t.Fatal("a blank credential id must fail before any request")
	}
}

// Forward's 404 body carries the request's apiUrl. For a missing CAP
// credential that URL reads /api/networks/N/..., which used to supply the
// word "network" to the "not found" rule, so a stale credential id was
// reported as a missing network. Only a message that names the network may
// say so.
func TestMissingChildOfANetworkIsNotAMissingNetwork(t *testing.T) {
	missing := func(body string) error {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, body)
		}))
		defer server.Close()
		_, _, err := newTestClient(t, server.URL).CloudAccounts.GetAWSCapCredential(context.Background(), "12", "CAP-9")
		return err
	}
	if err := missing(`{"apiUrl":"/api/networks/12/aws-cap-credentials/CAP-9","httpMethod":"GET","message":"AWS CAP credential CAP-9 not found"}`); errors.Is(err, ErrNetworkNotFound) || !isStatus(err, http.StatusNotFound) {
		t.Fatalf("a missing credential must be a plain 404, not a missing network: %v", err)
	}
	if err := missing(`{"apiUrl":"/api/networks/12/aws-cap-credentials/CAP-9","httpMethod":"GET","message":"Network 12 not found"}`); !errors.Is(err, ErrNetworkNotFound) {
		t.Fatalf("a message naming the network must still classify as a missing network: %v", err)
	}
}
