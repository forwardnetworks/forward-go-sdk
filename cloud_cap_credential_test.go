package forward

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if cred.ClientCertificateNotAfterReason != CapClientCertificateNotParsed {
		t.Fatalf("with no stored password the reason is that the SDK does not parse PKCS#12: %+v", cred)
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
	if err := missing(`{"apiUrl":"/api/networks/12/aws-cap-credentials/CAP-9","httpMethod":"GET","message":"AWS CAP credential CAP-9 not found"}`); errors.Is(err, ErrNetworkNotFound) || !IsStatus(err, http.StatusNotFound) {
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

func caCertPEM(t *testing.T, name string, notBefore, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: notBefore, NotAfter: notAfter, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// A portal CA that expires takes the whole AWS collection down, so the read
// derives the bundle's validity: how many certificates parsed, the EARLIEST
// NotAfter and the LATEST NotBefore. Unreadable material is "absent" (zero),
// never an error; the client .p12 can only report why its expiry is unknown;
// and no CA byte reaches any formatting or JSON of the result.
func TestGetAWSCapCredentialDerivesCAValidityWithoutKeepingBytes(t *testing.T) {
	soon := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	later := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	bundle := append(caCertPEM(t, "ca-a", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), later),
		caCertPEM(t, "ca-b", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), soon)...)
	encoded := base64.StdEncoding.EncodeToString(bundle)

	respond := func(ca string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"id":"c","baseCapServerUrl":"https://cap.example.mil","clientCertificateBase64":"`+capSecret+`","clientCertificatePasswordId":"SEC-1","certificateAuthorityBase64":"`+ca+`"}`)
		}))
	}
	read := func(ca string) *AWSCapCredential {
		server := respond(ca)
		defer server.Close()
		cred, _, err := newTestClient(t, server.URL).CloudAccounts.GetAWSCapCredential(context.Background(), "net-1", "c")
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}

	cred := read(encoded)
	if cred.CertificateAuthorityCount != 2 || !cred.CertificateAuthorityNotAfter.Equal(soon) ||
		!cred.CertificateAuthorityNotBefore.Equal(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("bundle validity = %+v", cred)
	}
	if cred.ClientCertificateNotAfter != (time.Time{}) || cred.ClientCertificateNotAfterReason != CapClientCertificatePasswordRequired {
		t.Fatalf("with a stored password the .p12 expiry is unreadable and must say why: %+v", cred)
	}
	encodedJSON, _ := json.Marshal(cred)
	for name, text := range map[string]string{"%+v": fmt.Sprintf("%+v", cred), "json": string(encodedJSON)} {
		if strings.Contains(text, encoded[:40]) || strings.Contains(text, capSecret) || strings.Contains(text, "BEGIN CERTIFICATE") {
			t.Fatalf("%s carries certificate bytes: %s", name, text)
		}
	}

	// Forward loads only PEM text, so a bare DER certificate is NOT a usable CA
	// and must not read as one; padding-free, whitespace-wrapped base64, a
	// non-certificate block and an already-expired certificate are the cases
	// the PEM path has to get right.
	block, _ := pem.Decode(caCertPEM(t, "der", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), later))
	if der := read(base64.StdEncoding.EncodeToString(block.Bytes)); !der.HasCertificateAuthority || der.CertificateAuthorityCount != 0 {
		t.Fatalf("a bare DER CA is not loaded by Forward and must read as unreadable: %+v", der)
	}
	expired := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mixed := append([]byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), caCertPEM(t, "old", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), expired)...)
	wrapped := base64.RawStdEncoding.EncodeToString(mixed)
	wrapped = wrapped[:60] + "\n" + wrapped[60:]
	if got := read(strings.ReplaceAll(wrapped, "\n", "\\n")); got.CertificateAuthorityCount != 1 || !got.CertificateAuthorityNotAfter.Equal(expired) {
		t.Fatalf("an expired cert in an unpadded, wrapped bundle with a key block must count once: %+v", got)
	}
	// Forward keys CAs by subject and the last one wins, so a renewed root must
	// replace the superseded one rather than report its old expiry.
	oldRoot := caCertPEM(t, "same-subject", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), expired)
	renewed := caCertPEM(t, "same-subject", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), later)
	if got := read(base64.StdEncoding.EncodeToString(append(oldRoot, renewed...))); got.CertificateAuthorityCount != 1 || !got.CertificateAuthorityNotAfter.Equal(later) {
		t.Fatalf("a renewed root must supersede the old one: %+v", got)
	}
	// Present but unreadable: HasCertificateAuthority stays true, the count and dates are zero, and there is no error.
	if bad := read("Q0EtYnl0ZXM="); !bad.HasCertificateAuthority || bad.CertificateAuthorityCount != 0 || !bad.CertificateAuthorityNotAfter.IsZero() {
		t.Fatalf("unreadable bundle = %+v", bad)
	}
	if none := read(""); none.HasCertificateAuthority || none.CertificateAuthorityCount != 0 {
		t.Fatalf("no bundle = %+v", none)
	}
}
