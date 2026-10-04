package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// CollectionSchedulesService reads and writes a network's collection schedules: when
// Forward starts collections on its own. The published Collection Schedules
// API (collection-schedules.yaml), served on primary 15398425a69 and stable
// 67e89c87124.
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

// CollectionScheduleDefinition is a schedule to create or replace. It is
// either time-of-day (Times) or periodic (PeriodInSeconds, optionally within
// StartAt and EndAt), never both. Forward's rules, checked here first:
// DaysOfTheWeek is never empty and holds 0 (Sun) to 6 (Sat); Times, StartAt and
// EndAt are "HH:mm"; PeriodInSeconds is positive; StartAt and EndAt belong to a
// periodic schedule only. TimeZone, when set, must be one of the IDs in
// Forward's TimeZone list (UTC and about fifty regional zones); empty means the
// organization's preferred zone. Enabled false keeps the schedule but stops it
// running. Forward delays a scheduled collection that cannot start on time
// because an earlier one is still running, since a collector performs one
// network collection at a time.
type CollectionScheduleDefinition struct {
	Enabled         bool
	TimeZone        string
	DaysOfTheWeek   []int
	Times           []string
	PeriodInSeconds *int
	StartAt         string
	EndAt           string
}

var scheduleClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (d CollectionScheduleDefinition) check() error {
	if len(d.DaysOfTheWeek) == 0 {
		return errors.New("forward: a collection schedule needs at least one day of the week")
	}
	for _, day := range d.DaysOfTheWeek {
		if day < 0 || day > 6 {
			return fmt.Errorf("forward: day of the week %d must be 0 (Sun) to 6 (Sat)", day)
		}
	}
	if (len(d.Times) == 0) == (d.PeriodInSeconds == nil) {
		return errors.New("forward: a collection schedule needs exactly one of Times or PeriodInSeconds")
	}
	for _, t := range d.Times {
		if !scheduleClock.MatchString(t) {
			return fmt.Errorf("forward: schedule time %q must be HH:mm", t)
		}
	}
	if d.PeriodInSeconds != nil {
		if *d.PeriodInSeconds <= 0 {
			return errors.New("forward: PeriodInSeconds must be positive")
		}
	} else if d.StartAt != "" || d.EndAt != "" {
		return errors.New("forward: StartAt and EndAt apply only to a periodic schedule")
	}
	for name, v := range map[string]string{"StartAt": d.StartAt, "EndAt": d.EndAt} {
		if v != "" && !scheduleClock.MatchString(v) {
			return fmt.Errorf("forward: %s %q must be HH:mm", name, v)
		}
	}
	return nil
}

// body returns the JSON object Forward reads, with id when replacing.
func (d CollectionScheduleDefinition) body(id string) ([]byte, error) {
	if err := d.check(); err != nil {
		return nil, err
	}
	out := map[string]any{"enabled": d.Enabled, "daysOfTheWeek": d.DaysOfTheWeek}
	if id != "" {
		out["id"] = id
	}
	if d.TimeZone != "" {
		out["timeZone"] = d.TimeZone
	}
	if len(d.Times) != 0 {
		out["times"] = d.Times
	}
	if d.PeriodInSeconds != nil {
		out["periodInSeconds"] = *d.PeriodInSeconds
	}
	if d.StartAt != "" {
		out["startAt"] = d.StartAt
	}
	if d.EndAt != "" {
		out["endAt"] = d.EndAt
	}
	return json.Marshal(out)
}

// Definition returns the schedule as a definition, for a read-modify-write
// through Replace.
func (c CollectionSchedule) Definition() CollectionScheduleDefinition {
	return CollectionScheduleDefinition{
		Enabled: c.Enabled, TimeZone: c.TimeZone, DaysOfTheWeek: append([]int(nil), c.DaysOfTheWeek...),
		Times: append([]string(nil), c.Times...), PeriodInSeconds: c.PeriodInSeconds, StartAt: c.StartAt, EndAt: c.EndAt,
	}
}

func (s *CollectionSchedulesService) schedulePath(networkID, scheduleID string) (string, error) {
	networkID, err := s.client.resolveNetworkID(networkID)
	if err != nil {
		return "", err
	}
	path := "/api/networks/" + url.PathEscape(networkID) + "/collection-schedules"
	if scheduleID == "" {
		return path, nil
	}
	return path + "/" + url.PathEscape(scheduleID), nil
}

// Create adds a collection schedule to the network and returns it with the ID
// Forward gave it. POST /api/networks/{id}/collection-schedules
// (addCollectionSchedule; EDIT_COLLECTION_SCHEDULES). Forward answers 400 for a
// duplicate of an existing schedule. A network can have several schedules.
func (s *CollectionSchedulesService) Create(ctx context.Context, networkID string, definition CollectionScheduleDefinition) (*CollectionSchedule, *Response, error) {
	body, err := definition.body("")
	if err != nil {
		return nil, nil, err
	}
	path, err := s.schedulePath(networkID, "")
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, json.RawMessage(body))
	if err != nil {
		return nil, nil, err
	}
	req = markOperation(req, "CollectionSchedules.Create")
	out := new(CollectionSchedule)
	resp, err := s.client.doRequired(req, out)
	if err != nil {
		return nil, resp, err
	}
	return out, resp, nil
}

// Replace overwrites the schedule with schedule.ID by the given definition: it
// replaces, not merges, so every field is restated. To change one field, read
// the schedule, edit schedule.Definition() and send it back. PUT
// /api/networks/{id}/collection-schedules/{scheduleId} (replaceCollectionSchedule;
// EDIT_COLLECTION_SCHEDULES). Forward requires the ID in the path and in the
// body to match, which this does by construction, and answers 400 for a
// duplicate of another schedule.
func (s *CollectionSchedulesService) Replace(ctx context.Context, networkID, scheduleID string, definition CollectionScheduleDefinition) (*Response, error) {
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		return nil, errors.New("forward: collection schedule ID is required")
	}
	body, err := definition.body(scheduleID)
	if err != nil {
		return nil, err
	}
	path, err := s.schedulePath(networkID, scheduleID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPut, path, json.RawMessage(body))
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CollectionSchedules.Replace")
	return s.client.Do(req, nil)
}

// Delete removes a collection schedule; the network then collects only on the
// schedules it has left, or never if it had one. A schedule that does not
// exist counts as success. DELETE
// /api/networks/{id}/collection-schedules/{scheduleId}
// (deleteCollectionSchedule; EDIT_COLLECTION_SCHEDULES; 204).
func (s *CollectionSchedulesService) Delete(ctx context.Context, networkID, scheduleID string) (*Response, error) {
	if scheduleID = strings.TrimSpace(scheduleID); scheduleID == "" {
		// Never collapse onto the collection route.
		return nil, errors.New("forward: collection schedule ID is required")
	}
	path, err := s.schedulePath(networkID, scheduleID)
	if err != nil {
		return nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	req = markOperation(req, "CollectionSchedules.Delete")
	resp, err := s.client.Do(req, nil)
	if isStatus(err, http.StatusNotFound) {
		return resp, nil
	}
	return resp, err
}
