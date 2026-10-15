package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func bannerServer(t *testing.T, status int, body string, wantPath string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != wantPath {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.Path, wantPath)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// No announcement is {} (the controller returns an empty map), which must read
// as nil rather than a zero announcement; an on-prem deployment does not serve
// the route at all, and that 404 stays the typed "endpoint not served" error.
func TestAnnouncementIsNilWhenNonePostedAndTypedWhereNotServed(t *testing.T) {
	ctx := context.Background()
	none := bannerServer(t, 200, `{}`, "/api/system-announcement")
	got, _, err := newTestClient(t, none.URL).Banners.Announcement(ctx)
	if err != nil || got != nil {
		t.Fatalf("no announcement must be nil, nil: %+v, %v", got, err)
	}
	posted := bannerServer(t, 200, `{"announcement":{"message":"Upgrade Saturday","linkUrl":"https://status.example.com","createdAt":"2026-10-14T09:30:00Z"}}`, "/api/system-announcement")
	got, _, err = newTestClient(t, posted.URL).Banners.Announcement(ctx)
	if err != nil || got == nil || got.Message != "Upgrade Saturday" || got.LinkURL != "https://status.example.com" || !got.CreatedAt.Equal(time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("announcement = %+v, %v", got, err)
	}
	onPrem := bannerServer(t, 404, `{"message":"No endpoint GET /api/system-announcement."}`, "/api/system-announcement")
	if _, _, err = newTestClient(t, onPrem.URL).Banners.Announcement(ctx); !errors.Is(err, ErrEndpointNotServed) {
		t.Fatalf("an on-prem 404 must be ErrEndpointNotServed: %v", err)
	}
}

// SaaS sends MAINTENANCE_WINDOW as null or {start,end} in epoch milliseconds;
// on-prem sends the storage booleans. The window and the booleans are typed,
// Reported says which names this deployment sent, and Active follows the UI's
// priority order.
func TestCriticalStatesSaasAndOnPrem(t *testing.T) {
	ctx := context.Background()
	saas := bannerServer(t, 200, `{"MAINTENANCE_WINDOW":{"start":1792000000000,"end":1792003600000},"COLLECTOR_OUTDATED":true,"LICENSE_CCH_EXHAUSTED":false,"LICENSE_CCH_GRACE_PERIOD_ENDED":false}`, "/api/critical-states")
	states, _, err := newTestClient(t, saas.URL).Banners.CriticalStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states.MaintenanceWindow == nil || states.MaintenanceWindow.Start.UnixMilli() != 1792000000000 || states.MaintenanceWindow.End.Sub(states.MaintenanceWindow.Start) != time.Hour || !states.CollectorOutdated {
		t.Fatalf("saas states = %+v", states)
	}
	if want := []string{CriticalMaintenanceWindow, CriticalCollectorOutdated}; !reflect.DeepEqual(states.Active(), want) {
		t.Fatalf("Active = %v, want %v", states.Active(), want)
	}
	idle := bannerServer(t, 200, `{"MAINTENANCE_WINDOW":null,"COLLECTOR_OUTDATED":false}`, "/api/critical-states")
	states, _, err = newTestClient(t, idle.URL).Banners.CriticalStates(ctx)
	if err != nil || states.MaintenanceWindow != nil || len(states.Active()) != 0 || !reflect.DeepEqual(states.Reported, []string{"COLLECTOR_OUTDATED", "MAINTENANCE_WINDOW"}) {
		t.Fatalf("no window must be nil and the name still Reported: %+v, %v", states, err)
	}
	onPrem := bannerServer(t, 200, `{"LOW_STORAGE":true,"NO_STORAGE":false,"NO_BACKUP_STORAGE":true,"COLLECTOR_OUTDATED":false,"LICENSE_CCH_EXHAUSTED":true,"LICENSE_CCH_GRACE_PERIOD_ENDED":false}`, "/api/critical-states")
	states, _, err = newTestClient(t, onPrem.URL).Banners.CriticalStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{CriticalNoBackupStorage, CriticalLowStorage, CriticalLicenseCCHExhausted}; !reflect.DeepEqual(states.Active(), want) {
		t.Fatalf("on-prem Active = %v, want %v", states.Active(), want)
	}
}

func TestLicenseStatusCriticalStateMirrorsTheUI(t *testing.T) {
	for status, want := range map[string]string{
		"": CriticalUnlicensed, "EXPIRED": CriticalLicenseExpired, "EXPIRED_RECENTLY": CriticalLicenseExpired,
		"NOT_ACTIVE_YET": CriticalLicenseNotStartedYet, "ACTIVE": "", "EXEMPT": "", "SOFTWARE_CENTRAL": "",
	} {
		if got := LicenseStatusCriticalState(status); got != want {
			t.Errorf("LicenseStatusCriticalState(%q) = %q, want %q", status, got, want)
		}
	}
}

// A CriticalStates value must write the wire shape it reads, so printing it as
// JSON gives something Forward-shaped; a state this SDK does not model keeps
// its raw value; a JSON null window leaves a MaintenanceWindow untouched.
func TestCriticalStatesRoundTripAndKeepUnknownStates(t *testing.T) {
	const wire = `{"COLLECTOR_OUTDATED":true,"LICENSE_CCH_EXHAUSTED":false,"LICENSE_WILL_EXPIRE":true,"MAINTENANCE_WINDOW":{"start":1792000000000,"end":1792003600000}}`
	var states CriticalStates
	if err := json.Unmarshal([]byte(wire), &states); err != nil {
		t.Fatal(err)
	}
	if string(states.Unknown["LICENSE_WILL_EXPIRE"]) != "true" {
		t.Fatalf("unknown state lost: %+v", states.Unknown)
	}
	encoded, err := json.Marshal(states)
	if err != nil {
		t.Fatal(err)
	}
	var again CriticalStates
	if err := json.Unmarshal(encoded, &again); err != nil || !reflect.DeepEqual(states, again) {
		t.Fatalf("round trip differs: %s\n%+v\n%+v (%v)", encoded, states, again, err)
	}
	var null CriticalStates
	if err := json.Unmarshal([]byte(`{"MAINTENANCE_WINDOW":null,"LOW_STORAGE":null}`), &null); err != nil || null.MaintenanceWindow != nil || null.LowStorage {
		t.Fatalf("null states must read as inactive: %+v, %v", null, err)
	}
	window := MaintenanceWindow{Start: time.UnixMilli(5).UTC()}
	if err := json.Unmarshal([]byte(`null`), &window); err != nil || window.Start.UnixMilli() != 5 {
		t.Fatalf("a null window must not overwrite: %+v, %v", window, err)
	}
}
