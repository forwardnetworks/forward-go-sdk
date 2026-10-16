package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// Names are unique across every collection source, so guests still held by
// classic devices make a plain Create fail with a 400. Forward's atomic way to
// take them over is POST ...controller-managed-setups?action=migrate, which
// deletes the overlapping classic devices and adds the setup in one
// transaction. It must be a distinct call, never a Create option a caller
// can reach by accident, and it must send the same body and the query.
func TestControllerManagedCreateMigratingClassicSendsActionMigrate(t *testing.T) {
	t.Parallel()

	var gotQuery, gotBody, gotMethod, migrateBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotQuery = r.Method, r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"sdwan"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	setup := NewControllerManagedSetup{Name: "sdwan", Controllers: []ControllerDevice{{Name: "vs1"}}}
	if _, _, err := client.ControllerManagedSetups.CreateMigratingClassic(context.Background(), "5", setup); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotQuery != "action=migrate" {
		t.Fatalf("got %s ?%s", gotMethod, gotQuery)
	}
	migrateBody = gotBody
	if _, _, err := client.ControllerManagedSetups.Create(context.Background(), "5", setup); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Fatalf("a plain Create must never carry action=migrate, got ?%s", gotQuery)
	}
	if gotBody == "" || gotBody != migrateBody {
		t.Fatalf("both calls must send the same setup body: %q vs %q", migrateBody, gotBody)
	}
}

// The setup test is queued, not run: Forward answers 204 at once. The body must
// name devices or setups, never both or neither (Forward's rule, checked here so
// the caller gets the reason without a round trip), and a collector id rides in
// the query only when given.
func TestControllerManagedTestSelection(t *testing.T) {
	t.Parallel()

	var gotQuery, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", ControllerAndManagedDeviceSelection{SetupNames: []string{"sdwan"}}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "action=test" || gotBody != `{"setupNames":["sdwan"]}` {
		t.Fatalf("got ?%s %s", gotQuery, gotBody)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "C1", ControllerAndManagedDeviceSelection{Names: []string{"vs1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", ControllerAndManagedDeviceSelection{SetupNames: []string{" SDWAN "}}); err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"setupNames":["sdwan"]}` {
		t.Fatalf("setup names must be folded to the stored lowercase form, got %s", gotBody)
	}
	if _, err := client.ControllerManagedSetups.Test(ctx, "5", "C1", ControllerAndManagedDeviceSelection{Names: []string{"vs1"}}); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "action=test&collectorId=C1" {
		t.Fatalf("got ?%s", gotQuery)
	}
	gotQuery = "untouched"
	for _, bad := range []ControllerAndManagedDeviceSelection{{}, {Names: []string{"a"}, SetupNames: []string{"b"}}} {
		if _, err := client.ControllerManagedSetups.Test(ctx, "5", "", bad); err == nil {
			t.Fatalf("%+v must be refused", bad)
		}
	}
	if gotQuery != "untouched" {
		t.Fatal("a refused selection must not reach Forward")
	}
}

// forward-skills reads a migrated setup back to show which collector it runs
// through and how each device's last test went. The collector is one setup-wide
// id (no per-device pin exists), a number or a string on the wire; the test
// result is returned only for ?with=testResult and decodes like a classic
// device's.
func TestControllerManagedReadsCollectorAndTestResults(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"setups":[{"name":"sdwan","collectorId":"C7",
			"controllers":[{"name":"vs1","testResult":{"error":"AUTHENTICATION_FAILED","errorPhase":"AUTHENTICATION"}}],
			"managedDevices":[{"name":"edge1","testResult":{"discoveredType":"cisco_ios_xe_ssh","controllerInfos":[{"name":"vs1","ipAddress":"10.0.0.1"}]}},{"name":"edge2"}]}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	setups, _, err := client.ControllerManagedSetups.ListWithTestResults(context.Background(), "5")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "with=testResult" || len(setups) != 1 || setups[0].CollectorID != "C7" {
		t.Fatalf("got ?%s %+v", gotQuery, setups)
	}
	controller := setups[0].Controllers[0].TestResult
	if controller == nil || controller.Error != "AUTHENTICATION_FAILED" || controller.ErrorPhase != "AUTHENTICATION" {
		t.Fatalf("controller test result %+v", controller)
	}
	edge1, edge2 := setups[0].ManagedDevices[0].TestResult, setups[0].ManagedDevices[1].TestResult
	if edge1 == nil || edge1.DiscoveredType != "cisco_ios_xe_ssh" || len(edge1.ControllerInfos) != 1 || edge2 != nil {
		t.Fatalf("guest test results %+v / %+v", edge1, edge2)
	}
	if _, _, err := client.ControllerManagedSetups.List(context.Background(), "5"); err != nil || gotQuery != "" {
		t.Fatalf("plain List must not ask for test results: ?%s %v", gotQuery, err)
	}
}

// A setup is pinned to a collector at creation (there is no per-device pin), so
// CollectorID must reach the Create and migrate bodies when set and send no key
// when empty, leaving Forward's default collector in force.
func TestControllerManagedCreateSendsCollectorID(t *testing.T) {
	t.Parallel()

	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"sdwan"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	setup := NewControllerManagedSetup{Name: "sdwan", Controllers: []ControllerDevice{{Name: "vs1"}}, CollectorID: "C3"}
	for name, call := range map[string]func() error{
		"Create": func() error { _, _, err := client.ControllerManagedSetups.Create(ctx, "5", setup); return err },
		"CreateMigratingClassic": func() error {
			_, _, err := client.ControllerManagedSetups.CreateMigratingClassic(ctx, "5", setup)
			return err
		},
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(gotBody, `"collectorId":"C3"`) {
			t.Fatalf("%s body %s", name, gotBody)
		}
	}
	setup.CollectorID = ""
	if _, _, err := client.ControllerManagedSetups.Create(ctx, "5", setup); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotBody, "collectorId") {
		t.Fatalf("an empty collector must send no key: %s", gotBody)
	}
}

// A DeviceTestResult is what a caller caches, so it must encode under the same
// keys it decodes from, or a cached failure reads back as a pass. Every field is
// set in the input and checked non-zero, so a mistyped tag or decode key on any
// one of them fails here instead of round-tripping as an empty value.
func TestDeviceTestResultRoundTrips(t *testing.T) {
	t.Parallel()

	in := []byte(`{"error":"AUTHENTICATION_FAILED","errorPhase":"AUTHENTICATION","hostIps":["10.0.0.1"],"unauthorizedCommands":["show run"],"managedDevices":[{"name":"edge1","ipAddress":"10.0.1.1"}],"discoveredCliCredentialId":"L-3","discoveredHttpCredentialId":"H-4","discoveredSnmpCredentialId":"S-5","discoveredType":"cisco_sdwan_vsmart_ssh","controllerInfos":[{"name":"vs1","ipAddress":"10.0.2.1"}],"nameAliases":["vs1.example"],"dependentNameAndAliases":["edge1"],"startTime":"2026-10-16T00:59:00Z","savedAt":"2026-10-16T01:00:01Z","endTime":"2026-10-16T01:00:00Z"}`)
	var first DeviceTestResult
	if err := json.Unmarshal(in, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.HostIPs) != 1 || len(first.UnauthorizedCommands) != 1 || len(first.ManagedDevices) != 1 || first.ManagedDevices[0].IPAddress != "10.0.1.1" || first.DiscoveredCLICredentialID != "L-3" {
		t.Fatalf("phase details lost: %+v", first)
	}
	if first.Error != "AUTHENTICATION_FAILED" || first.SavedAt != "2026-10-16T01:00:01Z" || first.EndTime != "2026-10-16T01:00:00Z" {
		t.Fatalf("decoded %+v", first)
	}
	for i, value := 0, reflect.ValueOf(first); i < value.NumField(); i++ {
		if value.Field(i).IsZero() {
			t.Errorf("field %s was not decoded from the input", value.Type().Field(i).Name)
		}
	}
	out, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var second DeviceTestResult
	if err := json.Unmarshal(out, &second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round trip lost data: %s -> %+v", out, second)
	}
}
