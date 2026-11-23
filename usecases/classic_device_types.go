package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// CreateWithAPIKey creates an API_KEY HTTP credential and then a classic device that uses it: the recipe for the types whose HTTP
// credential is an API key (httpCredentialType API_KEY, such as k8s_cni_control_plane_api, where the key is the cluster's bearer
// token). The credential's Type is forced to API_KEY (it takes no username) and the device's HTTPCredentialID is the new
// credential's id; set neither yourself. It is not atomic: if the device create fails the credential stays, and the result still
// carries it with the error. Preview: composed from CreateHTTP and Create, and untested against a live Forward.
func (s *ClassicDevices) CreateWithAPIKey(ctx context.Context, networkID string, credential domain.HTTPCredentialRequest, device domain.ClassicDeviceRequest) (*domain.ClassicDeviceWithCredential, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if credential.Type != "" && !strings.EqualFold(credential.Type, "API_KEY") {
		return nil, nil, fmt.Errorf("forward: CreateWithAPIKey makes an API_KEY credential, not %q", credential.Type)
	}
	if device.HTTPCredentialID != "" {
		return nil, nil, errors.New("forward: CreateWithAPIKey sets the device's HTTP credential itself; leave HTTPCredentialID empty")
	}
	if err := device.Validate(); err != nil {
		return nil, nil, err
	}
	credential.Type = "API_KEY"
	created, resp, err := s.credentials.CreateHTTP(ctx, networkID, credential)
	if err != nil {
		return nil, resp, err
	}
	out := &domain.ClassicDeviceWithCredential{Credential: created}
	device.HTTPCredentialID = created.ID
	out.Device, resp, err = s.Create(ctx, networkID, device)
	return out, resp, err
}
