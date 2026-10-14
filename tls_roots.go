package forward

import (
	"crypto/x509"
	"fmt"
	"os"
)

// LoadRootCAs reads a PEM file of one or more CA certificates into a pool for
// Config.RootCAs. The pool holds only those certificates, so a client built
// with it trusts nothing else: that is what a Forward behind a private CA
// wants, and it is stricter than the system roots. A file with no certificate
// in it is an error rather than an empty pool that would fail every request
// with a certificate error that does not name the cause.
func LoadRootCAs(path string) (*x509.CertPool, error) {
	pemData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("forward: read CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("forward: CA bundle %s holds no PEM certificate", path)
	}
	return pool, nil
}
