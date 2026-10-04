package forward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDashboardsReads(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/networks/N1/dashboards":
			if r.URL.Query().Get("type") == "DEFAULT" {
				_, _ = io.WriteString(w, `[{"id":"default-network","name":"Network"}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"id":"5","name":"NOC wall","description":"big screen","layout":[{"widget":"checks"}],"createdBy":"mary","createdAt":"2026-10-01T09:00:00Z","extraField":1}]`)
		case "/api/networks/N1/dashboards/5":
			_, _ = io.WriteString(w, `{"id":"5","name":"NOC wall"}`)
		case "/api/networks/N1/dashboards/5/display-settings":
			_, _ = io.WriteString(w, `{"autoScrollEnabled":true,"autoScrollMode":"VERTICAL","autoScrollIntervalSec":30}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	dashboards := newTestClient(t, server.URL).Dashboards
	ctx := context.Background()

	list, _, err := dashboards.List(ctx, "N1")
	if err != nil || len(list) != 1 || list[0].ID != "5" || list[0].CreatedBy != "mary" || string(list[0].Layout) != `[{"widget":"checks"}]` || !strings.Contains(string(list[0].Raw), "extraField") {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	defaults, _, err := dashboards.Defaults(ctx, "N1")
	if err != nil || len(defaults) != 1 || defaults[0].Name != "Network" || uris[1] != "/api/networks/N1/dashboards?type=DEFAULT" {
		t.Fatalf("Defaults() = %+v, %v; %s", defaults, err, uris[1])
	}
	if got, _, err := dashboards.Get(ctx, "N1", "5"); err != nil || got.Name != "NOC wall" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if got, _, err := dashboards.Get(ctx, "N1", "404"); got != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", got, err)
	}
	settings, _, err := dashboards.DisplaySettings(ctx, "N1", "5")
	if err != nil || settings.AutoScrollEnabled == nil || !*settings.AutoScrollEnabled || settings.AutoScrollMode != "VERTICAL" || *settings.AutoScrollIntervalSec != 30 {
		t.Fatalf("DisplaySettings() = %+v, %v", settings, err)
	}
	if _, _, err := dashboards.Get(ctx, "N1", " "); err == nil {
		t.Fatal("a blank dashboard ID must be refused")
	}
}

func TestScorecardsReads(t *testing.T) {
	t.Parallel()

	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/networks/N1/scorecards" && q.Get("snapshotId") != "":
			_, _ = io.WriteString(w, `[{"id":"1","name":"Health","score":82.5,"categories":[{"id":"c1","name":"Reachability","systemManaged":true,"weight":60,"checks":[{"name":"x","score":1.0}]}]}]`)
		case r.URL.Path == "/api/networks/N1/scorecards" && q.Get("view") == "definitions":
			_, _ = io.WriteString(w, `[{"id":"1","name":"Health","categoryWeights":{"c1":60,"c2":40}}]`)
		case r.URL.Path == "/api/networks/N1/scorecards" && q.Get("view") == "trends":
			_, _ = io.WriteString(w, `{"1":[{"snapshotId":"13790","instant":1759395000000,"score":80.0},{"snapshotId":"13791","instant":1759481400000}]}`)
		case r.URL.Path == "/api/networks/N1/kpi-categories":
			_, _ = io.WriteString(w, `[{"id":"c1","name":"Reachability","systemManaged":true,"checks":[]}]`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	scorecards := newTestClient(t, server.URL).Scorecards
	ctx := context.Background()

	scored, _, err := scorecards.ForSnapshot(ctx, "N1", "13790")
	if err != nil || len(scored) != 1 || scored[0].Score == nil || *scored[0].Score != 82.5 || scored[0].Categories[0].Weight != 60 || !scored[0].Categories[0].SystemManaged || len(scored[0].Categories[0].Checks) == 0 {
		t.Fatalf("ForSnapshot() = %+v, %v", scored, err)
	}
	defs, _, err := scorecards.Definitions(ctx, "N1")
	if err != nil || defs[0].CategoryWeights["c2"] != 40 {
		t.Fatalf("Definitions() = %+v, %v", defs, err)
	}
	start, end := time.Date(2026, 9, 1, 2, 0, 0, 0, time.FixedZone("x", 3600)), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	trends, _, err := scorecards.Trends(ctx, "N1", start, end, 0)
	if err != nil || uris[2] != "/api/networks/N1/scorecards?end=2026-10-01T00%3A00%3A00Z&start=2026-09-01T01%3A00%3A00Z&view=trends" {
		t.Fatalf("Trends: %v %s", err, uris[2])
	}
	points := trends["1"]
	if len(points) != 2 || !points[0].Time.Equal(time.UnixMilli(1759395000000)) || points[0].Score == nil || *points[0].Score != 80 || points[1].Score != nil || points[1].SnapshotID != "13791" {
		t.Fatalf("points = %+v", points)
	}
	if _, _, err := scorecards.Trends(ctx, "N1", start, end, 30); err != nil || !strings.Contains(uris[3], "maxPoints=30") {
		t.Fatalf("maxPoints must be sent when stated: %v %s", err, uris[3])
	}
	cats, _, err := scorecards.KPICategories(ctx, "N1")
	if err != nil || cats[0].ID != "c1" {
		t.Fatalf("KPICategories() = %+v, %v", cats, err)
	}
	if _, _, err := scorecards.KPICategoryDefinitions(ctx, "N1"); err != nil || uris[len(uris)-1] != "/api/networks/N1/kpi-categories?view=definitions" {
		t.Fatalf("KPICategoryDefinitions: %v %s", err, uris[len(uris)-1])
	}
	for name, f := range map[string]func() error{
		"no snapshot":      func() error { _, _, err := scorecards.ForSnapshot(ctx, "N1", " "); return err },
		"end before start": func() error { _, _, err := scorecards.Trends(ctx, "N1", end, start, 0); return err },
		"zero start":       func() error { _, _, err := scorecards.Trends(ctx, "N1", time.Time{}, end, 0); return err },
		"one point":        func() error { _, _, err := scorecards.Trends(ctx, "N1", start, end, 1); return err },
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestChecksReports(t *testing.T) {
	t.Parallel()

	var uris []string
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.URL.RequestURI())
		accept = r.Header.Get("Accept")
		if r.URL.Path == "/api/snapshots/S9/checksReport" {
			_, _ = w.Write([]byte("PK\x03\x04workbook bytes"))
			return
		}
		_, _ = io.WriteString(w, "not a workbook")
	}))
	defer server.Close()
	checks := newTestClient(t, server.URL).Checks
	ctx := context.Background()

	var out bytes.Buffer
	n, _, err := checks.ChecksReport(ctx, "S9", ChecksReportOptions{}, &out)
	if err != nil || n != int64(out.Len()) || uris[0] != "/api/snapshots/S9/checksReport" || !strings.Contains(accept, "spreadsheetml") {
		t.Fatalf("ChecksReport: n=%d err=%v uri=%s accept=%s", n, err, uris[0], accept)
	}
	if _, _, err := checks.CheckCategoryReport(ctx, "S1", "intent", "/dc1", ChecksReportOptions{}, io.Discard); err == nil || !strings.Contains(err.Error(), "did not return a workbook") || uris[1] != "/api/snapshots/S1/checks-report?dir=%2Fdc1&type=INTENT" {
		t.Fatalf("a non-workbook body must be an error: %v %s", err, uris[1])
	}
	for name, f := range map[string]func() error{
		"unknown category": func() error {
			_, _, err := checks.CheckCategoryReport(ctx, "S1", "LOGS", "", ChecksReportOptions{}, io.Discard)
			return err
		},
		"dir on non-intent": func() error {
			_, _, err := checks.CheckCategoryReport(ctx, "S1", "NQE", "/x", ChecksReportOptions{}, io.Discard)
			return err
		},
		"nil writer": func() error { _, _, err := checks.ChecksReport(ctx, "S1", ChecksReportOptions{}, nil); return err },
		"blank snapshot": func() error {
			_, _, err := checks.ChecksReport(ctx, " ", ChecksReportOptions{}, io.Discard)
			return err
		},
	} {
		if err := f(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}
