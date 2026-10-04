package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCollectionSchedulesCreateReplaceDelete(t *testing.T) {
	t.Parallel()

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		switch {
		case r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"id":"7","enabled":true,"timeZone":"America/Chicago","daysOfTheWeek":[1,2,3,4,5],"times":["02:00","14:30"]}`)
		case r.URL.Path == "/api/networks/N1/collection-schedules/gone":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	schedules := newTestClient(t, server.URL).CollectionSchedules
	ctx := context.Background()

	created, _, err := schedules.Create(ctx, "N1", CollectionScheduleDefinition{Enabled: true, TimeZone: "America/Chicago", DaysOfTheWeek: []int{1, 2, 3, 4, 5}, Times: []string{"02:00", "14:30"}})
	if err != nil || created.ID != "7" || calls[0] != `POST /api/networks/N1/collection-schedules {"daysOfTheWeek":[1,2,3,4,5],"enabled":true,"timeZone":"America/Chicago","times":["02:00","14:30"]}` {
		t.Fatalf("Create() = %+v, %v; %q", created, err, calls[0])
	}
	// Replace restates every field and puts the id in the body as well as the
	// path, which Forward requires to match.
	periodic := created.Definition()
	periodic.Times, periodic.PeriodInSeconds, periodic.StartAt, periodic.EndAt = nil, Ptr(3600), "08:00", "18:00"
	if _, err := schedules.Replace(ctx, "N1", "7", periodic); err != nil ||
		calls[1] != `PUT /api/networks/N1/collection-schedules/7 {"daysOfTheWeek":[1,2,3,4,5],"enabled":true,"endAt":"18:00","id":"7","periodInSeconds":3600,"startAt":"08:00","timeZone":"America/Chicago"}` {
		t.Fatalf("Replace: %v %q", err, calls[1])
	}
	if _, err := schedules.Delete(ctx, "N1", "7"); err != nil || calls[2] != "DELETE /api/networks/N1/collection-schedules/7 " {
		t.Fatalf("Delete: %v %q", err, calls[2])
	}
	if _, err := schedules.Delete(ctx, "N1", "gone"); err != nil {
		t.Fatalf("deleting a missing schedule must be success: %v", err)
	}
}

func TestCollectionScheduleDefinitionRefusals(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	schedules := newTestClient(t, server.URL).CollectionSchedules
	ctx := context.Background()
	days := []int{0, 6}

	for name, d := range map[string]CollectionScheduleDefinition{
		"no days":                  {Times: []string{"02:00"}},
		"day out of range":         {DaysOfTheWeek: []int{7}, Times: []string{"02:00"}},
		"negative day":             {DaysOfTheWeek: []int{-1}, Times: []string{"02:00"}},
		"neither times nor period": {DaysOfTheWeek: days},
		"both times and period":    {DaysOfTheWeek: days, Times: []string{"02:00"}, PeriodInSeconds: Ptr(60)},
		"bad time":                 {DaysOfTheWeek: days, Times: []string{"2:00"}},
		"hour 24":                  {DaysOfTheWeek: days, Times: []string{"24:00"}},
		"zero period":              {DaysOfTheWeek: days, PeriodInSeconds: Ptr(0)},
		"start with times":         {DaysOfTheWeek: days, Times: []string{"02:00"}, StartAt: "01:00"},
		"bad end":                  {DaysOfTheWeek: days, PeriodInSeconds: Ptr(60), EndAt: "6pm"},
	} {
		if _, _, err := schedules.Create(ctx, "N1", d); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if _, err := schedules.Replace(ctx, "N1", " ", CollectionScheduleDefinition{DaysOfTheWeek: days, Times: []string{"02:00"}}); err == nil {
		t.Error("a blank schedule ID must be refused")
	}
	if _, err := schedules.Delete(ctx, "N1", ""); err == nil {
		t.Error("a blank schedule ID must never reach the collection route")
	}
	if calls != 0 {
		t.Fatalf("refused requests reached the wire %d times", calls)
	}
}
