package forward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Forward's DeviceDiscoveryParams is a union keyed on "type", and its mapper
// rejects properties it does not know, so each params type must marshal with its
// own type and nothing extra, and the collector must travel as the ?collectorId
// query parameter (the controller reads it with @RequestParam) and never in the
// body. Start returns the collector task id and refuses what Forward cannot run
// before any request: empty scans, no seeds and a nil params. DIRECT is sent as just
// its type (Forward builds the device list itself).
func TestDeviceDiscoveryStart(t *testing.T) {
	t.Parallel()

	var gotURL, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Method + " " + r.URL.String()
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"taskId":42}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	task, _, err := client.DeviceDiscovery.Start(ctx, "5", "C2", SubnetScanParams{Subnets: []string{"10.0.0.0/24"}, Protocols: []string{"SSH"}, TryDirectConnection: true})
	if err != nil {
		t.Fatal(err)
	}
	if task != "42" || gotURL != "POST /api/networks/5/deviceDiscovery?collectorId=C2&op=start" {
		t.Fatalf("task %q, request %s", task, gotURL)
	}
	if gotBody != `{"protocols":["SSH"],"subnets":["10.0.0.0/24"],"tryDirectConnection":true,"type":"SUBNET_SCAN"}` || strings.Contains(gotBody, "collectorId") {
		t.Fatalf("body %s", gotBody)
	}
	if _, _, err := client.DeviceDiscovery.Start(ctx, "5", "", SeededDiscoveryParams{SeedDevices: []string{"10.0.0.1"}, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	if gotURL != "POST /api/networks/5/deviceDiscovery?op=start" || gotBody != `{"recursive":true,"seedDevices":["10.0.0.1"],"type":"SEEDED"}` {
		t.Fatalf("seeded: %s %s", gotURL, gotBody)
	}
	if _, _, err := client.DeviceDiscovery.Start(ctx, "5", "", CloudDiscoveryParams{SetupIDs: []string{"aws1"}}); err != nil || gotBody != `{"setupIds":["aws1"],"type":"CLOUD"}` {
		t.Fatalf("cloud: %v %s", err, gotBody)
	}

	if _, _, err := client.DeviceDiscovery.Start(ctx, "5", "", DirectDiscoveryParams{}); err != nil || gotBody != `{"type":"DIRECT"}` {
		t.Fatalf("direct: %v %s", err, gotBody)
	}
	sent := gotURL
	gotURL = ""
	for name, bad := range map[string]DiscoveryParams{"empty scan": SubnetScanParams{}, "no seeds": SeededDiscoveryParams{}, "nil": nil} {
		if _, _, err := client.DeviceDiscovery.Start(ctx, "5", "", bad); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if gotURL != "" {
		t.Fatalf("a refused discovery reached Forward (after %s)", sent)
	}
}

// Stop is a bodiless POST answering 204, and ListDiscovered reads the jobs and
// the devices with their job id, so a caller can add a discovered device with
// its job's collector. since goes as an ISO instant.
func TestDeviceDiscoveryStopAndList(t *testing.T) {
	t.Parallel()

	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.Method + " " + r.URL.String()
		if strings.Contains(r.URL.Path, "discovered-devices") {
			_, _ = w.Write([]byte(`{"jobs":[{"id":"j1","type":"SUBNET_SCAN","collectorId":"C2","startedAt":"2026-10-16T01:00:00Z","counts":{"queued":0,"discovered":2,"inaccessible":1},"error":"NONE"}],
				"devices":[{"name":"edge1","type":"cisco_ios_xe_ssh","host":"10.0.0.7","port":22,"jobId":"j1","discoveredAt":"2026-10-16T01:05:00Z"}]}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if _, err := client.DeviceDiscovery.Stop(ctx, "5"); err != nil || gotURL != "POST /api/networks/5/deviceDiscovery?op=stop" {
		t.Fatalf("stop: %v %s", err, gotURL)
	}
	found, _, err := client.DeviceDiscovery.ListDiscovered(ctx, "5", time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "GET /api/networks/5/discovered-devices?since=2026-10-16T00%3A00%3A00Z" {
		t.Fatalf("list request %s", gotURL)
	}
	job := found.JobFor(found.Devices[0])
	if len(found.Devices) != 1 || job == nil || job.CollectorID != "C2" || job.Counts.Discovered != 2 || *found.Devices[0].Port != 22 {
		t.Fatalf("decoded %+v", found)
	}
	if _, _, err := client.DeviceDiscovery.ListDiscovered(ctx, "5", time.Time{}); err != nil || gotURL != "GET /api/networks/5/discovered-devices" {
		t.Fatalf("no since must send none: %v %s", err, gotURL)
	}
}

// A scheduled discovery is a name, an enabled flag, the typed params and an
// optional weekly or monthly schedule (none = after every collection). Forward
// refuses CLOUD there, a schedule that is both or neither form, and a bad time,
// and the stored one comes back with its params decoded to the right type.
func TestDeviceDiscoveryScheduled(t *testing.T) {
	t.Parallel()

	var gotBody string
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/missing"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiUrl":"/api/networks/5/scheduled-discoveries/missing","httpMethod":"GET","message":"Network \"5\" with scheduled discovery \"missing\" not found"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"7","enabled":false,"name":"nightly","params":{"type":"SEEDED","seedDevices":["10.0.0.1"],"recursive":true},"schedule":{"daysOfTheWeek":[1,3],"times":["02:00"]}}`))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	created, _, err := client.DeviceDiscovery.CreateScheduled(ctx, "5", ScheduledDiscoveryDefinition{
		Name: "nightly", Params: SeededDiscoveryParams{SeedDevices: []string{"10.0.0.1"}, Recursive: true},
		Schedule: &DiscoverySchedule{DaysOfTheWeek: []int{1, 3}, Times: []string{"02:00"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != `{"enabled":false,"name":"nightly","params":{"recursive":true,"seedDevices":["10.0.0.1"],"type":"SEEDED"},"schedule":{"daysOfTheWeek":[1,3],"times":["02:00"]}}` {
		t.Fatalf("body %s", gotBody)
	}
	seeded, ok := created.Params.(SeededDiscoveryParams)
	if !ok || created.ID != "7" || !seeded.Recursive || created.Schedule == nil || created.Schedule.Times[0] != "02:00" {
		t.Fatalf("created %+v", created)
	}
	if _, err := client.DeviceDiscovery.DeleteScheduled(ctx, "5", "7"); err != nil {
		t.Fatal(err)
	}
	// Forward words a missing schedule like a missing network; it must not read as ErrNetworkNotFound.
	if _, _, err := client.DeviceDiscovery.GetScheduled(ctx, "5", "missing"); err == nil || errors.Is(err, ErrNetworkNotFound) || !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("a missing scheduled discovery is a plain 404, got %v", err)
	}

	sent := len(calls)
	one, two := 1, 2
	for name, def := range map[string]ScheduledDiscoveryDefinition{
		"cloud":      {Name: "c", Params: CloudDiscoveryParams{SetupIDs: []string{"a"}}},
		"no name":    {Params: DirectDiscoveryParams{}},
		"both forms": {Name: "n", Params: DirectDiscoveryParams{}, Schedule: &DiscoverySchedule{DaysOfTheWeek: []int{1}, Times: []string{"01:00"}, Time: "01:00", DayOfTheWeek: &one, WeekOfTheMonth: &two}},
		"bad time":   {Name: "n", Params: DirectDiscoveryParams{}, Schedule: &DiscoverySchedule{DaysOfTheWeek: []int{1}, Times: []string{"25:00"}}},
		"empty scan": {Name: "n", Params: SubnetScanParams{}},
		"week of 5":  {Name: "n", Params: DirectDiscoveryParams{}, Schedule: &DiscoverySchedule{DayOfTheWeek: &one, WeekOfTheMonth: &[]int{5}[0], Time: "01:00"}},
	} {
		if _, _, err := client.DeviceDiscovery.CreateScheduled(ctx, "5", def); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(calls) != sent {
		t.Fatal("a refused schedule reached Forward")
	}
}
