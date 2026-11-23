package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// UpdateSNMPCredential changes part of a stored SNMP credential (PATCH /api/networks/{id}/snmpCredentials/{credentialId};
// CredentialController.patchSnmpCredential, SnmpCredentialUpdate; EDIT_DEVICE_CREDENTIALS; 204; Preview: not in the published
// spec). It is the typed form of
// UpdateSNMP, whose map the caller has to spell right. Forward checks the merged credential, so a patch that leaves a V3
// credential without a username, or a V2C one with authSettings, is a 400.
func (s *Credentials) UpdateSNMPCredential(ctx context.Context, networkID, credentialID string, patch ports.SNMPCredentialPatch) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, err
	}
	path, err := credentialPath(networkID, "snmpCredentials", credentialID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "Credentials.UpdateSNMPCredential")
	return s.client.Do(req, nil)
}
