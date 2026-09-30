package forward

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// v=2 selects the summarized analysis; without it the same path is the v1
// CVE-OS view. The scope filters are repeated parameters, as the controller's
// Set<LocationId>/Set<Tag> bindings read them.
func TestVulnerabilitiesListSendsV2AndScope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.EscapedPath() != "/api/networks/net-1/vulnerabilities" || q.Get("v") != "2" ||
			q.Get("snapshotId") != "1021" || q.Get("internetAddressable") != "true" ||
			!reflect.DeepEqual(q["location"], []string{"atl", "nyc-b"}) || !reflect.DeepEqual(q["tag"], []string{"SEC"}) {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		// Shapes and values from the published spec's examples.
		_, _ = io.WriteString(w, `{"indexCreatedAt":"2025-06-01T12:34:56.789Z","indexUploadedBy":"me@example.com","vulnerabilities":[
		  {"id":"CVE-2019-0201","description":"An issue is present in Apache ZooKeeper","hasCisaKevEntry":true,"weaknesses":["CWE-345"],
		   "osInfos":[{"vendor":"ARISTA","os":"arista_eos","severity":"CRITICAL","v3Score":9.8,"url":"https://example.com/adv",
		     "configDependent":true,"configAnalysis":"SUPPORTED","osVersions":["4.15.0F"],"deviceCount":200,
		     "resultCounts":[{"result":"VULNERABLE","deviceCount":80},{"result":"NOT_VULNERABLE","deviceCount":120}],
		     "locationIds":["atl","nyc-b"],"tags":["SEC"]}]}]}`)
	}))
	defer server.Close()

	yes := true
	got, _, err := newTestClient(t, server.URL).Vulnerabilities.List(context.Background(), "net-1", VulnerabilityListOptions{
		SnapshotID: "1021", InternetAddressable: &yes, LocationIDs: []string{"atl", " ", "nyc-b"}, Tags: []string{"SEC"},
	})
	if err != nil {
		t.Fatalf("Vulnerabilities.List() error = %v", err)
	}
	if got.IndexCreatedAt == "" || got.IndexUploadedBy != "me@example.com" || len(got.Vulnerabilities) != 1 {
		t.Fatalf("List() = %+v", got)
	}
	v := got.Vulnerabilities[0]
	if v.ID != "CVE-2019-0201" || !v.HasCISAKEVEntry || len(v.OSInfos) != 1 {
		t.Fatalf("vulnerability = %+v", v)
	}
	os := v.OSInfos[0]
	if os.Severity != VulnerabilitySeverityCritical || os.V3Score == nil || *os.V3Score != 9.8 || os.V2Score != nil ||
		os.ConfigDependent == nil || !*os.ConfigDependent || os.DeviceCount != 200 {
		t.Fatalf("os info = %+v", os)
	}
	if len(os.ResultCounts) != 2 || os.ResultCounts[0].Result != CVEDetectionVulnerable || os.ResultCounts[0].DeviceCount != 80 {
		t.Fatalf("result counts = %+v", os.ResultCounts)
	}
}

// With nothing scoped, only v=2 is sent: an absent filter is Forward's
// default (every device, every location), not false.
func TestVulnerabilitiesListUnscoped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "v=2" {
			t.Errorf("query = %q, want only v=2", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"vulnerabilities":[],"indexCreatedAt":"2025-06-01T12:34:56.789Z"}`)
	}))
	defer server.Close()

	if _, _, err := newTestClient(t, server.URL).Vulnerabilities.List(context.Background(), "net-1", VulnerabilityListOptions{}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

// Get carries the per-device verdict NQE's boolean cveFindings cannot: the
// detection result, the derived status, and the configuration lines.
func TestVulnerabilitiesGetReturnsPerDeviceVerdicts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/networks/net-1/vulnerabilities/CVE-2016-0270" || r.URL.RawQuery != "" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"id":"CVE-2016-0270","description":"GCM nonce","osInfos":[
		  {"vendor":"F5","os":"f5_bigip","severity":"HIGH","description":"Back in February we were contacted","devices":[
		    {"name":"nyc-dc01-fw02","osVersion":"4.15.0F","model":"BIG-IP Virtual Edition","managementIps":["10.10.10.10"],
		     "locationId":"nyc-b","internetAddressable":true,"status":"VULNERABLE","result":"VULNERABLE",
		     "fileRanges":{"nyc-dc01-fw02,configuration.txt":[{"start":286,"end":291}]}},
		    {"name":"atl-fw01","osVersion":"4.15.0F","managementIps":[],"locationId":"atl",
		     "status":"POTENTIALLY_VULNERABLE","result":"UNCONFIRMED"}]}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Vulnerabilities.Get(context.Background(), "net-1", " CVE-2016-0270 ", "")
	if err != nil {
		t.Fatalf("Vulnerabilities.Get() error = %v", err)
	}
	if got.ID != "CVE-2016-0270" || len(got.OSInfos) != 1 || got.OSInfos[0].Description == "" || len(got.OSInfos[0].Devices) != 2 {
		t.Fatalf("Get() = %+v", got)
	}
	confirmed, unconfirmed := got.OSInfos[0].Devices[0], got.OSInfos[0].Devices[1]
	if confirmed.Result != CVEDetectionVulnerable || confirmed.Status != DeviceVulnerable ||
		confirmed.InternetAddressable == nil || !*confirmed.InternetAddressable {
		t.Fatalf("confirmed = %+v", confirmed)
	}
	lines := confirmed.FileRanges["nyc-dc01-fw02,configuration.txt"]
	if len(lines) != 1 || lines[0].Start == nil || *lines[0].Start != 286 || *lines[0].End != 291 {
		t.Fatalf("file ranges = %+v", confirmed.FileRanges)
	}
	if unconfirmed.Result != CVEDetectionUnconfirmed || unconfirmed.Status != DevicePotentiallyVulnerable || unconfirmed.InternetAddressable != nil {
		t.Fatalf("unconfirmed = %+v", unconfirmed)
	}

	if _, _, err := newTestClient(t, server.URL).Vulnerabilities.Get(context.Background(), "net-1", " ", ""); err == nil {
		t.Fatal("Get() accepted an empty CVE ID")
	}
}

// The v1 view pages with offset/limit and has no v parameter.
func TestVulnerabilitiesListByOSPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.EscapedPath() != "/api/networks/net-1/vulnerabilities" || q.Has("v") ||
			q.Get("offset") != "200" || q.Get("limit") != "100" || q.Get("snapshotId") != "1021" {
			t.Errorf("request = %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"offset":200,"total":1234,"indexCreatedAt":"2025-06-01T12:34:56.789Z","vulnerabilities":[
		  {"id":"CVE-2019-0201","severity":"MEDIUM","vendor":"ARISTA","os":"arista_eos","osVersions":["4.15.0F"],
		   "dependsOnConfig":true,"detectionMethod":"CONFIG","knownExploitSource":"CISA",
		   "deviceResults":[{"device":"dev01","vulnerable":false,"fileLines":{"dev01,configuration.txt":[{"start":117,"end":119}]}},
		                    {"device":"dev02"}]}]}`)
	}))
	defer server.Close()

	offset, limit := int32(200), int32(100)
	got, _, err := newTestClient(t, server.URL).Vulnerabilities.ListByOS(context.Background(), "net-1",
		VulnerabilityByOSOptions{SnapshotID: "1021", Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("ListByOS() error = %v", err)
	}
	if got.Offset != 200 || got.Total != 1234 || len(got.Vulnerabilities) != 1 {
		t.Fatalf("ListByOS() = %+v", got)
	}
	v := got.Vulnerabilities[0]
	if v.Severity != VulnerabilitySeverityMedium || v.DetectionMethod != "CONFIG" || v.KnownExploitSource != "CISA" || len(v.DeviceResults) != 2 {
		t.Fatalf("vulnerability = %+v", v)
	}
	// A result whose analysis errored has no verdict, which is not "not vulnerable".
	if v.DeviceResults[0].Vulnerable == nil || *v.DeviceResults[0].Vulnerable || v.DeviceResults[1].Vulnerable != nil {
		t.Fatalf("device results = %+v", v.DeviceResults)
	}
}
