package forward

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCollectorUpgradeWire pins both upgrade calls to POST with ?action=upgrade (without it Forward would read
// POST /api/collectors as a registration) and the all-collectors reply to a bare list of names.
func TestCollectorUpgradeWire(t *testing.T) {
	t.Parallel()
	var uris []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris = append(uris, r.Method+" "+r.RequestURI)
		if r.URL.Path == "/api/collectors" {
			_, _ = w.Write([]byte(`["edge-collector","lab"]`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	if _, err := client.Collectors.Upgrade(context.Background(), " edge collector "); err != nil {
		t.Fatal(err)
	}
	names, _, err := client.Collectors.UpgradeAll(context.Background())
	if err != nil || len(names) != 2 || names[0] != "edge-collector" {
		t.Fatalf("UpgradeAll = %v, %v", names, err)
	}
	want := []string{"POST /api/collectors/edge%20collector?action=upgrade", "POST /api/collectors?action=upgrade"}
	if strings.Join(uris, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v, want %v", uris, want)
	}
	if _, err := client.Collectors.Upgrade(context.Background(), " "); err == nil {
		t.Fatal("Upgrade with no name was sent")
	}
}

// TestCollectorUpgradeBlocker pins the advisory check to Forward's own order and statuses: an outdated, connected,
// service-installed collector is clear, and each other state names its reason.
func TestCollectorUpgradeBlocker(t *testing.T) {
	t.Parallel()
	ok := Collector{ConnectionStatus: "CONNECTED", UpdateStatus: "OUTDATED", Features: &CollectorFeatures{Latest: 9, Updatable: true}}
	if got := ok.UpgradeBlocker(); got != "" {
		t.Fatalf("outdated connected collector blocked: %q", got)
	}
	notSupported := ok
	notSupported.UpdateStatus = "NOT_SUPPORTED"
	if got := notSupported.UpgradeBlocker(); got != "" {
		t.Fatalf("NOT_SUPPORTED blocked: %q (Forward upgrades those too)", got)
	}
	queuedAndOffline := ok
	queuedAndOffline.CollectorUpdating, queuedAndOffline.ConnectionStatus = true, "DISCONNECTED"
	if got := queuedAndOffline.UpgradeBlocker(); !strings.Contains(got, "already queued") {
		t.Fatalf("queued and offline = %q; Forward checks in-progress (409) first", got)
	}
	for name, mutate := range map[string]func(*Collector){
		"queued":      func(c *Collector) { c.CollectorUpdating = true },
		"never":       func(c *Collector) { c.ConnectionStatus = "NEVER_CONNECTED" },
		"offline":     func(c *Collector) { c.ConnectionStatus = "DISCONNECTED" },
		"no features": func(c *Collector) { c.Features = nil },
		"not service": func(c *Collector) { c.Features = &CollectorFeatures{Latest: 9} },
		"dev":         func(c *Collector) { c.UpdateStatus = "" },
		"current":     func(c *Collector) { c.UpdateStatus = "UP_TO_DATE" },
		"eol":         func(c *Collector) { c.UpdateStatus = "END_OF_LIFE" },
	} {
		c := ok
		mutate(&c)
		if c.UpgradeBlocker() == "" {
			t.Errorf("%s: not blocked", name)
		}
	}
}

// TestCollectorListDecodesUpgradeFields decodes a CollectorWithStatus-shaped body, so the fields UpgradeBlocker reads are
// the names Forward serialises (features.updatable is omitted when false).
func TestCollectorListDecodesUpgradeFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"C1","name":"edge","username":"collector-edge","connectionStatus":"CONNECTED","version":"26.9.1-01","updateStatus":"OUTDATED","collectorUpdating":false,"features":{"latest":9,"updatable":true}},{"id":"C2","name":"old","username":"collector-old","connectionStatus":"CONNECTED","updateStatus":"OUTDATED","features":{"latest":3}}]`))
	}))
	defer server.Close()
	list, _, err := newTestClient(t, server.URL).Collectors.List(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %#v, %v", list, err)
	}
	if got := list[0].UpgradeBlocker(); got != "" {
		t.Errorf("edge blocked: %q", got)
	}
	if got := list[1].UpgradeBlocker(); !strings.Contains(got, "not remotely upgradeable") {
		t.Errorf("old = %q, want not remotely upgradeable", got)
	}
}
