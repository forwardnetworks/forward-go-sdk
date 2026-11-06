package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type snowSeen struct{ method, uri, body string }

func snowServer(t *testing.T, seen *[]snowSeen, answer func(r *http.Request) (int, string)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = append(*seen, snowSeen{r.Method, r.URL.RequestURI(), string(b)})
		status, body := answer(r)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return newTestClient(t, server.URL)
}

// The check-incident calls are the same routes as the check calls, told apart by their query: each action and view must reach the wire
// exactly as CheckController maps it, the incident number goes in the query, and a refusal never reaches the server.
func TestServiceNowCheckIncidents(t *testing.T) {
	t.Parallel()
	var seen []snowSeen
	link := `{"checkId":"12","number":"INC1","state":"New","stateLastUpdated":1700000000000}`
	in := snowServer(t, &seen, func(r *http.Request) (int, string) {
		switch {
		case r.Method == "GET" && r.URL.Query().Get("view") == "serviceNowIncidents":
			return 200, "[" + link + "]"
		case r.Method == "GET":
			return 200, `[{"snapshotCreationTime":1,"totalFailures":2}]`
		case r.URL.Query().Get("action") == "unlinkServiceNowIncident" || r.URL.Query().Get("action") == "refreshServiceNowIncidents":
			return 204, ""
		case r.URL.Query().Get("action") == "addServiceNowIncidents":
			return 200, "[" + link + "]"
		}
		return 200, link
	}).Integrations
	ctx := context.Background()

	links, _, err := in.ListCheckIncidents(ctx, "99", []string{CheckTypeNetworkQuery, CheckTypePredefined})
	if err != nil || len(links) != 1 || links[0].Number != "INC1" || links[0].StateLastUpdated != 1700000000000 || links[0].State != "New" {
		t.Fatalf("links = %+v, %v", links, err)
	}
	input := ServiceNowIncidentInput{Summary: "down", Details: "d", Impact: ServiceNowIncidentLevelHigh, Urgency: ServiceNowIncidentLevelLow}
	if _, _, err := in.AddCheckIncident(ctx, "99", "12", input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := in.AddCheckIncidents(ctx, "99", map[string]ServiceNowIncidentInput{"12": input}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := in.LinkCheckIncident(ctx, "99", "12", "INC 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.UnlinkCheckIncident(ctx, "99", "12", "INC1"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.RefreshCheckIncidents(ctx, "99"); err != nil {
		t.Fatal(err)
	}
	if h, _, err := in.CheckIncidentUpdateHistory(ctx, "99", "12", "INC1"); err != nil || len(h) != 1 || h[0].TotalFailures != 2 {
		t.Fatalf("history = %+v, %v", h, err)
	}

	want := []snowSeen{
		{"GET", "/api/snapshots/99/checks?type=NQE&type=Predefined&view=serviceNowIncidents", ""},
		{"POST", "/api/snapshots/99/checks/12?action=addServiceNowIncident", `{"summary":"down","details":"d","impact":"1","urgency":"3"}`},
		{"POST", "/api/snapshots/99/checks?action=addServiceNowIncidents", `{"12":{"summary":"down","details":"d","impact":"1","urgency":"3"}}`},
		{"POST", "/api/snapshots/99/checks/12?action=linkServiceNowIncident&incident=INC+1", ""},
		{"POST", "/api/snapshots/99/checks/12?action=unlinkServiceNowIncident&incident=INC1", ""},
		{"POST", "/api/snapshots/99/checks?action=refreshServiceNowIncidents", ""},
		{"GET", "/api/snapshots/99/checks/12?incident=INC1&view=serviceNowIncidentUpdateHistory", ""},
	}
	if len(seen) != len(want) {
		t.Fatalf("requests = %+v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, seen[i], want[i])
		}
	}

	before := len(seen)
	bad := []error{}
	_, _, e1 := in.ListCheckIncidents(ctx, "99", nil)
	_, _, e2 := in.AddCheckIncident(ctx, "99", "12", ServiceNowIncidentInput{Summary: "x", Impact: "HIGH", Urgency: "1"})
	_, _, e3 := in.AddCheckIncident(ctx, "99", "12", ServiceNowIncidentInput{Impact: "1", Urgency: "1"})
	_, _, e4 := in.AddCheckIncidents(ctx, "99", nil)
	_, _, e5 := in.LinkCheckIncident(ctx, "99", "12", " ")
	_, e6 := in.UnlinkCheckIncident(ctx, "", "12", "INC1")
	for _, err := range append(bad, e1, e2, e3, e4, e5, e6) {
		if err == nil {
			t.Error("a refused call returned no error")
		}
	}
	if len(seen) != before {
		t.Fatalf("refused calls reached the server: %+v", seen[before:])
	}
}

// Incidents by number: one close is a POST with an action query and no body, closing several sends the numbers as a JSON array, and a
// blank number is refused before the wire.
func TestServiceNowIncidents(t *testing.T) {
	t.Parallel()
	var seen []snowSeen
	in := snowServer(t, &seen, func(r *http.Request) (int, string) {
		if r.Method == "GET" {
			return 200, `{"number":"INC1","sysId":"s1","summary":"x","impact":"1","urgency":"2","state":"In Progress","url":"https://i.service-now.com/nav_to.do?uri=/incident.do?sys_id=s1"}`
		}
		return 204, ""
	}).Integrations
	ctx := context.Background()

	got, _, err := in.GetServiceNowIncident(ctx, "INC1")
	if err != nil || got.SysID != "s1" || got.State != "In Progress" || got.Urgency != ServiceNowIncidentLevelMedium {
		t.Fatalf("incident = %+v, %v", got, err)
	}
	if _, err := in.CloseServiceNowIncident(ctx, "INC1"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.CloseServiceNowIncidents(ctx, []string{"INC1", " ", "INC2"}); err != nil {
		t.Fatal(err)
	}
	want := []snowSeen{
		{"GET", "/api/integrations/servicenow/incidents/INC1", ""},
		{"POST", "/api/integrations/servicenow/incidents/INC1?action=close", ""},
		{"POST", "/api/integrations/servicenow/incidents?action=close", `["INC1","INC2"]`},
	}
	for i := range want {
		if i >= len(seen) || seen[i] != want[i] {
			t.Fatalf("requests = %+v, want %+v", seen, want)
		}
	}
	if _, err := in.CloseServiceNowIncidents(ctx, []string{" "}); err == nil {
		t.Error("closing no incidents was accepted")
	}
	if _, _, err := in.GetServiceNowIncident(ctx, ""); err == nil {
		t.Error("an empty incident number was accepted")
	}
	if len(seen) != len(want) {
		t.Fatalf("refused calls reached the server: %+v", seen[len(want):])
	}
}

// UpdateServiceNow must send only the fields the caller set: a flag left nil is not switched off, which is what the full-state request
// would do. A token together with a username or password is refused first, as Forward refuses it.
func TestServiceNowSettingsUpdate(t *testing.T) {
	t.Parallel()
	var seen []snowSeen
	in := snowServer(t, &seen, func(r *http.Request) (int, string) {
		return 200, `{"instanceUrl":"https://i.service-now.com","token":"","enabled":true,"autoCreateImpact":"3"}`
	}).Integrations
	ctx := context.Background()

	autoCreate := true
	token := "t0k"
	got, _, err := in.UpdateServiceNow(ctx, ServiceNowSettingsUpdate{Token: &token, AutoCreate: &autoCreate})
	if err != nil || got.Token == nil || *got.Token != "" || got.Password != nil || !got.Enabled {
		t.Fatalf("settings = %+v, %v", got, err)
	}
	if len(seen) != 1 || seen[0].method != "PATCH" || seen[0].uri != "/api/integrations/servicenow" || seen[0].body != `{"token":"t0k","autoCreate":true}` {
		t.Fatalf("request = %+v", seen)
	}
	user := "u"
	if _, _, err := in.UpdateServiceNow(ctx, ServiceNowSettingsUpdate{Token: &token, Username: &user}); err == nil || len(seen) != 1 {
		t.Fatalf("token with username reached the wire: err=%v seen=%d", err, len(seen))
	}
	// The full-state request leaves the unused credential out so token auth is possible at all.
	raw, _ := json.Marshal(ServiceNowIntegrationRequest{InstanceURL: "https://i", Token: "t", Enabled: true})
	if body := string(raw); strings.Contains(body, "username") || strings.Contains(body, "password") {
		t.Errorf("token request carries a username or password: %s", body)
	}
}

// The CMDB calls: connection-taking routes send the connection as the body (secrets and the new certificate, client and proxy fields
// included), a failed test is a 200 whose result says so, a missing configuration reads as absent rather than an error, and the
// downloads stream the body to the writer.
func TestServiceNowCMDBRoutes(t *testing.T) {
	t.Parallel()
	var seen []snowSeen
	in := snowServer(t, &seen, func(r *http.Request) (int, string) {
		switch {
		case r.URL.Path == "/api/integrations/servicenow-cmdb/test":
			return 200, `{"errorMessage":"bad login","error":{"errorType":"AUTH_INVALID_CREDENTIALS","userMessage":"bad login","count":1}}`
		case r.URL.Path == "/api/integrations/servicenow-cmdb/configuration" && r.Method == "GET":
			return 404, `{"message":"No configuration found"}`
		case r.URL.Path == "/api/integrations/servicenow-cmdb/synchronization-history":
			return 200, `{"infos":[{"syncInstant":1700000000000,"networkName":"n","entitiesInserted":4,"entitiesUpdated":0},{"syncInstant":1,"networkName":"m","error":"boom","errors":[{"errorType":"CONNECTION_TIMEOUT","userMessage":"slow","count":2,"fieldContext":{"ciTableName":"cmdb_ci","fieldName":"name"}}]}]}`
		case r.URL.Path == "/api/integrations/servicenow-cmdb/status":
			return 200, `{"status":"INCORRECT_MAPPINGS","missingClassNames":["cmdb_ci_x"]}`
		case r.URL.Query().Get("view") == "payload" && r.URL.Query().Get("disposition") == "attachment":
			return 200, "PK-zip"
		case r.URL.Query().Get("view") == "payload":
			return 200, `{"tables":[{"tableName":"cmdb_ci","rows":[{"name":"r1","ip":"10.0.0.1"}]}]}`
		case r.URL.Query().Get("view") != "":
			return 200, `["a","b"]`
		case r.URL.Path == "/api/integrations/servicenow-cmdb/synchronization-report":
			return 200, "PK-xlsx"
		}
		return 204, ""
	}).Integrations
	ctx := context.Background()

	conn := ServiceNowConnectionDetails{
		InstanceURL: "https://i.service-now.com", Username: "u", Password: "",
		ClientID: "cid", ClientSecret: "cs", ProxyConfig: &ServiceNowCMDBProxy{Host: "http://proxy", Port: 3128},
	}
	if got, _, err := in.ServiceNowCMDBDiscoverySources(ctx, conn); err != nil || len(got) != 2 {
		t.Fatalf("sources = %v, %v", got, err)
	}
	if got, _, err := in.ServiceNowCMDBConfigurationItems(ctx, conn); err != nil || len(got) != 2 {
		t.Fatalf("items = %v, %v", got, err)
	}
	res, _, err := in.TestServiceNowCMDB(ctx, conn)
	if err != nil || res.OK() || res.Error.ErrorType != "AUTH_INVALID_CREDENTIALS" {
		t.Fatalf("test result = %+v, %v", res, err)
	}
	if cfg, found, _, err := in.GetServiceNowCMDBConfiguration(ctx); err != nil || found || cfg != nil {
		t.Fatalf("missing configuration = %+v, %v, %v", cfg, found, err)
	}
	hist, _, err := in.ServiceNowCMDBSyncHistory(ctx)
	if err != nil || len(hist) != 2 || *hist[0].EntitiesInserted != 4 || hist[1].Error != "boom" || hist[1].Errors[0].FieldContext.FieldName != "name" {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	st, _, err := in.ServiceNowCMDBStatusReport(ctx)
	if err != nil || st.Status != ServiceNowCMDBStatusIncorrectMappings || st.MissingClassNames[0] != "cmdb_ci_x" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	pay, _, err := in.ServiceNowCMDBPayload(ctx, "7")
	if err != nil || pay.Tables[0].Rows[0]["name"] != "r1" {
		t.Fatalf("payload = %+v, %v", pay, err)
	}
	var zip, xlsx bytes.Buffer
	if _, err := in.DownloadServiceNowCMDBPayload(ctx, &zip); err != nil || zip.String() != "PK-zip" {
		t.Fatalf("zip = %q, %v", zip.String(), err)
	}
	if _, err := in.DownloadServiceNowCMDBSyncReport(ctx, &xlsx); err != nil || xlsx.String() != "PK-xlsx" {
		t.Fatalf("xlsx = %q, %v", xlsx.String(), err)
	}
	for _, call := range []func() (*Response, error){
		func() (*Response, error) { return in.DisableServiceNowCMDB(ctx) },
		func() (*Response, error) { return in.PushServiceNowCMDB(ctx) },
		func() (*Response, error) { return in.DeleteServiceNowCMDBConfiguration(ctx) },
	} {
		if _, err := call(); err != nil {
			t.Fatal(err)
		}
	}

	connBody := `{"instanceUrl":"https://i.service-now.com","username":"u","password":"","clientId":"cid","clientSecret":"cs","proxyConfig":{"host":"http://proxy","port":3128}}`
	want := []snowSeen{
		{"POST", "/api/integrations/servicenow-cmdb?view=discovery-sources", connBody},
		{"POST", "/api/integrations/servicenow-cmdb?view=configuration-items", connBody},
		{"POST", "/api/integrations/servicenow-cmdb/test", connBody},
		{"GET", "/api/integrations/servicenow-cmdb/configuration", ""},
		{"GET", "/api/integrations/servicenow-cmdb/synchronization-history", ""},
		{"GET", "/api/integrations/servicenow-cmdb/status", ""},
		{"GET", "/api/integrations/servicenow-cmdb?networkId=7&view=payload", ""},
		{"GET", "/api/integrations/servicenow-cmdb?view=payload&disposition=attachment", ""},
		{"GET", "/api/integrations/servicenow-cmdb/synchronization-report", ""},
		{"POST", "/api/integrations/servicenow-cmdb?action=disable", ""},
		{"POST", "/api/integrations/servicenow-cmdb?action=push", ""},
		{"DELETE", "/api/integrations/servicenow-cmdb/configuration", ""},
	}
	if len(seen) != len(want) {
		t.Fatalf("requests = %+v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, seen[i], want[i])
		}
	}
	if _, _, err := in.ServiceNowCMDBPayload(ctx, " "); err == nil || len(seen) != len(want) {
		t.Errorf("an empty network ID reached the wire: %v", err)
	}
}
