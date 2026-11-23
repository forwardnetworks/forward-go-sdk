package domain

// CLICredentialRequest creates a CLI login, privilege escalation, expert, or
// shell credential. Password is write-only secret material.
type CLICredentialRequest struct {
	// Type is OMITTED when empty, deliberately. Absent means LOGIN, which is
	// what Forward defaults to; an empty STRING is a different thing entirely
	// and fails enum deserialization with a 400.
	Type                     string `json:"type,omitempty"`
	Name                     string `json:"name"`
	Username                 string `json:"username,omitempty"`
	Password                 string `json:"password"`
	PrivilegedModePasswordID string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32 `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool  `json:"autoAssociate,omitempty"`
}

// CLICredential is a stored CLI credential. PasswordID is an opaque identifier
// substituted by Forward and is not secret material usable for authentication.
type CLICredential struct {
	ID                       string `json:"id,omitempty"`
	Type                     string `json:"type"`
	Name                     string `json:"name"`
	Username                 string `json:"username,omitempty"`
	PasswordID               string `json:"password"`
	PrivilegedModePasswordID string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32 `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool  `json:"autoAssociate,omitempty"`
	CreatedBy                string `json:"createdBy,omitempty"`
	CreatedAt                string `json:"createdAt,omitempty"`
}

// SNMP credentials, typed from Forward's own model (SnmpCredential.java).
//
// The version decides which of the two credential halves is legal, and the
// server enforces it rather than ignoring the wrong one:
//
//	V2C -> communityString applies; authSettings is NOT supported
//	V3  -> authSettings is REQUIRED; communityString is NOT supported
//
// There is no V1. Version itself is required -- the constructor does
// checkNotNull on it -- so it is sent even when empty would have been
// tempting, and the zero value is not silently meaningful.
type SNMPVersion string

const (
	SNMPVersionV2C SNMPVersion = "V2C"
	SNMPVersionV3  SNMPVersion = "V3"
)

// SNMPAuthType is the v3 authentication digest. MD5 and SHA are SHA-1 era;
// the SHA_2xx family is what current devices expect.
type SNMPAuthType string

const (
	SNMPAuthMD5    SNMPAuthType = "MD5"
	SNMPAuthSHA    SNMPAuthType = "SHA"
	SNMPAuthSHA256 SNMPAuthType = "SHA_256"
	SNMPAuthSHA384 SNMPAuthType = "SHA_384"
	SNMPAuthSHA512 SNMPAuthType = "SHA_512"
)

// SNMPPrivacyProtocol is the v3 privacy cipher. AES_192 and AES_256 are the
// extended-key variants some vendors ship, not RFC 3826 standard.
type SNMPPrivacyProtocol string

const (
	SNMPPrivacyDES    SNMPPrivacyProtocol = "DES"
	SNMPPrivacyAES128 SNMPPrivacyProtocol = "AES_128"
	SNMPPrivacyAES192 SNMPPrivacyProtocol = "AES_192"
	SNMPPrivacyAES256 SNMPPrivacyProtocol = "AES_256"
	// The TDES variants are for devices with broken AES 192/256 key extension (Forward's PrivAES192With3DESKeyExtension).
	SNMPPrivacyTDES       SNMPPrivacyProtocol = "TDES"
	SNMPPrivacyAES192TDES SNMPPrivacyProtocol = "AES_192_TDES"
	SNMPPrivacyAES256TDES SNMPPrivacyProtocol = "AES_256_TDES"
)

// SNMPAuthSettings is the v3 half. The fields are conditionally dependent, in
// Forward's words: password applies iff authType is set, privacyProtocol
// applies iff privacyPassword is set AND requires authType. Username is the
// only unconditional field and cannot be blank.
type SNMPAuthSettings struct {
	Username        string              `json:"username"`
	Password        string              `json:"password,omitempty"`
	AuthType        SNMPAuthType        `json:"authType,omitempty"`
	PrivacyProtocol SNMPPrivacyProtocol `json:"privacyProtocol,omitempty"`
	PrivacyPassword string              `json:"privacyPassword,omitempty"`
}

// SNMPCredentialRequest creates an SNMP credential.
type SNMPCredentialRequest struct {
	Name            string            `json:"name,omitempty"`
	Version         SNMPVersion       `json:"version"`
	Port            *int              `json:"port,omitempty"`
	TimeoutSec      *int              `json:"timeoutSec,omitempty"`
	CommunityString string            `json:"communityString,omitempty"`
	AuthSettings    *SNMPAuthSettings `json:"authSettings,omitempty"`
	AutoAssociate   *bool             `json:"autoAssociate,omitempty"`
}

// SNMPCredential is a stored SNMP credential. Forward returns its secrets
// (the community string and the two V3 passwords) as secret IDs, never as
// values, and this type does not carry them.
type SNMPCredential struct {
	ID      string      `json:"id,omitempty"`
	Name    string      `json:"name,omitempty"`
	Version SNMPVersion `json:"version,omitempty"`
	// Port and TimeoutSec are marked deprecated on Forward's credential record (161 and 5 seconds by default).
	Port          *int  `json:"port,omitempty"`
	TimeoutSec    *int  `json:"timeoutSec,omitempty"`
	AutoAssociate *bool `json:"autoAssociate,omitempty"`
	// AuthSettings is the V3 half without its secrets (the community string and the two passwords come back only as secret IDs, which this type does not carry); nil for V2C.
	AuthSettings *SNMPStoredAuthSettings `json:"authSettings,omitempty"`
	CreatedBy    string                  `json:"createdBy,omitempty"`
	CreatedAt    string                  `json:"createdAt,omitempty"`
}

// SNMPStoredAuthSettings is what a stored V3 credential shows of its authentication settings. AuthType is set when a password is,
// and PrivacyProtocol when a privacy password is.
type SNMPStoredAuthSettings struct {
	Username        string              `json:"username,omitempty"`
	AuthType        SNMPAuthType        `json:"authType,omitempty"`
	PrivacyProtocol SNMPPrivacyProtocol `json:"privacyProtocol,omitempty"`
}

// HTTPCredentialRequest creates an HTTP login or API-key credential.
type HTTPCredentialRequest struct {
	// Omitted when empty, for the same reason as the CLI request: Forward
	// defaults an absent type to LOGIN (NewHttpCredential.getType()), while an
	// empty STRING fails enum deserialization.
	Type          string `json:"type,omitempty"`
	Name          string `json:"name"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password"`
	LoginType     string `json:"loginType,omitempty"`
	AutoAssociate *bool  `json:"autoAssociate,omitempty"`
}

// HTTPCredential is a stored HTTP credential.
type HTTPCredential struct {
	ID            string `json:"id,omitempty"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Username      string `json:"username,omitempty"`
	PasswordID    string `json:"password"`
	LoginType     string `json:"loginType,omitempty"`
	AutoAssociate *bool  `json:"autoAssociate,omitempty"`
	CreatedBy     string `json:"createdBy,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
}

// CLICredentialPatch changes part of a stored CLI credential. Every field is a
// pointer so that "not stated" and "stated as empty" stay distinguishable: a
// patch that clears a username and one that leaves it alone are different
// requests.
type CLICredentialPatch struct {
	Name                     *string `json:"name,omitempty"`
	Username                 *string `json:"username,omitempty"`
	Password                 *string `json:"password,omitempty"`
	PrivilegedModePasswordID *string `json:"privilegedModePasswordId,omitempty"`
	PrivilegeLevel           *int32  `json:"privilegeLevel,omitempty"`
	AutoAssociate            *bool   `json:"autoAssociate,omitempty"`
}

// HTTPCredentialPatch changes part of a stored HTTP credential.
type HTTPCredentialPatch struct {
	Name          *string `json:"name,omitempty"`
	Username      *string `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	LoginType     *string `json:"loginType,omitempty"`
	AutoAssociate *bool   `json:"autoAssociate,omitempty"`
}
