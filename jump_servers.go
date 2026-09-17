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
type JumpServerRequest struct {
	Host        string `json:"host"`
	Port        int    `json:"port,omitempty"`
	Username    string `json:"username"`
	PrivateKey  string `json:"privateKey,omitempty"`
	Certificate string `json:"certificate,omitempty"`
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

func (s *JumpServersService) Create(ctx context.Context, networkID string, input JumpServerRequest) (*JumpServer, *Response, error) {
	return s.create(ctx, networkID, "/jump-servers", input, "JumpServers.Create")
}
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
