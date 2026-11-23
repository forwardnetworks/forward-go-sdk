package usecases

import (
	"context"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// Get returns one network's basic information. GET /api/networks/{id}
// (NetworkController.getNetwork; published from Forward 26.11, FWD-61722). A
// network that has expired is a 404. A build before 26.11 does not map GET on
// this path (only PATCH and DELETE) and answers 405, seen live on 26.10.5 with
// an HTML body; Get then reads the network from List instead (a 405, or a 404
// "No endpoint", is the signal; a plain 404 is returned as it is). The fallback
// returns ErrNetworkNotFound when the listing has no such network. From 26.11 this is
// also the way to read a network's own properties, because every property of the
// snapshot listing except its snapshots list is deprecated (FWD-61718, removal in
// 27.2).
func (s *Networks) Get(ctx context.Context, networkID string) (*domain.Network, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	networkID, err := domain.ResolveNetworkID(networkID, s.session.Network())
	if err != nil {
		return nil, nil, err
	}
	network, resp, err := s.byID.GetByID(ctx, networkID)
	if err != nil {
		if ports.IsStatus(err, http.StatusMethodNotAllowed) || ports.IsErrorKind(err, domain.ErrorKindEndpointNotServed) {
			return s.getFromList(ctx, networkID)
		}
		return nil, resp, err
	}
	return network, resp, nil
}

func (s *Networks) getFromList(ctx context.Context, networkID string) (*domain.Network, *ports.Response, error) {
	networks, resp, err := s.List(ctx)
	if err != nil {
		return nil, resp, err
	}
	for i := range networks {
		if string(networks[i].ID) == networkID {
			return &networks[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: %s", domain.ErrNetworkNotFound, networkID)
}
