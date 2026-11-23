package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

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

// Body returns the JSON object Forward reads, with id when replacing.
func (d CollectionScheduleDefinition) Body(id string) ([]byte, error) {
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
