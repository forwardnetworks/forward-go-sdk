package usecases

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// Access reads the calling credential's own session roles (GET /api/users/current, Users.CurrentSession) and returns a checker over
// table (nil allowed). It is one request; the checks that follow make none.
func (s *Users) Access(ctx context.Context, table *domain.AccessTable) (*domain.Access, *ports.Response, error) {
	if err := s.failClosed(); err != nil {
		return nil, nil, err
	}
	session, resp, err := s.CurrentSession(ctx)
	if err != nil {
		return nil, resp, err
	}
	return domain.NewAccessForSession(table, session), resp, nil
}
