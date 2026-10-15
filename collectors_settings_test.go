package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward serializes only what was set (@JsonInclude NON_EMPTY, defaults live
// in getters): an unset concurrency is absent, and must stay nil rather than
// read as 0 or as the default. That is what lets a caller compare a collector's
// configured concurrency with what it is actually running at.
func TestCollectorsGetSettings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/collectors/7/collection-settings":
			_, _ = io.WriteString(w, `{"concurrency":256}`)
		case "/api/collectors/8/collection-settings":
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	collectors := newTestClient(t, server.URL).Collectors
	ctx := context.Background()

	set, _, err := collectors.GetSettings(ctx, "7")
	if err != nil || set.Concurrency == nil || *set.Concurrency != 256 || set.EffectiveConcurrency() != 256 {
		t.Fatalf("GetSettings(7) = %+v, %v", set, err)
	}
	if set.SNMPCollectionConcurrency != nil || set.EffectiveSNMPCollectionConcurrency() != DefaultCollectorSNMPConcurrency {
		t.Fatalf("an unset SNMP concurrency must be nil with an explicit default: %+v", set)
	}
	empty, _, err := collectors.GetSettings(ctx, "8")
	if err != nil || empty.Concurrency != nil || empty.EffectiveConcurrency() != 128 || empty.EffectiveVCenterConcurrency() != 1 {
		t.Fatalf("GetSettings(8) = %+v, %v", empty, err)
	}
	if _, _, err := collectors.GetSettings(ctx, " "); err == nil {
		t.Fatal("an empty collector ID must be refused")
	}
}

func TestCollectorsGetOrganizationSettings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/collection-settings" {
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"maxDeviceAuthNPerSecond":50,"perDeviceConcurrencyBoost":2,"deviceCollectionTimeoutMinutes":90,
		  "commandDelayMs":250,"collectionRetries":0,"enableBetaCommands":false,"passwordPrompts":["Token:"],
		  "ribRouteLimit":5000,"disabledCommands":{"cisco_ios_xe_ssh":[{"command":"show tech","version":{"start":"1.0","end":"99.0"}}]}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Collectors.GetOrganizationSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxDeviceAuthNPerSecond == nil || *got.MaxDeviceAuthNPerSecond != 50 || got.EffectiveMaxDeviceAuthNPerSecond() != 50 ||
		got.DeviceCollectionTimeoutMins == nil || got.EffectiveDeviceCollectionTimeoutMinutes() != 90 || *got.CommandDelayMS != 250 || got.PasswordPrompts[0] != "Token:" {
		t.Fatalf("settings = %+v", got)
	}
	// An explicit zero or false is a setting, not an absence.
	if got.CollectionRetries == nil || *got.CollectionRetries != 0 || got.EnableBetaCommands == nil || *got.EnableBetaCommands {
		t.Fatalf("explicit zero/false must survive: retries=%v beta=%v", got.CollectionRetries, got.EnableBetaCommands)
	}
	if got.MaxScanConnectionsPerSecond != nil || got.EffectiveMaxScanConnectionsPerSecond() != 2000 || got.EffectiveSnapshotCollectionTimeoutMinutes() != 360 {
		t.Fatalf("unset fields must be nil with explicit defaults: %+v", got)
	}
	if string(got.Raw["ribRouteLimit"]) != "5000" || len(got.Raw["disabledCommands"]) == 0 {
		t.Fatalf("unmodelled settings must stay reachable in Raw: %v", got.Raw)
	}
}

// Disabling a built-in command org-wide goes through a patch whose
// disabledCommands REPLACES the stored map: nil must send nothing (leave it
// alone), an empty map must be sent (clear all), and an entry missing its
// command or a version bound must fail before the request, since Forward
// requires all three.
func TestOrgSettingsDisabledCommandsPatchAndRead(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"commandDelayMs":5,"disabledCommands":{"panos_ssh":[{"command":"show .*session","version":{"start":"1.0","end":"99.0"}}]}}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	collectors := newTestClient(t, server.URL).Collectors
	ctx := context.Background()

	settings, _, err := collectors.GetOrganizationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := settings.DisabledCommands["panos_ssh"]
	if len(got) != 1 || got[0].Command != "show .*session" || got[0].Version != (DisabledCommandVersion{Start: "1.0", End: "99.0"}) {
		t.Fatalf("disabled commands = %+v", settings.DisabledCommands)
	}

	add := map[string][]DisabledCommand{"f5_ssh": {{Command: "tmsh list", Version: DisabledCommandVersion{Start: "1.0", End: "99.0"}}}}
	empty := map[string][]DisabledCommand{}
	for _, patch := range []OrgCollectionSettingsPatch{
		{CommandDelayMS: Ptr(5)},
		{DisabledCommands: &add},
		{DisabledCommands: &empty},
	} {
		if _, err := collectors.PatchOrganizationSettings(ctx, patch); err != nil {
			t.Fatal(err)
		}
	}
	if len(bodies) != 3 {
		t.Fatalf("expected 3 PATCH bodies, got %d: %v", len(bodies), bodies)
	}
	want := []string{`{"commandDelayMs":5}`, `{"disabledCommands":{"f5_ssh":[{"command":"tmsh list","version":{"start":"1.0","end":"99.0"}}]}}`, `{"disabledCommands":{}}`}
	for i := range want {
		if bodies[i] != want[i] {
			t.Errorf("body %d = %s, want %s", i, bodies[i], want[i])
		}
	}

	sent := len(bodies)
	for name, bad := range map[string]map[string][]DisabledCommand{
		"no command": {"f5_ssh": {{Version: DisabledCommandVersion{Start: "1", End: "2"}}}},
		"no start":   {"f5_ssh": {{Command: "x", Version: DisabledCommandVersion{End: "2"}}}},
		"no end":     {"f5_ssh": {{Command: "x", Version: DisabledCommandVersion{Start: "1"}}}},
		"empty type": {"": {{Command: "x", Version: DisabledCommandVersion{Start: "1", End: "2"}}}},
	} {
		bad := bad
		if _, err := collectors.PatchOrganizationSettings(ctx, OrgCollectionSettingsPatch{DisabledCommands: &bad}); err == nil || len(bodies) != sent {
			t.Errorf("%s must fail before any request (err=%v)", name, err)
		}
	}
}
