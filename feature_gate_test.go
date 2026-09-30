package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// AccessEnforcer refuses a route whose org or deployment property is not set
// the way the route needs with a 403 and no reason code; the message is
// PermissionsErrorMessages' fixed template, so that sentence is what
// classifies. A feature switched off must be distinguishable from a missing
// permission without the caller matching prose.
func TestFeatureGatedRoutesAreClassified(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		message  string
		gated    bool
		property string
		enabled  bool
		scope    string
	}{
		{name: "org feature off", message: "LOCATION_CONNECTIVITY_DIFFS is off for your organization", gated: true, property: "LOCATION_CONNECTIVITY_DIFFS", scope: "organization"},
		{name: "org feature on", message: "SOFTWARE_CENTRAL is on for your organization", gated: true, property: "SOFTWARE_CENTRAL", enabled: true, scope: "organization"},
		{name: "deployment feature off", message: "OUTBOUND_CONNECTIONS is off for your deployment", gated: true, property: "OUTBOUND_CONNECTIONS", scope: "deployment"},
		// The authentication heuristic keys on "token"; a property named for
		// tokens is still a feature gate.
		{name: "property named for tokens", message: "API_TOKENS is off for your organization", gated: true, property: "API_TOKENS", scope: "organization"},
		{name: "ordinary permission refusal", message: "Access is denied"},
		{name: "template embedded in other text", message: "Denied: X is off for your organization, ask an admin"},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"apiUrl":"/api/diffs/1/2/subnet-connectivity","httpMethod":"GET","message":"`+tc.message+`"}`)
		}))
		_, _, err := newTestClient(t, server.URL).Diffs.SubnetConnectivity(context.Background(), "1", "2")
		server.Close()

		var apiErr *ErrorResponse
		if !errors.As(err, &apiErr) {
			t.Fatalf("%s: err = %v, want an ErrorResponse", tc.name, err)
		}
		if got := errors.Is(err, ErrFeatureGated); got != tc.gated {
			t.Errorf("%s: errors.Is(ErrFeatureGated) = %v, want %v (kind %s)", tc.name, got, tc.gated, apiErr.Kind)
			continue
		}
		if !tc.gated {
			if apiErr.GateProperty != "" {
				t.Errorf("%s: GateProperty = %q on a non-gate error", tc.name, apiErr.GateProperty)
			}
			continue
		}
		if errors.Is(err, ErrAuthentication) {
			t.Errorf("%s: a feature gate also reads as an authentication failure", tc.name)
		}
		if apiErr.GateProperty != tc.property || apiErr.GateEnabled != tc.enabled || apiErr.GateScope != tc.scope {
			t.Errorf("%s: gate = %q enabled=%v scope=%q", tc.name, apiErr.GateProperty, apiErr.GateEnabled, apiErr.GateScope)
		}
	}
}

// The shape is DeviceController.getMissingDevices' MissingDevices envelope.
// vendor and discoveryMethod are omitted when Forward does not know them, and
// type is omitted when it is UNKNOWN.
func TestDevicesMissing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/networks/net-1/missing-devices" || r.URL.Query().Get("snapshotId") != "1021" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"devices":[
		  {"name":"core-3","ipAddresses":["10.0.0.3","10.0.1.3"],"type":"CISCO_IOS_XE","vendor":"CISCO","neighbors":["leaf-1","leaf-2"],"discoveryMethod":"LLDP"},
		  {"name":"mystery","ipAddresses":[],"neighbors":["leaf-4"]}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Devices.Missing(context.Background(), "net-1", "1021")
	if err != nil {
		t.Fatalf("Devices.Missing() error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "core-3" || got[0].Vendor != "CISCO" || len(got[0].Neighbors) != 2 ||
		got[0].DiscoveryMethod != "LLDP" || len(got[0].IPAddresses) != 2 {
		t.Fatalf("Devices.Missing() = %+v", got)
	}
	if got[1].Vendor != "" || got[1].Type != "" || got[1].DiscoveryMethod != "" {
		t.Fatalf("unknown fields should stay empty: %+v", got[1])
	}
}

// Without a snapshot ID Forward chooses the snapshot; nothing is sent for it.
func TestDevicesMissingLetsForwardChooseTheSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"devices":[]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Devices.Missing(context.Background(), "net-1", " ")
	if err != nil || len(got) != 0 {
		t.Fatalf("Devices.Missing() = %v, %v", got, err)
	}
}
