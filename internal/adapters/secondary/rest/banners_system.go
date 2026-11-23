package rest

import (
	"context"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
)

// Announcement reads the current system announcement, or nil when none is
// posted. GET /api/system-announcement (SaasAnnouncementController, Preview;
// any signed-in user). The route exists on SaaS only (@ProfileCriteria(not =
// ON_PREM)); an on-premises deployment answers with its typed
// ErrEndpointNotServed 404, which is "no announcements possible", not a
// failure. Posting and deleting are operator-only and not modelled.
func (s *Banners) Announcement(ctx context.Context) (*ports.SystemAnnouncement, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/system-announcement", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.Announcement")
	var body struct {
		Announcement *ports.SystemAnnouncement `json:"announcement"`
	}
	resp, err := s.client.doRequired(req, &body)
	if err != nil {
		return nil, resp, err
	}
	return body.Announcement, resp, nil
}

// CriticalStates reads the org's critical states. GET /api/critical-states
// (NotificationController.getCriticalStates, Preview; any signed-in user).
func (s *Banners) CriticalStates(ctx context.Context) (*ports.CriticalStates, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/critical-states", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "Banners.CriticalStates")
	out := new(ports.CriticalStates)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
