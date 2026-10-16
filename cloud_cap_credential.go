package forward

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"strings"
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

// GetAWSCapCredential reads a CAP credential by the id an AWS account carries
// in awsCapCredentialId (CloudAccount.Raw). GET
// /api/networks/{networkId}/aws-cap-credentials/{credentialId}
// (CloudAccountController.getAwsCapCredential, Preview; VIEW_DEVICE_CREDENTIALS).
// A caller without that permission gets Forward's typed 403 (ErrPermissionDenied,
// MissingPermission), and an unknown id its 404.
//
// Forward's body carries clientCertificateBase64, the client's secret .p12
// certificate; it is read off the wire and dropped here, never stored in the
// result or in an error.
func (s *CloudAccountsService) GetAWSCapCredential(ctx context.Context, networkID, credentialID string) (*AWSCapCredential, *Response, error) {
	if credentialID = strings.TrimSpace(credentialID); credentialID == "" {
		return nil, nil, errors.New("forward: CAP credential id is required")
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	path, err := networkPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path+"/aws-cap-credentials/"+url.PathEscape(credentialID), nil)
	if err != nil {
		return nil, nil, err
	}
	var body struct {
		ID                         string `json:"id"`
		BaseCapServerURL           string `json:"baseCapServerUrl"`
		CertificateAuthorityBase64 string `json:"certificateAuthorityBase64"`
		// Present only when a password is stored (the id of a secret, never the password).
		ClientCertificatePasswordID string `json:"clientCertificatePasswordId"`
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	credential := &AWSCapCredential{
		ID: body.ID, BaseCapServerURL: body.BaseCapServerURL,
		HasCertificateAuthority:         strings.TrimSpace(body.CertificateAuthorityBase64) != "",
		ClientCertificateNotAfterReason: CapClientCertificateNotParsed,
	}
	if body.ClientCertificatePasswordID != "" {
		credential.ClientCertificateNotAfterReason = CapClientCertificatePasswordRequired
	}
	credential.CertificateAuthorityCount, credential.CertificateAuthorityNotBefore, credential.CertificateAuthorityNotAfter =
		certificateBundleValidity(body.CertificateAuthorityBase64)
	return credential, resp, nil
}

// certificateBundleValidity reads a base64 CA bundle the way Forward does
// (AwsC2SAuthenticator.loadX509Certificates: the base64 of PEM text, nothing
// else; a bare DER certificate is NOT loaded by Forward, so it is not counted
// here either) and reports how many certificates parsed, the latest NotBefore
// and the earliest NotAfter. Forward keys certificates by lowercased subject, so
// a later certificate with the same subject replaces an earlier one; so here.
// Anything unreadable counts as absent, never an error, and no byte of the
// bundle outlives the call.
func certificateBundleValidity(encoded string) (count int, notBefore, notAfter time.Time) {
	encoded = strings.Join(strings.Fields(encoded), "")
	if encoded == "" {
		return 0, time.Time{}, time.Time{}
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(encoded)
		if err != nil {
			return 0, time.Time{}, time.Time{}
		}
	}
	var certs []*x509.Certificate
	bySubject := map[string]int{}
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		subject := strings.ToLower(string(cert.RawSubject))
		if i, seen := bySubject[subject]; seen {
			certs[i] = cert
			continue
		}
		bySubject[subject] = len(certs)
		certs = append(certs, cert)
	}
	for _, cert := range certs {
		if count == 0 || cert.NotAfter.Before(notAfter) {
			notAfter = cert.NotAfter
		}
		if count == 0 || cert.NotBefore.After(notBefore) {
			notBefore = cert.NotBefore
		}
		count++
	}
	return count, notBefore.UTC(), notAfter.UTC()
}
