package rest

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
	"time"
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
func (s *CloudAccounts) GetAWSCapCredential(ctx context.Context, networkID, credentialID string) (*ports.AWSCapCredential, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
	credential := &ports.AWSCapCredential{
		ID: body.ID, BaseCapServerURL: body.BaseCapServerURL,
		HasCertificateAuthority:         strings.TrimSpace(body.CertificateAuthorityBase64) != "",
		ClientCertificateNotAfterReason: ports.CapClientCertificateNotParsed,
	}
	if body.ClientCertificatePasswordID != "" {
		credential.ClientCertificateNotAfterReason = ports.CapClientCertificatePasswordRequired
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
