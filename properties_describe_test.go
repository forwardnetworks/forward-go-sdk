package forward

import (
	"github.com/forwardnetworks/forward-go-sdk/domain"
	"github.com/forwardnetworks/forward-go-sdk/ports"
)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPropertyKindOf(t *testing.T) {
	t.Parallel()
	cases := map[string]domain.PropertyKind{
		`true`:           domain.PropertyKindBoolean,
		`false`:          domain.PropertyKindBoolean,
		`1000`:           domain.PropertyKindInteger,
		`-3`:             domain.PropertyKindInteger,
		`0.5`:            domain.PropertyKindNumber,
		`1e3`:            domain.PropertyKindNumber,
		`"LOW_PRIORITY"`: domain.PropertyKindString,
		`["a","b"]`:      domain.PropertyKindList,
		`{"k":1}`:        domain.PropertyKindObject,
		`null`:           domain.PropertyKindNull,
		``:               domain.PropertyKindNull,
		"  true \n":      domain.PropertyKindBoolean,
	}
	for raw, want := range cases {
		if got := domain.PropertyKindOf(json.RawMessage(raw)); got != want {
			t.Errorf("PropertyKindOf(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestPropertyValueString(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`true`:           "true",
		`1000`:           "1000",
		`"LOW_PRIORITY"`: "LOW_PRIORITY",
		`[ "a", "b" ]`:   `["a","b"]`,
		`{ "k" : 1 }`:    `{"k":1}`,
		`null`:           "",
	}
	for raw, want := range cases {
		if got := domain.PropertyValueString(json.RawMessage(raw)); got != want {
			t.Errorf("PropertyValueString(%q) = %q, want %q", raw, got, want)
		}
	}
	// "no value" must never read as false.
	if domain.PropertyValueString(json.RawMessage(`null`)) == "false" {
		t.Fatal("null rendered as false")
	}
}

func TestDescribeOrganizationJoinsEffectiveConfiguredAndDefaults(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/orgs/22/config" && r.URL.Query().Get("filter") == "OFF":
			seen["effective"] = true
			_, _ = io.WriteString(w, `{"ai_allowed":true,"maximum_devices":500,"background_snapshot_reprocess":"LOW_PRIORITY","brand_new_flag":false,"opaque":null,"hosts":["a"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/orgs/22/config" && r.URL.Query().Get("filter") == "CONFIGURED":
			seen["configured"] = true
			_, _ = io.WriteString(w, `{"background_snapshot_reprocess":"LOW_PRIORITY"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/global-config" && r.URL.Query().Get("filter") == "OFF":
			seen["defaults"] = true
			_, _ = io.WriteString(w, `{"ai_allowed":true,"maximum_devices":500,"background_snapshot_reprocess":"NORMAL","brand_new_flag":false,"opaque":7,"hosts":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/orgs/22/config" && r.URL.Query().Get("filter") == "NONDEFAULT":
			seen["nondefault"] = true
			_, _ = io.WriteString(w, `{"maximum_devices":500,"background_snapshot_reprocess":"LOW_PRIORITY"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/global-config" && r.URL.Query().Get("filter") == "CONFIGURED":
			seen["globalrows"] = true
			_, _ = io.WriteString(w, `{"maximum_devices":500,"background_snapshot_reprocess":"NORMAL"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	catalog, _, err := newTestClient(t, server.URL).Properties.DescribeOrganization(context.Background(), "22")
	if err != nil {
		t.Fatalf("DescribeOrganization() error = %v", err)
	}
	for _, read := range []string{"effective", "configured", "defaults", "nondefault", "globalrows"} {
		if !seen[read] {
			t.Errorf("the %s read was not made", read)
		}
	}
	if catalog.DefaultsErr != nil {
		t.Fatalf("DefaultsErr = %v", catalog.DefaultsErr)
	}
	// Every property the appserver reported appears -- including one no client
	// list has ever named. Positive control on the set: six in, six out.
	if len(catalog.Properties) != 6 {
		t.Fatalf("got %d properties, want 6: %+v", len(catalog.Properties), catalog.Properties)
	}
	by := map[domain.OrgProperty]domain.PropertyDescription{}
	for i, p := range catalog.Properties {
		by[p.Name] = p
		if i > 0 && catalog.Properties[i-1].Name >= p.Name {
			t.Errorf("not sorted at %d: %s >= %s", i, catalog.Properties[i-1].Name, p.Name)
		}
	}
	want := map[domain.OrgProperty]domain.PropertyDescription{
		"AI_ALLOWED":                    {Kind: domain.PropertyKindBoolean, Value: "true", HasDefault: true, Default: "true"},
		"MAXIMUM_DEVICES":               {Kind: domain.PropertyKindInteger, Value: "500", HasDefault: true, Default: "500"},
		"BACKGROUND_SNAPSHOT_REPROCESS": {Kind: domain.PropertyKindString, Value: "LOW_PRIORITY", Configured: true, ConfiguredValue: "LOW_PRIORITY", HasDefault: true, Default: "NORMAL"},
		"BRAND_NEW_FLAG":                {Kind: domain.PropertyKindBoolean, Value: "false", HasDefault: true, Default: "false"},
		// null effective value: the kind is observed from the default.
		"OPAQUE": {Kind: domain.PropertyKindInteger, Value: "", HasDefault: true, Default: "7"},
		"HOSTS":  {Kind: domain.PropertyKindList, Value: `["a"]`, HasDefault: true, Default: `[]`},
	}
	// Where each value comes from: an org row wins, then a global default override, else Forward's built-in default.
	layers := map[domain.OrgProperty]struct {
		layer      string
		global     bool
		nonDefault bool
		compiled   string
	}{
		"AI_ALLOWED":                    {domain.PropertyLayerCompiled, false, false, "true"},
		"MAXIMUM_DEVICES":               {domain.PropertyLayerGlobal, true, true, ""},
		"BACKGROUND_SNAPSHOT_REPROCESS": {domain.PropertyLayerOrg, true, true, ""},
		"HOSTS":                         {domain.PropertyLayerCompiled, false, false, "[]"},
	}
	if catalog.LayersErr != nil {
		t.Fatalf("LayersErr = %v", catalog.LayersErr)
	}
	for name, l := range layers {
		got := by[name]
		if got.Layer != l.layer || got.GlobalOverridden != l.global || got.NonDefault != l.nonDefault || got.CompiledDefault != l.compiled || got.HasCompiledDefault != (l.compiled != "") {
			t.Errorf("%s layer = %+v, want %+v", name, got, l)
		}
	}
	for name, w := range want {
		got, ok := by[name]
		if !ok {
			t.Errorf("%s missing", name)
			continue
		}
		if got.Kind != w.Kind || got.Value != w.Value || got.Configured != w.Configured ||
			got.ConfiguredValue != w.ConfiguredValue || got.HasDefault != w.HasDefault || got.Default != w.Default {
			t.Errorf("%s = %+v, want %+v", name, got, w)
		}
	}
}

func TestDescribeOrganizationSurvivesMissingDefaultsButNotMissingEffective(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/orgs/22/config":
			_, _ = io.WriteString(w, `{"ai_allowed":false}`)
		case r.URL.Path == "/api/global-config":
			http.Error(w, "nope", http.StatusForbidden)
		case r.URL.Path == "/api/orgs/404/config":
			http.Error(w, "gone", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	catalog, _, err := client.Properties.DescribeOrganization(context.Background(), "22")
	if err != nil {
		t.Fatalf("DescribeOrganization() error = %v", err)
	}
	if catalog.DefaultsErr == nil {
		t.Fatal("DefaultsErr = nil, want the 403")
	}
	if len(catalog.Properties) != 1 || catalog.Properties[0].HasDefault {
		t.Fatalf("properties = %+v, want one without a default", catalog.Properties)
	}
	if _, _, err := client.Properties.DescribeOrganization(context.Background(), "404"); err == nil {
		t.Fatal("a failed effective read must fail the description")
	}
	if _, _, err := client.Properties.DescribeOrganization(context.Background(), " "); err == nil {
		t.Fatal("empty org id accepted")
	}
}

func TestUnknownOrgPropertyIsClassified(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/orgs/22/config/retired_flag":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"No enum constant com.forwardnetworks.cv.config.OrgProperty.RETIRED_FLAG"}`)
		case "/api/orgs/22/config/ai_allowed":
			// Positive control for the negative case: a 400 about the VALUE.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"Invalid value for ai_allowed: maybe"}`)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	_, _, err := client.Properties.SetOrganization(context.Background(), "22", "retired_flag", "false")
	if !domain.IsUnknownOrgProperty(err) {
		t.Fatalf("IsUnknownOrgProperty(%v) = false, want true", err)
	}
	_, _, err = client.Properties.SetOrganization(context.Background(), "22", "ai_allowed", "maybe")
	if err == nil || domain.IsUnknownOrgProperty(err) {
		t.Fatalf("a bad VALUE classified as an unknown property: %v", err)
	}
	if !ports.IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("IsStatus(400) = false for %v", err)
	}
}

func TestPropertiesOperationsAreMarked(t *testing.T) {
	t.Parallel()
	ops := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{HTTPClient: privateHTTPClient(), BaseURL: server.URL, Username: "u", Password: "p", Hooks: []Hook{func(_ context.Context, e Event) {
		if e.Type == EventRequest {
			ops[e.Method+" "+e.Path] = e.Operation
		}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, _, _ = client.Properties.Organization(ctx, "1", domain.PropertyFilterOff)
	_, _, _ = client.Properties.Global(ctx, domain.PropertyFilterOff)
	_, _, _ = client.Properties.SetOrganization(ctx, "1", "x", "true")
	_, _ = client.Properties.ClearOrganization(ctx, "1", "x")
	_, _, _ = client.Properties.SetGlobal(ctx, "x", "true")
	_, _ = client.Properties.ClearGlobal(ctx, "x")
	want := map[string]string{
		"GET /api/orgs/1/config":      "Properties.Organization",
		"GET /api/global-config":      "Properties.Global",
		"PUT /api/orgs/1/config/x":    "Properties.SetOrganization",
		"DELETE /api/orgs/1/config/x": "Properties.ClearOrganization",
		"PUT /api/global-config/x":    "Properties.SetGlobal",
		"DELETE /api/global-config/x": "Properties.ClearGlobal",
	}
	for k, w := range want {
		if ops[k] != w {
			t.Errorf("%s operation = %q, want %q", k, ops[k], w)
		}
	}
}
