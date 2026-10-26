package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClassicDevicesUseTheBoundNetwork: a ForNetwork client must be able to pass "" like the other network-scoped
// services do. Before, every ClassicDevices method refused an empty ID even on a bound client.
func TestClassicDevicesUseTheBoundNetwork(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("77")
	ctx := context.Background()

	if _, _, err := client.ClassicDevices.List(ctx, ""); err != nil {
		t.Fatalf("List on a bound client: %v", err)
	}
	if _, err := client.ClassicDevices.Delete(ctx, "", "sw1"); err != nil {
		t.Fatalf("Delete on a bound client: %v", err)
	}
	want := []string{"GET /api/networks/77/classic-devices", "DELETE /api/networks/77/classic-devices/sw1"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v, want %v", paths, want)
	}
	// An unbound client still has no network to fall back to.
	if _, _, err := newTestClient(t, server.URL).ClassicDevices.List(ctx, ""); err == nil {
		t.Fatal("List with no network ID and no bound network succeeded")
	}
}

// TestControllerManagedSetupDeleteFoldsCase: Forward stores setup names lowercased and answers 404 for a name sent in
// another case, which Delete reads as "already gone". Sending the name unchanged would report success and delete nothing.
func TestControllerManagedSetupDeleteFoldsCase(t *testing.T) {
	t.Parallel()
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, err := client.ControllerManagedSetups.Delete(context.Background(), "7", " SDWAN "); err != nil {
		t.Fatal(err)
	}
	if path != "/api/networks/7/controller-managed-setups/sdwan" {
		t.Fatalf("path = %q, want the lowercased, trimmed name", path)
	}
}

// TestFailedOverrideReadIsNotAnEmptySet: SetNetworkOverrides replaces everything staged, so a failed read must not come
// back as an empty (non-nil) set that a read-modify-write caller could write back.
func TestFailedOverrideReadIsNotAnEmptySet(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if got, _, err := client.Topology.NetworkOverrides(ctx, "7"); err == nil || got != nil {
		t.Fatalf("NetworkOverrides = %#v, %v; want nil and an error", got, err)
	}
	if got, _, err := client.Topology.SnapshotOverrides(ctx, "7", "9"); err == nil || got != nil {
		t.Fatalf("SnapshotOverrides = %#v, %v; want nil and an error", got, err)
	}
	if got, _, err := client.Collectors.UpgradeAll(ctx); err == nil || got != nil {
		t.Fatalf("UpgradeAll = %#v, %v; want nil and an error", got, err)
	}
}

// TestControllerManagedEntriesDoNotSendTheTestResult: testResult is output-only. An entry read with ListWithTestResults and
// sent back (as Patch's documentation suggests) must not carry it.
func TestControllerManagedEntriesDoNotSendTheTestResult(t *testing.T) {
	t.Parallel()
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	controllers := []ControllerDevice{{Name: "c1", Host: "10.0.0.1", TestResult: &DeviceTestResult{Error: "x"}}}
	if _, _, err := client.ControllerManagedSetups.Patch(context.Background(), "7", "s1", ControllerManagedSetupPatch{Controllers: &controllers}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "testResult") || !strings.Contains(body, `"c1"`) {
		t.Fatalf("body = %s, want the controller without testResult", body)
	}
	// Only the request is stripped: the caller's entry keeps its result, and encoding it elsewhere (a cache, a tool's output) still
	// carries it.
	if controllers[0].TestResult == nil {
		t.Fatal("Patch cleared the caller's TestResult")
	}
	if encoded, err := json.Marshal(controllers[0]); err != nil || !strings.Contains(string(encoded), "testResult") {
		t.Fatalf("Marshal = %s, %v; want testResult kept", encoded, err)
	}
}

// TestInputsAreTrimmedBeforeTheyAreSent: a padded name that passes the emptiness check must reach the wire trimmed, or Forward
// looks up a name nothing has.
func TestInputsAreTrimmedBeforeTheyAreSent(t *testing.T) {
	t.Parallel()
	var uris, bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uris, bodies = append(uris, r.RequestURI), append(bodies, string(b))
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	_, _, _ = client.Devices.Interface(ctx, "7", "sw1", " eth0 ", DeviceInterfaceOptions{})
	_, _, _ = client.Routing.PrefixTrace(ctx, "7", "s1", RoutingTraceRequest{Prefix: "10.0.0.0/8", Outcome: "INSTALLED", Router: DeviceVRF{Device: " r1 "}, OriginatedFrom: DeviceVRF{Device: " r2 "}})
	_, _, _ = client.Routing.PrefixDetails(ctx, "7", "s1", RoutingPrefixDetailsRequest{Prefix: "10.0.0.0/8", Node: RoutingNode{Device: " r1 "}})
	if len(uris) != 3 || !strings.HasSuffix(uris[0], "/interfaces/eth0") {
		t.Fatalf("requests = %v", uris)
	}
	for _, b := range bodies[1:] {
		if strings.Contains(b, `" r`) {
			t.Fatalf("body %s carries an untrimmed device name", b)
		}
	}
}

// TestNewAccessForSessionToleratesNoSession: a nil session must not panic an exported call.
func TestNewAccessForSessionToleratesNoSession(t *testing.T) {
	t.Parallel()
	if NewAccessForSession(nil, nil) == nil {
		t.Fatal("NewAccessForSession(nil, nil) returned nil")
	}
}

// TestPathBulkResultDecodesBothShapes: the fast path (no "error" key) and the probe path must give the same answers as
// before: a response object, and Forward's {"error": true, "errorMessage": "..."} object.
func TestPathBulkResultDecodesBothShapes(t *testing.T) {
	t.Parallel()
	var ok PathBulkResult
	if err := ok.UnmarshalJSON([]byte(`{"srcIpLocationType":"HOST","dstIpLocationType":"ROUTE","info":{"paths":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if ok.Failed || ok.Response == nil || ok.Response.DstIPLocationType != "ROUTE" {
		t.Fatalf("response result = %#v", ok)
	}
	var failed PathBulkResult
	if err := failed.UnmarshalJSON([]byte(`{"error":true,"errorMessage":"no such device"}`)); err != nil {
		t.Fatal(err)
	}
	if !failed.Failed || failed.ErrorMessage != "no such device" || failed.Response != nil {
		t.Fatalf("error result = %#v", failed)
	}
	var exact PathBulkResult
	if err := exact.UnmarshalJSON([]byte(`{"srcIpLocationType":"HOST","info":{"paths":[{"hops":[{"deviceName":"error"}]}]}}`)); err != nil || exact.Failed || exact.Response == nil {
		t.Fatalf("a result with the value \"error\" = %#v, %v", exact, err)
	}
	var named PathBulkResult
	if err := named.UnmarshalJSON([]byte(`{"srcIpLocationType":"HOST","info":{"paths":[{"forwardingOutcome":"DELIVERED","hops":[{"deviceName":"error-gw"}]}]}}`)); err != nil || named.Failed || named.Response == nil {
		t.Fatalf("a result mentioning \"error\" inside a value = %#v, %v", named, err)
	}
}

// TestExposureDetailsRefuseANegativeAssetID: a negative id can never be a host id, so it must not reach the wire.
func TestExposureDetailsRefuseANegativeAssetID(t *testing.T) {
	t.Parallel()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, _, err := client.ThreatExposure.DirectExposureDetails(ctx, "7", "d1", "s1", -1); err == nil {
		t.Fatal("DirectExposureDetails accepted -1")
	}
	if _, _, err := client.ThreatExposure.TransitiveExposureDetails(ctx, "7", "d1", "s1", -1, ThreatExposurePaging{}); err == nil {
		t.Fatal("TransitiveExposureDetails accepted -1")
	}
	if called {
		t.Fatal("a request was sent for a negative asset id")
	}
}

// TestClassicDeviceCallsNameTheirOperation: hooks key on Event.Operation, and the per-device calls used to reach them with an
// empty one, unlike List and the batch calls.
func TestClassicDeviceCallsNameTheirOperation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"name":"sw1"}`))
	}))
	defer server.Close()
	var operations []string
	client, err := NewClient(Config{
		BaseURL:    server.URL,
		Username:   "user",
		Password:   "pass",
		HTTPClient: privateHTTPClient(),
		Hooks: []Hook{func(_ context.Context, e Event) {
			if e.Type == EventRequest {
				operations = append(operations, e.Operation)
			}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	device := ClassicDeviceRequest{Name: "sw1", Host: "10.0.0.1"}
	_, _, _ = client.ClassicDevices.Create(ctx, "7", device)
	_, _, _ = client.ClassicDevices.Get(ctx, "7", "sw1")
	_, _, _ = client.ClassicDevices.Put(ctx, "7", "sw1", device)
	_, _, _ = client.ClassicDevices.Patch(ctx, "7", "sw1", ClassicDevicePatch{Note: Ptr("n")})
	_, _ = client.ClassicDevices.Delete(ctx, "7", "sw1")
	want := "ClassicDevices.Create ClassicDevices.Get ClassicDevices.Put ClassicDevices.Patch ClassicDevices.Delete"
	if got := strings.Join(operations, " "); got != want {
		t.Fatalf("operations = %q, want %q", got, want)
	}
}
