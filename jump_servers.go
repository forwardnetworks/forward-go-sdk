package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type JumpServersService service
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

// jumpServerKeyWire is what Create sends. NewJumpServer requires
// supportsPortForwarding to be true whenever sshKey is set; Forward defaults
// it to true, but the key lane states it rather than depend on that default.
type jumpServerKeyWire struct {
	JumpServerRequest
	SupportsPortForwarding bool `json:"supportsPortForwarding"`
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

// List returns the network's jump servers.
func (s *JumpServersService) List(ctx context.Context, networkID string) ([]JumpServer, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[JumpServer]{Keys: []string{"jumpServers", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/jumpServers", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "JumpServers.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// CreateWithPassword adds a password-authenticated jump server.
func (s *JumpServersService) CreateWithPassword(ctx context.Context, networkID string, input JumpServerPasswordRequest) (*JumpServer, *Response, error) {
	if err := validateJumpValue(input.Host); err != nil {
		return nil, nil, err
	}
	if err := validateJumpValue(input.Username); err != nil {
		return nil, nil, err
	}
	if err := validateJumpValue(input.Password); err != nil {
		return nil, nil, err
	}
	return s.create(ctx, networkID, "/jumpServers", input, "JumpServers.CreateWithPassword")
}

// Create adds a key-authenticated jump server
// (POST /api/networks/{networkId}/jumpServers).
//
// It used to POST /jump-servers with privateKey/certificate fields. Forward
// has no such route on any build this SDK targets -- NetworkSetupController
// maps only /jumpServers -- so every call 404'd.
func (s *JumpServersService) Create(ctx context.Context, networkID string, input JumpServerRequest) (*JumpServer, *Response, error) {
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.SSHKey = strings.TrimSpace(input.SSHKey)
	input.SSHCert = strings.TrimSpace(input.SSHCert)
	for _, value := range []string{input.Host, input.Username, input.SSHKey} {
		if err := validateJumpValue(value); err != nil {
			return nil, nil, err
		}
	}
	return s.create(ctx, networkID, "/jumpServers", jumpServerKeyWire{JumpServerRequest: input, SupportsPortForwarding: true}, "JumpServers.Create")
}

// CreateLegacy posts a caller-built legacy body to the same route as Create.
//
// Deprecated: use Create, which targets the same route with a validated
// NewJumpServer body.
func (s *JumpServersService) CreateLegacy(ctx context.Context, networkID string, input LegacyJumpServerRequest) (*JumpServer, *Response, error) {
	return s.create(ctx, networkID, "/jumpServers", input, "JumpServers.CreateLegacy")
}
func (s *JumpServersService) create(ctx context.Context, networkID, suffix string, input any, operation string) (*JumpServer, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+suffix, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(JumpServer)
	response, err := s.client.doRequired(req, out)
	if err == nil && out.ID == "" {
		err = errors.New("forward: jump server create returned no ID")
	}
	return out, response, err
}
func validateJumpValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("forward: jump server value is required")
	}
	return nil
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

// Update patches one jump server. A change makes Forward re-handle the devices that go through it.
// PATCH /api/networks/{networkId}/jumpServers/{jumpServerId} (NetworkSetupController.editJumpServer).
func (s *JumpServersService) Update(ctx context.Context, networkID, jumpServerID string, patch JumpServerUpdate) (*Response, error) {
	path, err := s.jumpServerPath(networkID, jumpServerID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPatch, path, patch)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "JumpServers.Update")
	return s.client.Do(req, nil)
}

// Delete removes one jump server; one that is already gone counts as success. A 404 that means the route is not
// served (ErrEndpointNotServed) is still an error.
// DELETE /api/networks/{networkId}/jumpServers/{jumpServerId} (NetworkSetupController.deleteJumpServer).
func (s *JumpServersService) Delete(ctx context.Context, networkID, jumpServerID string) (*Response, error) {
	path, err := s.jumpServerPath(networkID, jumpServerID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "JumpServers.Delete")
	response, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) && !errors.Is(err, ErrEndpointNotServed) {
		return response, nil
	}
	return response, err
}

func (s *JumpServersService) jumpServerPath(networkID, jumpServerID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	if jumpServerID = strings.TrimSpace(jumpServerID); jumpServerID == "" {
		return "", errors.New("forward: jump server id is required")
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/jumpServers/" + url.PathEscape(jumpServerID), nil
}
