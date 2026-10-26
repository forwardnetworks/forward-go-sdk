package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// SNMPCredentialPatch changes part of a stored SNMP credential (Forward's SnmpCredentialUpdate, JsonProp semantics: a nil field
// is left alone, and a name in Clear is sent as an explicit null).
type SNMPCredentialPatch struct {
	Name *string
	// Version switches the credential between V2C and V3; empty leaves it. Forward keeps the stored half, so switching also needs the
	// new half set (a community string for V2C, AuthSettings for V3) AND the old one cleared (Clear "authSettings" or
	// "communityString"): V3 with a community string, and V2C with authSettings, are 400s.
	Version         SNMPVersion
	Port            *int
	TimeoutSec      *int
	CommunityString *string
	AuthSettings    *SNMPAuthSettingsPatch
	AutoAssociate   *bool
	// Clear names the properties to reset with an explicit JSON null: communityString, authSettings, port, timeoutSec,
	// autoAssociate. name and version cannot be cleared.
	Clear []string

	// The names below were never read by Forward. They are folded into CommunityString and AuthSettings when set, so a caller that set
	// them gets what it meant.

	// Deprecated: use CommunityString.
	Community *string `json:"-"`
	// Deprecated: use AuthSettings.Username.
	User *string `json:"-"`
	// Deprecated: use AuthSettings.AuthType.
	AuthenticationType *string `json:"-"`
	// Deprecated: use AuthSettings.Password.
	AuthenticationKey *string `json:"-"`
	// Deprecated: use AuthSettings.PrivacyProtocol.
	PrivacyType *string `json:"-"`
	// Deprecated: use AuthSettings.PrivacyPassword.
	PrivacyKey *string `json:"-"`
	// Deprecated: Forward has no such property; it is ignored.
	ContextName *string `json:"-"`
}

// SNMPAuthSettingsPatch changes part of a V3 credential's authentication settings (Forward's AuthSettingsUpdate). A nil field is
// left alone; Forward returns passwords only as secret IDs, never as values, so a password is only ever set or cleared, not read back.
type SNMPAuthSettingsPatch struct {
	Username        *string
	Password        *string
	AuthType        SNMPAuthType
	PrivacyProtocol SNMPPrivacyProtocol
	PrivacyPassword *string
	// Clear names the properties to reset with an explicit JSON null: password, authType, privacyProtocol, privacyPassword.
	Clear []string
}

var snmpCredentialPatchClearable = map[string]bool{
	"communityString": true, "authSettings": true, "port": true, "timeoutSec": true, "autoAssociate": true,
}

var snmpAuthSettingsPatchClearable = map[string]bool{
	"password": true, "authType": true, "privacyProtocol": true, "privacyPassword": true,
}

// MarshalJSON sends the set properties and a JSON null for each name in Clear.
func (a SNMPAuthSettingsPatch) MarshalJSON() ([]byte, error) {
	fields := map[string]any{}
	if a.Username != nil {
		fields["username"] = *a.Username
	}
	if a.Password != nil {
		fields["password"] = *a.Password
	}
	if a.AuthType != "" {
		fields["authType"] = a.AuthType
	}
	if a.PrivacyProtocol != "" {
		fields["privacyProtocol"] = a.PrivacyProtocol
	}
	if a.PrivacyPassword != nil {
		fields["privacyPassword"] = *a.PrivacyPassword
	}
	if err := applySNMPClears(fields, a.Clear, snmpAuthSettingsPatchClearable, "SNMP authentication settings"); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// MarshalJSON sends the set properties and a JSON null for each name in Clear.
func (p SNMPCredentialPatch) MarshalJSON() ([]byte, error) {
	fields := map[string]any{}
	if p.Name != nil {
		fields["name"] = *p.Name
	}
	if p.Version != "" {
		fields["version"] = p.Version
	}
	if p.Port != nil {
		fields["port"] = *p.Port
	}
	if p.TimeoutSec != nil {
		fields["timeoutSec"] = *p.TimeoutSec
	}
	community := p.CommunityString
	if community == nil {
		community = p.Community
	}
	if community != nil {
		fields["communityString"] = *community
	}
	auth := p.AuthSettings
	if auth == nil && (p.User != nil || p.AuthenticationType != nil || p.AuthenticationKey != nil || p.PrivacyType != nil || p.PrivacyKey != nil) {
		auth = &SNMPAuthSettingsPatch{Username: p.User, Password: p.AuthenticationKey, PrivacyPassword: p.PrivacyKey}
		if p.AuthenticationType != nil {
			auth.AuthType = SNMPAuthType(*p.AuthenticationType)
		}
		if p.PrivacyType != nil {
			auth.PrivacyProtocol = SNMPPrivacyProtocol(*p.PrivacyType)
		}
	}
	if auth != nil {
		fields["authSettings"] = auth
	}
	if p.AutoAssociate != nil {
		fields["autoAssociate"] = *p.AutoAssociate
	}
	if err := applySNMPClears(fields, p.Clear, snmpCredentialPatchClearable, "SNMP credential"); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func applySNMPClears(fields map[string]any, clear []string, clearable map[string]bool, what string) error {
	for _, name := range clear {
		if !clearable[name] {
			return fmt.Errorf("forward: %s property %q cannot be cleared with null", what, name)
		}
		if _, set := fields[name]; set && fields[name] != nil {
			return fmt.Errorf("forward: %s patch both sets and clears %q", what, name)
		}
		fields[name] = nil
	}
	return nil
}

// validate refuses what Forward would, so the message names the field.
func (p SNMPCredentialPatch) validate() error {
	switch p.Version {
	case "", SNMPVersionV2C, SNMPVersionV3:
	default:
		return errors.New("forward: SNMP credential version must be V2C or V3")
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return errors.New("forward: SNMP credential name must not be blank")
	}
	if p.TimeoutSec != nil && *p.TimeoutSec < 1 {
		return fmt.Errorf("forward: SNMP timeout %d seconds must be positive", *p.TimeoutSec)
	}
	if a := p.AuthSettings; a != nil && a.Username != nil && strings.TrimSpace(*a.Username) == "" {
		return errors.New("forward: SNMP V3 authSettings.username must not be blank")
	}
	return nil
}

// UpdateSNMPCredential changes part of a stored SNMP credential (PATCH /api/networks/{id}/snmpCredentials/{credentialId};
// CredentialController.patchSnmpCredential, SnmpCredentialUpdate; EDIT_DEVICE_CREDENTIALS; 204; Preview: not in the published
// spec). It is the typed form of
// UpdateSNMP, whose map the caller has to spell right. Forward checks the merged credential, so a patch that leaves a V3
// credential without a username, or a V2C one with authSettings, is a 400.
func (s *CredentialsService) UpdateSNMPCredential(ctx context.Context, networkID, credentialID string, patch SNMPCredentialPatch) (*Response, error) {
	if err := patch.validate(); err != nil {
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
