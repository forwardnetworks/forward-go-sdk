package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

type JumpServers service

// jumpServerKeyWire is what Create sends. NewJumpServer requires
// supportsPortForwarding to be true whenever sshKey is set; Forward defaults
// it to true, but the key lane states it rather than depend on that default.
type jumpServerKeyWire struct {
	ports.JumpServerRequest
	SupportsPortForwarding bool `json:"supportsPortForwarding"`
}

// List returns the network's jump servers.
func (s *JumpServers) List(ctx context.Context, networkID string) ([]ports.JumpServer, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	result := listResponse[ports.JumpServer]{Keys: []string{"jumpServers", "items", "data", "results"}, AllowSingle: true}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/jumpServers", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "JumpServers.List")
	response, err := s.client.doRequired(req, &result)
	return result.Items, response, err
}

// CreateWithPassword adds a password-authenticated jump server.
func (s *JumpServers) CreateWithPassword(ctx context.Context, networkID string, input ports.JumpServerPasswordRequest) (*ports.JumpServer, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
func (s *JumpServers) Create(ctx context.Context, networkID string, input ports.JumpServerRequest) (*ports.JumpServer, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
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
func (s *JumpServers) CreateLegacy(ctx context.Context, networkID string, input ports.LegacyJumpServerRequest) (*ports.JumpServer, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	return s.create(ctx, networkID, "/jumpServers", input, "JumpServers.CreateLegacy")
}

func (s *JumpServers) create(ctx context.Context, networkID, suffix string, input any, operation string) (*ports.JumpServer, *ports.Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/networks/"+url.PathEscape(networkID)+suffix, input)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, operation)
	out := new(ports.JumpServer)
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

// Update patches one jump server. A change makes Forward re-handle the devices that go through it.
// PATCH /api/networks/{networkId}/jumpServers/{jumpServerId} (NetworkSetupController.editJumpServer).
func (s *JumpServers) Update(ctx context.Context, networkID, jumpServerID string, patch ports.JumpServerUpdate) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
func (s *JumpServers) Delete(ctx context.Context, networkID, jumpServerID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
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
	if ports.IsGone(err) {
		return response, nil
	}
	return response, err
}

func (s *JumpServers) jumpServerPath(networkID, jumpServerID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	if jumpServerID = strings.TrimSpace(jumpServerID); jumpServerID == "" {
		return "", errors.New("forward: jump server id is required")
	}
	return "/api/networks/" + url.PathEscape(networkID) + "/jumpServers/" + url.PathEscape(jumpServerID), nil
}
