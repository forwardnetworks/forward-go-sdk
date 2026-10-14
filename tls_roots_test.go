package forward

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A Forward behind a private CA must be reachable by trusting that CA alone:
// without RootCAs the handshake fails, with it the request succeeds, and
// verification stays on (a server whose certificate is not in the pool is
// still refused).
func TestRootCAsTrustsAPrivateCAWithoutDisablingVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeCertPEM(t, bundle, server.Certificate())
	roots, err := LoadRootCAs(bundle)
	if err != nil {
		t.Fatal(err)
	}

	call := func(cfg Config) error {
		cfg.BaseURL, cfg.Username, cfg.Password = server.URL, "u", "p"
		client, err := NewClient(cfg)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(context.Background(), http.MethodGet, "/api/version", nil)
		if err != nil {
			return err
		}
		_, err = client.Do(req, nil)
		return err
	}

	var verifyErr *tls.CertificateVerificationError
	if err := call(Config{HTTPClient: privateHTTPClient()}); !errors.As(err, &verifyErr) {
		t.Fatalf("a self-signed server must fail certificate verification without RootCAs, got %v", err)
	}
	if err := call(Config{HTTPClient: privateHTTPClient(), RootCAs: roots}); err != nil {
		t.Fatalf("a server signed by the pooled CA was refused: %v", err)
	}
	// Every httptest server shares one certificate, so the foreign CA is
	// generated: a pool holding some other CA must still refuse this server.
	foreign := x509.NewCertPool()
	foreign.AddCert(generatedCA(t))
	if err := call(Config{HTTPClient: privateHTTPClient(), RootCAs: foreign}); !errors.As(err, &verifyErr) {
		t.Fatalf("RootCAs must not trust a certificate outside the pool, got %v", err)
	}
}

func TestLoadRootCAsRejectsAFileWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRootCAs(path); err == nil {
		t.Fatal("a file with no certificate must be an error")
	}
	if _, err := LoadRootCAs(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("a missing file must be an error")
	}
}

func writeCertPEM(t *testing.T, path string, cert *x509.Certificate) {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func generatedCA(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "unrelated test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
