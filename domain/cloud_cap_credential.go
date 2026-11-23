package domain

import (
	"time"
)

// AWSCapCredential is what a read of an AWS C2S Access Portal (CAP) credential
// keeps: its id, the portal's base URL, whether a certificate authority is
// configured for it, and that bundle's validity dates. The credential also
// holds the client's .p12 certificate and the CA's bytes, which Forward returns
// on the same read; only dates and a count are derived from them, so no String,
// GoString, %v or JSON of this type can carry the bytes. This SDK reads CAP credentials only to show which portal a setup uses;
// it does not create or edit them.
type AWSCapCredential struct {
	ID                      string `json:"id"`
	BaseCapServerURL        string `json:"baseCapServerUrl"`
	HasCertificateAuthority bool   `json:"hasCertificateAuthority"`

	// CertificateAuthorityCount is how many certificates the CA bundle holds
	// that could be parsed, and CertificateAuthorityNotAfter the EARLIEST expiry
	// among them (the first date after which the bundle stops being fully
	// trusted); CertificateAuthorityNotBefore is the LATEST NotBefore (when every
	// one of them is valid; the window from it to NotAfter can be empty or
	// inverted if the certificates do not overlap). Both dates are zero when there is no bundle or none
	// of it parsed (HasCertificateAuthority true with a zero count means the
	// stored bundle is unreadable). Derived on read; the bytes are never kept.
	CertificateAuthorityCount     int       `json:"certificateAuthorityCount,omitempty"`
	CertificateAuthorityNotBefore time.Time `json:"certificateAuthorityNotBefore,omitzero"`
	CertificateAuthorityNotAfter  time.Time `json:"certificateAuthorityNotAfter,omitzero"`

	// ClientCertificateNotAfter is always zero in this SDK: the client
	// certificate is a PKCS#12 (.p12), and the SDK does not parse PKCS#12 (the
	// standard library has no parser and this package takes no dependencies).
	// ClientCertificateNotAfterReason says why, so "unknown" differs from
	// "not set": CapClientCertificatePasswordRequired when Forward holds a
	// password for the .p12 (its certificate is normally encrypted under it and
	// is unreadable without it), CapClientCertificateNotParsed when no password
	// is stored (the certificate may be plaintext, but this SDK does not open the
	// file). The portal's SERVER certificate expiry is not reported anywhere by
	// Forward: its CAP connectivity test (CapConnectivityTestResult,
	// CloudConnectivityService.testCapConnection) returns only a test instant and
	// an error, and an expired server certificate surfaces as a collection error
	// with no date.
	ClientCertificateNotAfter       time.Time `json:"clientCertificateNotAfter,omitzero"`
	ClientCertificateNotAfterReason string    `json:"clientCertificateNotAfterReason,omitempty"`
}

// Reasons for AWSCapCredential.ClientCertificateNotAfterReason.
const (
	// CapClientCertificatePasswordRequired: the .p12 has a stored password, and its
	// certificate cannot be read without it.
	CapClientCertificatePasswordRequired = "PASSWORD_REQUIRED"
	// CapClientCertificateNotParsed: no password is stored, so the certificate may
	// be readable, but this SDK does not parse PKCS#12.
	CapClientCertificateNotParsed = "PKCS12_NOT_PARSED"
)
