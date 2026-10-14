package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// AWSCapCredential is what a read of an AWS C2S Access Portal (CAP) credential
// keeps: its id, the portal's base URL, and whether a certificate authority is
// configured for it. The credential also holds the client's .p12 certificate
// and the CA's bytes, which Forward returns on the same read; neither is
// decoded into this type, so no String, GoString, %v or JSON of it can carry
// them. This SDK reads CAP credentials only to show which portal a setup uses;
// it does not create or edit them.
type AWSCapCredential struct {
	ID                      string `json:"id"`
	BaseCapServerURL        string `json:"baseCapServerUrl"`
	HasCertificateAuthority bool   `json:"hasCertificateAuthority"`
}

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
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	return &AWSCapCredential{
		ID: body.ID, BaseCapServerURL: body.BaseCapServerURL,
		HasCertificateAuthority: strings.TrimSpace(body.CertificateAuthorityBase64) != "",
	}, resp, nil
}
