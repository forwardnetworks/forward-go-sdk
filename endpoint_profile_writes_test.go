package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A create carries only the published fields of its type -- an SNMP body has
// no CLI or HTTP keys -- and never the read-only id or attribution. Forward
// answers 201 with the stored profile.
func TestEndpointsCreateProfileDefinition(t *testing.T) {
	t.Parallel()

	var query string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		body = nil
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"SNMP-5","type":"SNMP","name":"printer","detectorOid":"1.3.6.1.2.1.1.2.0","createdBy":"mary"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	timeout := 20
	def := EndpointProfile{
		ID: "SNMP-99", Type: "snmp", Name: " printer ", CreatedBy: "someone",
		DetectorOID: "1.3.6.1.2.1.1.2.0", DetectorPatterns: []string{"^1.3.6.1.4.1.11"}, ResponseTimeoutSec: &timeout,
		OIDSets: []string{"STANDARD"}, CustomOIDs: []CustomOID{{Name: "system_desc", OID: "1.3.6.1.2.1.1.1"}},
		CommandSets: []string{"UNIX"}, // a CLI field: must not be sent for SNMP
	}
	got, _, err := client.Endpoints.CreateProfileDefinition(context.Background(), def)
	if err != nil || got.ID != "SNMP-5" || query != "type=SNMP" {
		t.Fatalf("CreateProfileDefinition() = %+v, %v; query %q", got, err, query)
	}
	for _, key := range []string{"id", "createdBy", "commandSets"} {
		if _, ok := body[key]; ok {
			t.Errorf("an SNMP create must not send %q: %v", key, body)
		}
	}
	if body["name"] != "printer" || body["detectorOid"] != "1.3.6.1.2.1.1.2.0" || body["responseTimeoutSec"] != float64(20) || body["customOids"] == nil {
		t.Fatalf("SNMP body = %v", body)
	}
	if _, _, err := client.Endpoints.CreateProfileDefinition(context.Background(), EndpointProfile{Name: "x", Type: "HTTP"}); err == nil {
		t.Fatal("an HTTP profile without https, authType and endpoints must be refused locally")
	}
	if _, _, err := client.Endpoints.CreateProfileDefinition(context.Background(), EndpointProfile{Name: "x", Type: "TELNET"}); err == nil {
		t.Fatal("an unknown type must be refused locally")
	}
}

// Forward refuses to delete a profile endpoints still use (it does not orphan
// them), with a 400 that must read as ErrEndpointProfileInUse; a 404 is
// already gone.
func TestEndpointsDeleteProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		body   string
		inUse  bool
		ok     bool
	}{
		{status: http.StatusNoContent, ok: true},
		{status: http.StatusNotFound, body: `{"message":"No endpoint profile SNMP-5"}`, ok: true},
		{status: http.StatusBadRequest, body: `{"apiUrl":"/api/endpoint-profiles/SNMP-5","httpMethod":"DELETE","message":"Profile is still used in 1 network(s): [3347]"}`, inUse: true},
	} {
		var method, path string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.EscapedPath()
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, err := newTestClient(t, server.URL).Endpoints.DeleteProfile(context.Background(), " SNMP-5 ")
		server.Close()
		if method != http.MethodDelete || path != "/api/endpoint-profiles/SNMP-5" {
			t.Errorf("%d: sent %s %s", tc.status, method, path)
		}
		if tc.ok != (err == nil) || tc.inUse != errors.Is(err, ErrEndpointProfileInUse) {
			t.Errorf("%d: err = %v", tc.status, err)
		}
	}
}

// Both published schedule forms decode, and an absent schedule is nil.
func TestCollectionSchedules(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/networks/n1/collection-schedules":
			_, _ = io.WriteString(w, `{"schedules":[
			  {"id":"1","enabled":true,"timeZone":"America/Chicago","daysOfTheWeek":[1,2,3,4,5],"times":["01:00","13:00"]},
			  {"id":"2","enabled":false,"daysOfTheWeek":[0,6],"periodInSeconds":1800,"startAt":"05:30","endAt":"20:00"}]}`)
		case "/api/networks/n1/collection-schedules/9":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL).ForNetwork("n1")

	got, _, err := client.CollectionSchedules.List(context.Background(), "")
	if err != nil || len(got) != 2 {
		t.Fatalf("List() = %+v, %v", got, err)
	}
	daily, periodic := got[0], got[1]
	if daily.Periodic() || !daily.Enabled || daily.TimeZone != "America/Chicago" || len(daily.Times) != 2 || len(daily.DaysOfTheWeek) != 5 {
		t.Fatalf("daily = %+v", daily)
	}
	if !periodic.Periodic() || *periodic.PeriodInSeconds != 1800 || periodic.Enabled || periodic.StartAt != "05:30" || periodic.EndAt != "20:00" {
		t.Fatalf("periodic = %+v", periodic)
	}
	if s, _, err := client.CollectionSchedules.Get(context.Background(), "", "9"); s != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", s, err)
	}
}

// QueryCommitInfo flattens CommitInfo, CommitAuthor (authorId for a user,
// authorEmail otherwise) and CommitMessage.
func TestNQERepositoryHistory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/nqe/queries/Q_abc/history" {
			t.Errorf("path = %s", r.URL.EscapedPath())
		}
		_, _ = io.WriteString(w, `{"commits":[
		  {"path":"/vpn/edges","id":"c2","authorId":"12","author":"mary","committedAt":"2026-10-01T10:00:00Z","title":"Tighten edge match","body":""},
		  {"path":"/vpn/edge","id":"c1","authorEmail":"ci@example.com","committedAt":"2026-09-01T10:00:00Z","title":"Add edge query","body":"first"}]}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).NQERepository.History(context.Background(), " Q_abc ")
	if err != nil || len(got) != 2 || got[0].ID != "c2" || got[0].AuthorID != "12" || got[1].AuthorEmail != "ci@example.com" || got[1].Path != "/vpn/edge" || got[1].Title != "Add edge query" {
		t.Fatalf("History() = %+v, %v", got, err)
	}
}

// The dry run commits nothing: it reports the errors the commit would
// introduce (including in importing queries) and who uses the changed queries.
// With a snapshot it also types against that snapshot's data.
func TestNQERepositoryCommitDryRun(t *testing.T) {
	t.Parallel()

	var query string
	var body map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/nqe/repos/org/commits" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = io.WriteString(w, `{"newErrors":{"/vpn/report":[{"severity":"ERROR","source":"TYPE_CHECKER","message":"unknown field 'peers'","location":{"start":{"line":3,"column":5}}}]},
		  "uses":[{"type":"CHECK","checkId":"C1"}],"unauthorizedQueryChanges":[],"unauthorizedAccessSettingChanges":[]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	got, _, err := client.NQERepository.CommitDryRun(context.Background(), []string{"/vpn/edges", "/vpn/report"}, "")
	if err != nil || query != "dryRun=true" || len(body["paths"]) != 2 {
		t.Fatalf("CommitDryRun() err %v, query %q, body %v", err, query, body)
	}
	diags := got.NewErrors["/vpn/report"]
	if len(diags) != 1 || diags[0].Severity != "ERROR" || diags[0].Message != "unknown field 'peers'" || len(diags[0].Location) == 0 || len(got.Uses) != 1 {
		t.Fatalf("dry run = %+v", got)
	}
	if _, _, err := client.NQERepository.CommitDryRun(context.Background(), []string{"/vpn/edges"}, "9164"); err != nil || query != "dryRun=true&snapshotId=9164" {
		t.Fatalf("with snapshot: query %q, %v", query, err)
	}
	if _, _, err := client.NQERepository.CommitDryRun(context.Background(), nil, ""); err == nil {
		t.Fatal("a dry run of no paths must be refused")
	}
}
