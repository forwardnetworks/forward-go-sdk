package usecases

import (
	"context"
	"errors"
	"fmt"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// Get returns one cloud setup by name.
//
// Forward exposes no per-name read, so this filters the list. The name is the
// setup's identity for every other call -- update, credential rotation,
// delete -- so a caller holding only a name can still read it back.
func (s *CloudAccounts) Get(ctx context.Context, networkID, name string) (*domain.CloudAccount, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, nil, errors.New("forward: cloud account name is required")
	}
	accounts, resp, err := s.List(ctx, networkID)
	if err != nil {
		return nil, resp, err
	}
	for i := range accounts {
		if accounts[i].Name == name {
			return &accounts[i], resp, nil
		}
	}
	return nil, resp, fmt.Errorf("%w: %s", domain.ErrCloudAccountNotFound, name)
}
