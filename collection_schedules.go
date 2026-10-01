package forward

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// CollectionSchedulesService reads a network's collection schedules: when
// Forward starts collections on its own. The published Collection Schedules
// API (collection-schedules.yaml), served on primary 15398425a69 and stable
// 67e89c87124. Read-only here.
type CollectionSchedulesService service

// CollectionSchedule is one schedule, in one of two forms. A time-of-day
// schedule sets Times ("HH:mm" starts); a periodic one sets PeriodInSeconds
// (the gap after the last collection started), optionally within StartAt and
// EndAt. DaysOfTheWeek is Sun (0) .. Sat (6). TimeZone interprets the times
// and day boundaries; empty means the organization's preferred zone. Forward
// does not report the next run: compute it from these fields if needed.
type CollectionSchedule struct {
	ID              Identifier `json:"id"`
	Enabled         bool       `json:"enabled"`
	TimeZone        string     `json:"timeZone,omitempty"`
	DaysOfTheWeek   []int      `json:"daysOfTheWeek,omitempty"`
	Times           []string   `json:"times,omitempty"`
	PeriodInSeconds *int       `json:"periodInSeconds,omitempty"`
	StartAt         string     `json:"startAt,omitempty"`
	EndAt           string     `json:"endAt,omitempty"`
}

// Periodic reports whether the schedule is rate-based rather than a list of
// times of day.
func (c CollectionSchedule) Periodic() bool { return c.PeriodInSeconds != nil }

// List returns the network's collection schedules. GET
// /api/networks/{id}/collection-schedules (getCollectionSchedules).
func (s *CollectionSchedulesService) List(ctx context.Context, networkID string) ([]CollectionSchedule, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/collection-schedules", nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.List")
	result := listResponse[CollectionSchedule]{Keys: []string{"schedules"}}
	resp, err := s.client.doRequired(req, &result)
	return result.Items, resp, err
}

// Get returns one collection schedule, or (nil, nil) when it does not exist.
// GET /api/networks/{id}/collection-schedules/{scheduleId}
// (getCollectionSchedule).
func (s *CollectionSchedulesService) Get(ctx context.Context, networkID, scheduleID string) (*CollectionSchedule, *Response, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return nil, nil, err
	}
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		return nil, nil, errors.New("forward: collection schedule ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, "/api/networks/"+url.PathEscape(networkID)+"/collection-schedules/"+url.PathEscape(scheduleID), nil)
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.Get")
	out := new(CollectionSchedule)
	resp, err := s.client.doRequired(req, out)
	if isStatus(err, http.StatusNotFound) {
		return nil, resp, nil
	}
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}
