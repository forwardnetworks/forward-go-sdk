package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"strings"
)

// Get returns the decorator with this name (compared case-insensitively, as
// Forward does), or nil and no error when there is none. Forward has no
// single-decorator read, so this lists and filters.
func (s *Decorators) Get(ctx context.Context, name string) (*domain.Decorator, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	if err := domain.RequireDecoratorName(name); err != nil {
		return nil, nil, err
	}
	all, resp, err := s.List(ctx)
	if err != nil {
		return nil, resp, err
	}
	for i := range all {
		if strings.EqualFold(all[i].Name, name) {
			return &all[i], resp, nil
		}
	}
	return nil, resp, nil
}
