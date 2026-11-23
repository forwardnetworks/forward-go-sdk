package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"strings"
)

// ResetKey issues a new password for the named collector, keeping its username,
// and invalidates the old one at once: a running collector stops authenticating
// until it is given the new token. POST /api/orgs/current/collectors/resetPassword
// (ClientController.resetCollectorPassword, @RequiresUser; the appserver then
// requires the caller to be allowed to modify that collector). Preview.
func (s *Collectors) ResetKey(ctx context.Context, collectorName string) (*ports.CollectorCredentials, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	collectorName = strings.TrimSpace(collectorName)
	if collectorName == "" {
		return nil, nil, errors.New("forward: collector name is required")
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, "/api/orgs/current/collectors/resetPassword", ports.CollectorCredentials{CollectorName: collectorName})
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Collectors.ResetKey")
	out := new(ports.CollectorCredentials)
	resp, err := s.client.doRequired(req, out)
	if err == nil && (strings.TrimSpace(out.Username) == "" || out.Password == "") {
		err = errors.New("forward: collector password reset returned no credentials")
	}
	return out, resp, err
}
