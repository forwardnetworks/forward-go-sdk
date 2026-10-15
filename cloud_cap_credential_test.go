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
		_, _ = io.WriteString(w, `{"id":"cred/7","baseCapServerUrl":"https://capAcct.example.mil","clientCertificateBase64":"`+capSecret+`","certificateAuthorityBase64":"Q0EtYnl0ZXM="}`)
	}))
	defer server.Close()

	cred, _, err := newTestClient(t, server.URL).CloudAccounts.GetAWSCapCredential(context.Background(), "net-1", "cred/7")
	if err != nil {
		t.Fatal(err)
	}
	if cred.ID != "cred/7" || cred.BaseCapServerURL != "https://capAcct.example.mil" || !cred.HasCertificateAuthority {
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
		_, _ = io.WriteString(w, `{"id":"c","baseCapServerUrl":"https://capAcct.example.mil","clientCertificateBase64":"x"}`)
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

// A CAP account is recognised from typed fields, so a caller no longer parses
// Raw. The embedded certificate-bearing awsCapCredential object has no field
// (it stays reachable only through Raw), and a plain assume-role account must
// not read as CAP.
func TestCloudAccountCarriesTheCAPFields(t *testing.T) {
	var capAcct, plain CloudAccount
	if err := json.Unmarshal([]byte(`{"type":"AWS","name":"gov","awsCapCredentialId":"cap-1","awsC2SAccountInfos":[{"accountId":"111122223333","accountRole":"Auditor"}]}`), &capAcct); err != nil {
		t.Fatal(err)
	}
	if capAcct.AWSCapCredentialID != "cap-1" || len(capAcct.AWSC2SAccountInfos) != 1 || capAcct.AWSC2SAccountInfos[0] != (AWSC2SAccountInfo{AccountID: "111122223333", AccountRole: "Auditor"}) || !capAcct.HasCAPCredential() {
		t.Fatalf("CAP account decoded wrong: %+v", capAcct)
	}
	if err := json.Unmarshal([]byte(`{"type":"AWS","name":"main","assumeRoleInfos":[{"accountId":"1","enabled":true}]}`), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.HasCAPCredential() || plain.AWSCapCredentialID != "" || plain.AWSC2SAccountInfos != nil {
		t.Fatalf("an assume-role account must not read as CAP: %+v", plain)
	}
	var embedded CloudAccount
	if err := json.Unmarshal([]byte(`{"type":"AWS","name":"e","awsCapCredential":{"baseCapServerUrl":"u","clientCertificateBase64":"`+capSecret+`"}}`), &embedded); err != nil {
		t.Fatal(err)
	}
	if !embedded.HasCAPCredential() {
		t.Fatal("an embedded CAP credential must read as CAP")
	}
	if encoded, _ := json.Marshal(embedded); strings.Contains(string(encoded), capSecret) {
		t.Fatalf("marshalling the account must not emit the embedded certificate: %s", encoded)
	}
	for _, empty := range []string{`"awsCapCredential":null`, `"awsCapCredential":{}`} {
		var account CloudAccount
		if err := json.Unmarshal([]byte(`{"type":"AWS","name":"n",`+empty+`}`), &account); err != nil {
			t.Fatal(err)
		}
		if account.HasCAPCredential() {
			t.Fatalf("%s is not a CAP credential", empty)
		}
	}
}

// PATCH takes the CAP filter (UpdateAwsAccountRequest.awsC2SAccountInfos): nil
// leaves it out, a non-nil empty list is sent and clears the filter (which collection rejects).
func TestPatchSendsTheCAPFilterOnlyWhenSet(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		_, _ = io.WriteString(w, `{"type":"AWS","name":"gov"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	filter := []AWSC2SAccountInfo{{AccountID: "111122223333", AccountRole: "Auditor"}}
	empty := []AWSC2SAccountInfo{}
	for _, patch := range []CloudAccountPatch{
		{Type: "AWS"},
		{Type: "AWS", AWSC2SAccountInfos: &filter},
		{Type: "AWS", AWSC2SAccountInfos: &empty},
	} {
		if _, _, err := client.CloudAccounts.Patch(ctx, "net-1", "gov", patch); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{`{"type":"AWS"}`, `{"type":"AWS","awsC2SAccountInfos":[{"accountId":"111122223333","accountRole":"Auditor"}]}`, `{"type":"AWS","awsC2SAccountInfos":[]}`}
	for i := range want {
		if strings.TrimSpace(bodies[i]) != want[i] {
			t.Fatalf("body %d = %s, want %s", i, bodies[i], want[i])
		}
	}
}
