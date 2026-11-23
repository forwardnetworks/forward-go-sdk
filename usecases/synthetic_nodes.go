package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// The per-resource methods below are the named surface the coverage catalog
// maps route-by-route. They are thin: the behaviour lives in the kind-based
// calls above, which is what a caller iterating over kinds should use.

// GetInternetNode returns the network's single internet node, or nil if it
// has none.
func (s *SyntheticNodes) GetInternetNode(ctx context.Context, networkID string) (*domain.SyntheticNode, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.Get(ctx, networkID, domain.SyntheticInternet, "")
}

// PutInternetNode creates or replaces the network's internet node.
func (s *SyntheticNodes) PutInternetNode(ctx context.Context, networkID string, node domain.SyntheticNode) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.Put(ctx, networkID, domain.SyntheticInternet, "", node)
}

// ListIntranetNodes returns the network's intranet nodes.
func (s *SyntheticNodes) ListIntranetNodes(ctx context.Context, networkID string) ([]domain.SyntheticNode, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.List(ctx, networkID, domain.SyntheticIntranet)
}

// GetIntranetNode returns one intranet node, or nil if absent.
func (s *SyntheticNodes) GetIntranetNode(ctx context.Context, networkID, name string) (*domain.SyntheticNode, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.Get(ctx, networkID, domain.SyntheticIntranet, name)
}

// PutIntranetNode creates or replaces one intranet node.
func (s *SyntheticNodes) PutIntranetNode(ctx context.Context, networkID, name string, node domain.SyntheticNode) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.Put(ctx, networkID, domain.SyntheticIntranet, name, node)
}

// DeleteIntranetNode removes one intranet node. A 404 is success.
func (s *SyntheticNodes) DeleteIntranetNode(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.Delete(ctx, networkID, domain.SyntheticIntranet, name)
}

// ListL3VPNs returns the network's L3 VPNs.
func (s *SyntheticNodes) ListL3VPNs(ctx context.Context, networkID string) ([]domain.SyntheticNode, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.List(ctx, networkID, domain.SyntheticL3VPN)
}

// GetL3VPN returns one L3 VPN, or nil if absent.
func (s *SyntheticNodes) GetL3VPN(ctx context.Context, networkID, name string) (*domain.SyntheticNode, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	return s.Get(ctx, networkID, domain.SyntheticL3VPN, name)
}

// PutL3VPN creates or replaces one L3 VPN.
func (s *SyntheticNodes) PutL3VPN(ctx context.Context, networkID, name string, node domain.SyntheticNode) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.Put(ctx, networkID, domain.SyntheticL3VPN, name, node)
}

// DeleteL3VPN removes one L3 VPN. A 404 is success.
func (s *SyntheticNodes) DeleteL3VPN(ctx context.Context, networkID, name string) (*ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, err
	}
	return s.Delete(ctx, networkID, domain.SyntheticL3VPN, name)
}
