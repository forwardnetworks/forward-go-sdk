package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET .../device-vulnerabilities is the one-call inverse of List/Get: a
// device per entry with its CVE counts, including internetAddressable, which
// is absent (nil) when exposure was not computed and must not read as false.
func TestVulnerabilitiesListDevices(t *testing.T) {
	t.Parallel()

	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"devices":[
		  {"name":"edge-fw1","osVersion":"9.1.4","model":"PA-3220","managementIps":["10.0.0.1"],"tags":["edge"],"locationId":"sjc",
		   "resultToCveCount":{"VULNERABLE":2,"OS_VULNERABLE":3},"severityToCveCount":{"CRITICAL":1,"HIGH":4},
		   "ageToCveCount":{"MONTH":1,"OLDER":4},"hasExploitToCveCount":{"true":1,"false":4},
		   "internetAddressable":true,"summary":"VULNERABLE"},
		  {"name":"core1","resultToCveCount":{"UNCONFIRMED":1},"severityToCveCount":{"LOW":1},"ageToCveCount":{"YEAR":1},
		   "hasExploitToCveCount":{"false":1},"internetAddressable":false,
		   "customLabelCounts":[{"label":"waived","deviceCount":1}],"customStatusCounts":[{"status":"NOT_VULNERABLE","deviceCount":1}],
		   "summary":"POTENTIALLY_VULNERABLE"},
		  {"name":"acc7","resultToCveCount":{"UNIMPLEMENTED":2},"severityToCveCount":{"MEDIUM":2},"ageToCveCount":{"OLDER":2},
		   "hasExploitToCveCount":{"false":2},"summary":"POTENTIALLY_VULNERABLE"}],
		  "totalDevices":40,"indexCreatedAt":"2026-09-30T04:00:00Z"}`)
	}))
	defer server.Close()
	vulns := newTestClient(t, server.URL).Vulnerabilities
	ctx := context.Background()

	got, _, err := vulns.ListDevices(ctx, "N1", DeviceVulnerabilityListOptions{})
	if err != nil || uri != "/api/networks/N1/device-vulnerabilities" {
		t.Fatalf("ListDevices() = %v; sent %s", err, uri)
	}
	if got.TotalDevices != 40 || len(got.Devices) != 3 || got.IndexCreatedAt == "" {
		t.Fatalf("result = %+v", got)
	}
	fw, core, acc := got.Devices[0], got.Devices[1], got.Devices[2]
	if fw.Name != "edge-fw1" || fw.LocationID != "sjc" || fw.ManagementIPs[0] != "10.0.0.1" || fw.TotalCVEs() != 5 || fw.CVEsWithExploit() != 1 ||
		fw.SeverityToCVECount[VulnerabilitySeverityCritical] != 1 || fw.AgeToCVECount[CVEAgeMonth] != 1 || fw.ResultToCVECount[CVEDetectionOSVulnerable] != 3 || fw.Summary != DeviceVulnerable {
		t.Fatalf("edge-fw1 = %+v", fw)
	}
	if fw.InternetAddressable == nil || !*fw.InternetAddressable || core.InternetAddressable == nil || *core.InternetAddressable {
		t.Fatalf("explicit exposure flags = %v, %v", fw.InternetAddressable, core.InternetAddressable)
	}
	if acc.InternetAddressable != nil {
		t.Fatal("an absent internetAddressable (exposure not computed) must stay nil, not false")
	}
	if core.CustomLabelCounts[0].Label != "waived" || core.CustomStatusCounts[0].Status != "NOT_VULNERABLE" || acc.CustomLabelCounts != nil {
		t.Fatalf("custom counts = %+v / %+v", core, acc)
	}
}

// Omitted options send no parameter, so the appserver applies its own
// defaults (latest snapshot, every CVE); a stated exploit:false is sent, and a
// misspelled enum is refused before it costs a round trip.
func TestVulnerabilitiesListDevicesFilters(t *testing.T) {
	t.Parallel()

	var uri string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"devices":[],"totalDevices":0}`)
	}))
	defer server.Close()
	vulns := newTestClient(t, server.URL).Vulnerabilities
	ctx := context.Background()

	if _, _, err := vulns.ListDevices(ctx, "N1", DeviceVulnerabilityListOptions{SnapshotID: "77", Severity: VulnerabilitySeverityCritical, Age: CVEAgeMonth, Exploit: Ptr(false)}); err != nil ||
		uri != "/api/networks/N1/device-vulnerabilities?age=MONTH&exploit=false&severity=CRITICAL&snapshotId=77" {
		t.Fatalf("filters: %v %s", err, uri)
	}
	uri = ""
	for _, options := range []DeviceVulnerabilityListOptions{{Severity: "critical"}, {Age: "WEEK"}} {
		if _, _, err := vulns.ListDevices(ctx, "N1", options); err == nil || uri != "" {
			t.Fatalf("%+v must be refused locally (err=%v, sent %q)", options, err, uri)
		}
	}
}
