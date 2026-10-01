package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forward's snmpCollectionStatus (SnmpCollectionStatus with SnmpErrorDetails
// unwrapped) is {timestamp} after a successful collection and {timestamp,
// errorType, error} after a failed one, and is absent when SNMP collection is
// off. The status/state form older fixtures used still decodes.
func TestSourceTestStatusSNMPCollectionShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                        string
		body                        string
		reported                    bool
		status, at, errType, errMsg string
	}{
		{name: "succeeded", body: `{"name":"r1","snmpCollectionStatus":{"timestamp":"2026-10-01T10:00:00Z"}}`,
			reported: true, at: "2026-10-01T10:00:00Z"},
		{name: "failed", body: `{"name":"r1","snmpCollectionStatus":{"timestamp":"2026-10-01T10:00:00Z","errorType":"NO_RESPONSE","error":"Request timed out"}}`,
			reported: true, status: "NO_RESPONSE", at: "2026-10-01T10:00:00Z", errType: "NO_RESPONSE", errMsg: "Request timed out"},
		{name: "epoch timestamp", body: `{"name":"r1","snmpCollectionStatus":{"timestamp":1790859432171}}`,
			reported: true, at: "1790859432171"},
		{name: "collection off", body: `{"name":"r1"}`},
		{name: "older state form", body: `{"name":"r1","snmpCollectionStatus":{"state":"FAILED"}}`, reported: true, status: "FAILED"},
	}
	for _, tc := range cases {
		var got SourceTestStatus
		if err := json.Unmarshal([]byte(tc.body), &got); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.SNMPCollectionReported != tc.reported || got.SNMPCollectionStatus != tc.status || got.SNMPLastCollectedAt != tc.at ||
			got.SNMPCollectionErrorType != tc.errType || got.SNMPCollectionError != tc.errMsg {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

// A classic device read says whether SNMP collection is on and with which
// credential, which is whether performance data can exist at all.
func TestClassicDeviceReadsSNMPEnablement(t *testing.T) {
	t.Parallel()

	var device ClassicDevice
	if err := json.Unmarshal([]byte(`{"name":"r1","host":"10.0.0.1","enableSnmpCollection":true,"snmpCredentialId":"S1"}`), &device); err != nil {
		t.Fatal(err)
	}
	if device.EnableSNMPCollection == nil || !*device.EnableSNMPCollection || device.SNMPCredentialID != "S1" {
		t.Fatalf("device = %+v", device)
	}
}

// An ERROR or TIMEOUT check carries its reason as the diagnosis summary
// (CheckResult.error/timeout store the message there), which only the lookup
// by id returns.
func TestChecksGetCarriesTheErrorReason(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"C1","name":"bgp up","status":"ERROR","createdAt":"2026-10-01T10:00:00Z","definedAt":"2026-10-01T10:00:00Z",
		  "definition":{"checkType":"NQE"},"diagnosis":{"summary":"NQE query failed: field 'peers' not found","details":[],"detailsIncomplete":false}}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Checks.Get(context.Background(), "9164", "C1")
	if err != nil || got.Status != "ERROR" || got.Diagnosis == nil || got.Diagnosis.Summary != "NQE query failed: field 'peers' not found" {
		t.Fatalf("Checks.Get() = %+v, %v", got, err)
	}
}
