package domain

type JumpServer struct {
	ID       Identifier `json:"id"`
	Host     string     `json:"host,omitempty"`
	Port     int        `json:"port,omitempty"`
	Username string     `json:"username,omitempty"`
}

// JumpServerRequest is a key-authenticated jump server, in the shape of
// Forward's NewJumpServer (api/schemas/jump-servers/NewJumpServer.yaml):
// sshKey is the private key, sshCert the optional user certificate, and port
// defaults to 22 server-side when omitted.
type JumpServerRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username"`
	SSHKey   string `json:"sshKey"`
	SSHCert  string `json:"sshCert,omitempty"`
}

type LegacyJumpServerRequest struct {
	Host                   string `json:"host"`
	Username               string `json:"username"`
	SSHKey                 string `json:"sshKey"`
	SSHCert                string `json:"sshCert,omitempty"`
	SupportsPortForwarding bool   `json:"supportsPortForwarding"`
}

// JumpServerPasswordRequest is a jump server the collector logs in to with a
// password rather than a key, forwarding device connections through it.
type JumpServerPasswordRequest struct {
	Host                   string `json:"host"`
	Port                   int    `json:"port,omitempty"`
	Username               string `json:"username"`
	Password               string `json:"password"`
	SupportsPortForwarding bool   `json:"supportsPortForwarding"`
}

// JumpServerUpdate changes part of a jump server; an unset field is left alone. Password, SSHKey and SSHCert are
// write-only: Forward never returns them and the SDK never logs them. Shape of Forward's JumpServerUpdate
// (NetworkSetupController.editJumpServer, EDIT_JUMP_SERVERS).
type JumpServerUpdate struct {
	Host                         *string `json:"host,omitempty"`
	Port                         *int    `json:"port,omitempty"`
	Username                     *string `json:"username,omitempty"`
	Password                     *string `json:"password,omitempty"`
	SSHKey                       *string `json:"sshKey,omitempty"`
	SSHCert                      *string `json:"sshCert,omitempty"`
	SupportsPortForwarding       *bool   `json:"supportsPortForwarding,omitempty"`
	VRF                          *string `json:"vrf,omitempty"`
	AuthenticationTimeoutSeconds *int    `json:"authenticationTimeoutSeconds,omitempty"`
	MaxSessions                  *int    `json:"maxSessions,omitempty"`
	MaxStartups                  *int    `json:"maxStartups,omitempty"`
}
