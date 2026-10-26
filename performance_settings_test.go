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

type perfSeen struct{ method, uri, body, accept, user, pass string }

func perfRecordingServer(t *testing.T, reply func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]perfSeen) {
	t.Helper()
	var seen []perfSeen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		seen = append(seen, perfSeen{r.Method, r.RequestURI, string(b), r.Header.Get("Accept"), user, pass})
		reply(w, r)
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

// TestPerformanceSettingsWire: the settings are read and changed on the same route; the interval is 1 to 1440 minutes and
// absent means Forward's default of 5; PATCH leaves an absent property alone, so only what the caller states may be sent, and
// ClearInterval must send an explicit null (JsonProp: absent leaves, null resets).
func TestPerformanceSettingsWire(t *testing.T) {
	t.Parallel()
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":true,"intervalMinutes":10}`))
	})
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	got, _, err := client.Performance.Settings(ctx, "7")
	if err != nil || !got.Enabled || got.Interval() != 10 {
		t.Fatalf("Settings = %#v, %v", got, err)
	}
	if (PerformanceSettings{}).Interval() != DefaultPerformanceIntervalMinutes || DefaultPerformanceIntervalMinutes != 5 {
		t.Fatal("an unset interval must read as Forward's default of 5 minutes")
	}
	if _, _, err := client.Performance.PatchSettings(ctx, "7", PerformanceSettingsPatch{IntervalMinutes: Ptr(15)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Performance.PatchSettings(ctx, "7", PerformanceSettingsPatch{Enabled: Ptr(false), ClearInterval: true}); err != nil {
		t.Fatal(err)
	}
	want := []perfSeen{
		{method: "GET", uri: "/api/networks/7/performance/settings"},
		{method: "PATCH", uri: "/api/networks/7/performance/settings", body: `{"intervalMinutes":15}`},
		{method: "PATCH", uri: "/api/networks/7/performance/settings", body: `{"enabled":false,"intervalMinutes":null}`},
	}
	for i, w := range want {
		if i >= len(*seen) || (*seen)[i].method != w.method || (*seen)[i].uri != w.uri || (*seen)[i].body != w.body {
			t.Fatalf("request %d = %#v, want %#v", i, (*seen)[i], w)
		}
	}
	before := len(*seen)
	for name, patch := range map[string]PerformanceSettingsPatch{
		"empty":         {},
		"interval 0":    {IntervalMinutes: Ptr(0)},
		"interval 1441": {IntervalMinutes: Ptr(1441)},
		"set and clear": {IntervalMinutes: Ptr(5), ClearInterval: true},
	} {
		if _, _, err := client.Performance.PatchSettings(ctx, "7", patch); err == nil {
			t.Fatalf("%s patch was accepted", name)
		}
	}
	if len(*seen) != before {
		t.Fatal("a refused patch reached the wire")
	}
}

// TestPerformanceReadRoutes pins the paths and query strings of the read routes: each takes only the parameters Forward reads,
// so a MetricQuery's other fields must not leak into them, and a name with a slash is percent-encoded.
func TestPerformanceReadRoutes(t *testing.T) {
	t.Parallel()
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/devices-with-data"):
			_, _ = w.Write([]byte(`["sw1","sw2"]`))
		case r.URL.Path == "/api/default-performance-thresholds":
			_, _ = w.Write([]byte(`{"minimumHighThreshold":0.5,"highCpuUsage":0.8}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	q := MetricQuery{Type: "cpu", Days: 3, Device: "ignored", StartTime: "2026-10-01T00:00:00Z", EndTime: "2026-10-02T00:00:00Z", SnapshotID: "55"}

	names, _, err := client.Performance.DevicesWithData(ctx, "7", q)
	if err != nil || len(names) != 2 {
		t.Fatalf("DevicesWithData = %v, %v", names, err)
	}
	if _, _, err := client.Performance.DeviceHealth(ctx, "7", "core 1", q); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Performance.DevicesHealth(ctx, "7", []string{"a", " ", "b"}, q); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Performance.InterfaceHealth(ctx, "7", "sw1", "Gi1/0/1", q); err != nil {
		t.Fatal(err)
	}
	th, _, err := client.Performance.DefaultThresholds(ctx)
	if err != nil || th.HighCPUUsage == nil || *th.HighCPUUsage != 0.8 || th.MediumCPUUsage != nil {
		t.Fatalf("DefaultThresholds = %#v, %v", th, err)
	}
	want := []string{
		"GET /api/networks/7/performance/devices-with-data?endTime=2026-10-02T00%3A00%3A00Z&snapshotId=55&startTime=2026-10-01T00%3A00%3A00Z",
		"GET /api/networks/7/devices/core%201/health?endTime=2026-10-02T00%3A00%3A00Z&snapshotId=55",
		"POST /api/networks/7/device-health?endTime=2026-10-02T00%3A00%3A00Z&snapshotId=55",
		"GET /api/networks/7/devices/sw1/interfaces/Gi1%2F0%2F1/health?endTime=2026-10-02T00%3A00%3A00Z&snapshotId=55",
		"GET /api/default-performance-thresholds",
	}
	for i, w := range want {
		if i >= len(*seen) || (*seen)[i].method+" "+(*seen)[i].uri != w {
			t.Fatalf("request %d = %q, want %q", i, (*seen)[i].method+" "+(*seen)[i].uri, w)
		}
	}
	if (*seen)[2].body != `["a","b"]` {
		t.Fatalf("DevicesHealth body = %s, want the non-blank names", (*seen)[2].body)
	}
	if _, _, err := client.Performance.DeviceHealth(ctx, "7", " ", q); err == nil {
		t.Fatal("DeviceHealth accepted a blank device")
	}
}

// TestCollectorDevicesUsesTheCollectorIdentity: PerfDevicesPB is read with the COLLECTOR's credential, not the API user's, and
// the answer is protobuf bytes, which are empty when the collector has no devices to poll; empty must not be an error.
func TestCollectorDevicesUsesTheCollectorIdentity(t *testing.T) {
	t.Parallel()
	reply := []byte{0x0a, 0x03, 'a', 'b', 'c'}
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.URL.Query().Get("empty") == "" {
			_, _ = w.Write(reply)
		}
	})
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	identity := CollectorIdentity{Username: "collector-1", AuthorizationKey: "secret"}

	got, _, err := client.Performance.CollectorDevices(ctx, "7", identity)
	if err != nil || string(got) != string(reply) {
		t.Fatalf("CollectorDevices = %x, %v", got, err)
	}
	r := (*seen)[0]
	if r.method != "GET" || r.uri != "/api/networks/7/performance/devices" || r.user != "collector-1" || r.pass != "secret" || r.accept != "application/octet-stream" {
		t.Fatalf("request = %#v", r)
	}
	if _, _, err := client.Performance.CollectorDevices(ctx, "7", CollectorIdentity{Username: "c"}); err == nil {
		t.Fatal("a collector identity without a key was accepted")
	}
	if len(*seen) != 1 {
		t.Fatal("a refused call reached the wire")
	}
}

func TestCollectorDevicesAcceptsAnEmptyAnswer(t *testing.T) {
	t.Parallel()
	server, _ := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	client := newTestClient(t, server.URL)
	got, _, err := client.Performance.CollectorDevices(context.Background(), "7", CollectorIdentity{Username: "c", AuthorizationKey: "k"})
	if err != nil || len(got) != 0 {
		t.Fatalf("CollectorDevices on an empty body = %x, %v", got, err)
	}
}

// TestRawSNMPDataWire: Forward needs a device, a credential and either OIDs or the device performance readings, and takes the
// OIDs as a repeated parameter.
func TestRawSNMPDataWire(t *testing.T) {
	t.Parallel()
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, _, err := client.Performance.RawSNMPData(ctx, "7", RawSNMPRequest{Device: "sw 1", CredentialID: "S-1", ParentOIDs: []string{"1.3.6.1.2.1.2", " ", "1.3.6.1.2.1.31"}, InterfaceDataProvider: "BULK_READ_TABLE", DeviceAddressOverride: "10.0.0.9"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Performance.RawSNMPData(ctx, "7", RawSNMPRequest{Device: "sw1", CredentialID: "S-1", IncludeDevicePerfData: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/api/networks/7/performance/raw-data?credentialId=S-1&deviceAddressOverride=10.0.0.9&deviceName=sw+1&interfaceDataProviderType=BULK_READ_TABLE&parentOids=1.3.6.1.2.1.2&parentOids=1.3.6.1.2.1.31",
		"/api/networks/7/performance/raw-data?credentialId=S-1&deviceName=sw1&includeDevicePerfData=true",
	}
	for i, w := range want {
		if (*seen)[i].method != "GET" || (*seen)[i].uri != w {
			t.Fatalf("request %d = %s %s, want %s", i, (*seen)[i].method, (*seen)[i].uri, w)
		}
	}
	before := len(*seen)
	for name, r := range map[string]RawSNMPRequest{
		"no OIDs":      {Device: "d", CredentialID: "c"},
		"no device":    {CredentialID: "c", IncludeDevicePerfData: true},
		"no cred":      {Device: "d", IncludeDevicePerfData: true},
		"bad provider": {Device: "d", CredentialID: "c", IncludeDevicePerfData: true, InterfaceDataProvider: "WALK"},
	} {
		if _, _, err := client.Performance.RawSNMPData(ctx, "7", r); err == nil {
			t.Fatalf("%s request was accepted", name)
		}
	}
	if len(*seen) != before {
		t.Fatal("a refused request reached the wire")
	}
}

// TestSNMPCredentialPatchUsesForwardsNames: Forward's SnmpCredentialUpdate reads name, version, port, timeoutSec,
// communityString, authSettings{username,password,authType,privacyProtocol,privacyPassword} and autoAssociate. The patch used to
// carry community/user/authenticationType/... which Forward never read, so a change made through it did nothing.
func TestSNMPCredentialPatchUsesForwardsNames(t *testing.T) {
	t.Parallel()
	encode := func(p SNMPCredentialPatch) string {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := encode(SNMPCredentialPatch{Name: Ptr("n"), CommunityString: Ptr("c"), AutoAssociate: Ptr(false)}); got != `{"autoAssociate":false,"communityString":"c","name":"n"}` {
		t.Fatalf("V2C patch = %s", got)
	}
	v3 := SNMPCredentialPatch{Version: SNMPVersionV3, AuthSettings: &SNMPAuthSettingsPatch{Username: Ptr("u"), Password: Ptr("p"), AuthType: SNMPAuthSHA256, PrivacyProtocol: SNMPPrivacyAES256TDES, PrivacyPassword: Ptr("q")}}
	if got := encode(v3); got != `{"authSettings":{"authType":"SHA_256","password":"p","privacyPassword":"q","privacyProtocol":"AES_256_TDES","username":"u"},"version":"V3"}` {
		t.Fatalf("V3 patch = %s", got)
	}
	cleared := SNMPCredentialPatch{Clear: []string{"communityString", "port"}, AuthSettings: &SNMPAuthSettingsPatch{Clear: []string{"privacyPassword", "privacyProtocol"}}}
	if got := encode(cleared); got != `{"authSettings":{"privacyPassword":null,"privacyProtocol":null},"communityString":null,"port":null}` {
		t.Fatalf("clearing patch = %s", got)
	}
	// The old field names map onto the wire names instead of being sent as properties Forward ignores.
	legacy := SNMPCredentialPatch{Community: Ptr("c"), User: Ptr("u"), AuthenticationType: Ptr("SHA"), AuthenticationKey: Ptr("k"), PrivacyType: Ptr("DES"), PrivacyKey: Ptr("x"), ContextName: Ptr("ignored")}
	if got := encode(legacy); got != `{"authSettings":{"authType":"SHA","password":"k","privacyPassword":"x","privacyProtocol":"DES","username":"u"},"communityString":"c"}` {
		t.Fatalf("legacy patch = %s", got)
	}
	for name, p := range map[string]SNMPCredentialPatch{
		"clear name":        {Clear: []string{"name"}},
		"set and clear":     {Port: Ptr(1), Clear: []string{"port"}},
		"clear auth member": {AuthSettings: &SNMPAuthSettingsPatch{Clear: []string{"username"}}},
	} {
		if _, err := json.Marshal(p); err == nil {
			t.Fatalf("%s was encoded", name)
		}
	}
}

func TestUpdateSNMPCredentialWire(t *testing.T) {
	t.Parallel()
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	if _, err := client.Credentials.UpdateSNMPCredential(ctx, "7", "S-1", SNMPCredentialPatch{TimeoutSec: Ptr(10)}); err != nil {
		t.Fatal(err)
	}
	if r := (*seen)[0]; r.method != "PATCH" || r.uri != "/api/networks/7/snmpCredentials/S-1" || r.body != `{"timeoutSec":10}` {
		t.Fatalf("request = %#v", r)
	}
	for name, p := range map[string]SNMPCredentialPatch{
		"bad version": {Version: "V1"},
		"blank name":  {Name: Ptr(" ")},
		"timeout 0":   {TimeoutSec: Ptr(0)},
		"blank user":  {AuthSettings: &SNMPAuthSettingsPatch{Username: Ptr("")}},
	} {
		if _, err := client.Credentials.UpdateSNMPCredential(ctx, "7", "S-1", p); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if len(*seen) != 1 {
		t.Fatal("a refused patch reached the wire")
	}
}

// TestSNMPCredentialReadShowsTheV3Half: a stored V3 credential reports its username, digest and cipher (never the secrets), which
// the SDK used to drop, leaving no way to tell two V3 credentials apart.
func TestSNMPCredentialReadShowsTheV3Half(t *testing.T) {
	t.Parallel()
	var c SNMPCredential
	body := `{"id":"S-1","name":"v3","version":"V3","authSettings":{"username":"u","authType":"SHA_256","privacyProtocol":"AES_128","password":"opaque"},"createdBy":"craig","createdAt":"2026-10-01T00:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	if c.AuthSettings == nil || c.AuthSettings.Username != "u" || c.AuthSettings.AuthType != SNMPAuthSHA256 || c.AuthSettings.PrivacyProtocol != SNMPPrivacyAES128 || c.CreatedBy != "craig" {
		t.Fatalf("credential = %#v", c)
	}
}

// TestReadinessNamesWhatBlocksPolling: Forward polls a device over SNMP only when the network's setting is enabled, an SNMP
// credential exists, and the device has collection and SNMP collection on (SNMP collection defaults to off, collection to on).
// Readiness must report each missing piece, and must not report a network as ready when only some are in place.
func TestReadinessNamesWhatBlocksPolling(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		settings, credentials, devices string
		wantReady                      bool
		wantEligible, wantExcluded     int
		wantBlockers                   int
	}{
		"nothing set up":  {`{"enabled":false}`, `[]`, `[{"name":"a","host":"1.1.1.1"}]`, false, 0, 1, 3},
		"enabled only":    {`{"enabled":true,"intervalMinutes":10}`, `[]`, `[{"name":"a","host":"1.1.1.1","enableSnmpCollection":true}]`, false, 1, 0, 1},
		"credential only": {`{"enabled":false}`, `[{"id":"S-1","name":"c","version":"V2C"}]`, `[{"name":"a","host":"1.1.1.1","enableSnmpCollection":true}]`, false, 1, 0, 1},
		"collection off":  {`{"enabled":true}`, `[{"id":"S-1"}]`, `[{"name":"a","host":"1.1.1.1","enableSnmpCollection":true,"collect":false},{"name":"b","host":"1.1.1.2","enableSnmpCollection":true,"collect":true}]`, true, 1, 1, 0},
		"all in place":    {`{"enabled":true}`, `[{"id":"S-1"}]`, `[{"name":"a","host":"1.1.1.1","enableSnmpCollection":true}]`, true, 1, 0, 0},
	}
	for name, c := range cases {
		c := c
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/performance/settings"):
					_, _ = w.Write([]byte(c.settings))
				case strings.HasSuffix(r.URL.Path, "/snmpCredentials"):
					_, _ = w.Write([]byte(c.credentials))
				default:
					_, _ = w.Write([]byte(c.devices))
				}
			})
			got, _, err := newTestClient(t, server.URL).Performance.Readiness(context.Background(), "7")
			if err != nil {
				t.Fatal(err)
			}
			if got.Ready() != c.wantReady || len(got.Eligible) != c.wantEligible || len(got.Excluded) != c.wantExcluded || len(got.Blockers) != c.wantBlockers {
				t.Fatalf("readiness = %#v", got)
			}
			wantURIs := []string{"/api/networks/7/performance/settings", "/api/networks/7/snmpCredentials", "/api/networks/7/classic-devices"}
			if len(*seen) != len(wantURIs) {
				t.Fatalf("requests = %d, want the settings, credentials and devices reads", len(*seen))
			}
			for i, r := range *seen {
				if r.method != "GET" || r.uri != wantURIs[i] {
					t.Fatalf("request %d = %s %s, want GET %s", i, r.method, r.uri, wantURIs[i])
				}
			}
			if name == "collection off" && (len(got.Excluded) != 1 || got.Excluded[0].Name != "a" || len(got.Excluded[0].Reasons) != 1 || got.Excluded[0].Reasons[0] != "collection is off") {
				t.Fatalf("excluded = %#v, want device a excluded because collection is off", got.Excluded)
			}
			if name == "nothing set up" && (len(got.Excluded) != 1 || got.Excluded[0].Reasons[0] != "SNMP collection is off") {
				t.Fatalf("excluded = %#v, want SNMP collection reported off when it is not stated", got.Excluded)
			}
		})
	}
}

// TestReadinessFollowsTheBoundNetwork: a ForNetwork client may pass "" for the network, and all three reads must use the bound one;
// the credential read used to fail on an empty ID after the settings read had already been sent.
func TestReadinessFollowsTheBoundNetwork(t *testing.T) {
	t.Parallel()
	server, seen := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/performance/settings"):
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case strings.HasSuffix(r.URL.Path, "/snmpCredentials"):
			_, _ = w.Write([]byte(`[{"id":"S-1"}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	})
	if _, _, err := newTestClient(t, server.URL).ForNetwork("9").Performance.Readiness(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"/api/networks/9/performance/settings", "/api/networks/9/snmpCredentials", "/api/networks/9/classic-devices"} {
		if i >= len(*seen) || (*seen)[i].uri != want {
			t.Fatalf("requests = %#v, want %s at %d", *seen, want, i)
		}
	}
	before := len(*seen)
	if _, _, err := newTestClient(t, server.URL).Performance.Readiness(context.Background(), ""); err == nil || len(*seen) != before {
		t.Fatal("Readiness with no network ID and no bound network must fail before any request")
	}
}

func TestReadinessReturnsAFailedReadAsAnError(t *testing.T) {
	t.Parallel()
	server, _ := perfRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/snmpCredentials") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"enabled":true}`))
	})
	got, _, err := newTestClient(t, server.URL).Performance.Readiness(context.Background(), "7")
	if err == nil || got != nil {
		t.Fatalf("Readiness = %#v, %v; want nil and the 403", got, err)
	}
}
