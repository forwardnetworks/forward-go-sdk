package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Azure and GCP selections and their discovered supersets are typed, so a
// caller compares configured against discovered without parsing Raw; an
// unselected Azure setup (empty subscriptionIds) reads as nil, which Forward
// defines as "every discovered subscription".
func TestCloudAccountCarriesSubscriptionAndProjectIDs(t *testing.T) {
	var azure, gcp CloudAccount
	if err := json.Unmarshal([]byte(`{"type":"AZURE","name":"az","discoveredSubscriptionIds":["s1","s2"],"testResults":{"s1":{"testInstant":5}}}`), &azure); err != nil {
		t.Fatal(err)
	}
	if azure.SubscriptionIDs != nil || len(azure.DiscoveredSubscriptionIDs) != 2 || len(azure.TestResults) != 1 {
		t.Fatalf("azure = %+v", azure)
	}
	if err := json.Unmarshal([]byte(`{"type":"GCP","name":"g","projectIds":["p1"],"discoveredProjectIds":["p1","p2"]}`), &gcp); err != nil {
		t.Fatal(err)
	}
	if len(gcp.ProjectIDs) != 1 || gcp.ProjectIDs[0] != "p1" || len(gcp.DiscoveredProjectIDs) != 2 {
		t.Fatalf("gcp = %+v", gcp)
	}
}

func TestNewlyDiscoveredReadsTheViewAndKeysBySetup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/networks/net-1/cloudAccounts" || r.URL.RawQuery != "view=newlyDiscovered" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"aws-gov":[{"id":"111122223333","name":"mission-a"}],"az":[]}`)
	}))
	defer server.Close()
	got, _, err := newTestClient(t, server.URL).CloudAccounts.NewlyDiscovered(context.Background(), "net-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got["aws-gov"]) != 1 || got["aws-gov"][0] != (DiscoveredCloudAccount{ID: "111122223333", Name: "mission-a"}) || got["az"] == nil || len(got["az"]) != 0 {
		t.Fatalf("got = %+v", got)
	}
}

// Discover is a bodyless POST that names its JSON media type (the route
// consumes JSON with an optional body), escapes the setup name as one path
// segment, and keeps a CAP entity's agency, mission and roles so a filter
// pair (id, role) can be checked against them.
func TestDiscoverSendsABodylessPostAndKeepsPortalEntities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/networks/net-1/cloudAccounts/aws%2Fgov/discovery" || len(body) != 0 || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s body=%q content-type=%q", r.Method, r.URL.EscapedPath(), body, r.Header.Get("Content-Type"))
		}
		_, _ = io.WriteString(w, `{"error":"NONE","discoveredEntities":[{"id":"111122223333","agency":"DOD","mission":"A","roles":["Auditor","Admin"]},{"id":"444455556666","mission":"B","roles":["Auditor"],"errorMsg":"denied"}]}`)
	}))
	defer server.Close()
	got, _, err := newTestClient(t, server.URL).CloudAccounts.Discover(context.Background(), "net-1", "aws/gov")
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != "NONE" || len(got.Entities) != 2 || got.Entities[0].Agency != "DOD" || len(got.Entities[0].Roles) != 2 || got.Entities[0].Roles[1] != "Admin" || got.Entities[1].ErrorMsg != "denied" {
		t.Fatalf("got = %+v", got)
	}
	if _, _, err := newTestClient(t, server.URL).CloudAccounts.Discover(context.Background(), "net-1", " "); err == nil {
		t.Fatal("a blank setup name must fail before any request")
	}
}
