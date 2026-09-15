package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCloudPredictChangeLoop drives the whole cloud change loop against a fake
// appserver and asserts the wire contract of every cloud-object route.
func TestCloudPredictChangeLoop(t *testing.T) {
	t.Parallel()
	const base = "/api/networks/n-1/change-sets/cs-1"
	var calls []string
	var edits []CloudObjectEdit
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/n-1/change-sets":
			_, _ = io.WriteString(w, `{"id":"cs-1","name":"demo","networkId":"n-1","snapshotId":"s-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == base+"/devices/aws-lab/cloud-objects":
			if r.URL.Query().Get("action") != "importTerraformPlan" {
				t.Errorf("import action = %q", r.URL.Query().Get("action"))
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"resource_changes"`) {
				t.Errorf("import body is not the plan JSON: %s", body)
			}
			_, _ = io.WriteString(w, `{"applied":[{"objectId":"rtb-1","added":1,"modified":0,"removed":0}],`+
				`"unsupported":["aws_security_group_rule.open"]}`)
		case r.Method == http.MethodGet && r.URL.Path == base+"/devices/aws-lab/cloud-objects/rtb-1/route-table-diff":
			_, _ = io.WriteString(w, `{"entries":[`+
				`{"diffType":"UNCHANGED","a":{"destination":"10.0.0.0/16","target":"local"},"b":{"destination":"10.0.0.0/16","target":"local"}},`+
				`{"diffType":"ADDED","b":{"destination":"10.51.1.0/24","target":"igw-1","status":"active","origin":"CreateRoute"}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == base+"/devices/aws-lab/cloud-objects/sg-1/security-group-diff":
			_, _ = io.WriteString(w, `{"entries":[{"diffType":"ADDED","b":{"protocol":"tcp","portRange":"8443","source":"10.140.0.0/16","description":"demo","action":"allow"}}]}`)
		case r.Method == http.MethodPost && r.URL.Path == base+"/devices/aws-lab/cloud-objects/sg-1/edits":
			var edit CloudObjectEdit
			if err := json.NewDecoder(r.Body).Decode(&edit); err != nil || edit.Type != "SECURITY_GROUP" || edit.Rule == nil {
				t.Errorf("security-group edit body: %+v (%v)", edit, err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == base+"/devices/aws-lab/cloud-objects/rtb-1/edits":
			var edit CloudObjectEdit
			if err := json.NewDecoder(r.Body).Decode(&edit); err != nil || edit.Type != "ROUTE_TABLE" {
				t.Errorf("edit body = %+v, %v", edit, err)
			}
			edits = append(edits, edit)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == base+"/commits":
			if r.URL.Query().Get("note") != "demo" {
				t.Errorf("commit note = %q", r.URL.Query().Get("note"))
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == base:
			if r.URL.Query().Get("action") != "predict" {
				t.Errorf("run action = %q", r.URL.Query().Get("action"))
			}
			_, _ = io.WriteString(w, `{"id":"s-2","state":"PROCESSING"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()
	changeSet, _, err := client.Predict.CreateChangeSet(ctx, "n-1", ChangeSetCreateRequest{Name: "demo", SnapshotID: "s-1"})
	if err != nil {
		t.Fatalf("CreateChangeSet: %v", err)
	}
	plan := []byte(`{"format_version":"1.2","resource_changes":[{"address":"aws_route.db","type":"aws_route","change":{"actions":["create"]}}]}`)
	imported, _, err := client.Predict.ImportTerraformPlan(ctx, "n-1", string(changeSet.ID), "aws-lab", plan)
	if err != nil {
		t.Fatalf("ImportTerraformPlan: %v", err)
	}
	if len(imported.Applied) != 1 || imported.Applied[0].ObjectID != "rtb-1" || imported.Applied[0].Added != 1 {
		t.Errorf("Applied = %+v", imported.Applied)
	}
	if len(imported.Unsupported) != 1 || imported.Unsupported[0] != "aws_security_group_rule.open" {
		t.Errorf("Unsupported = %v", imported.Unsupported)
	}

	diff, _, err := client.Predict.RouteTableDiff(ctx, "n-1", "cs-1", "aws-lab", "rtb-1")
	if err != nil {
		t.Fatalf("RouteTableDiff: %v", err)
	}
	if len(diff.Entries) != 2 || diff.Entries[1].DiffType != "ADDED" || diff.Entries[1].A != nil ||
		diff.Entries[1].B == nil || diff.Entries[1].B.Target != "igw-1" {
		t.Errorf("diff entries = %+v", diff.Entries)
	}

	route := CloudRoute{Destination: "10.9.0.0/16", Target: "tgw-1"}
	if _, err := client.Predict.AddRoute(ctx, "n-1", "cs-1", "aws-lab", "rtb-1", route); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}
	if _, err := client.Predict.UpdateRoute(ctx, "n-1", "cs-1", "aws-lab", "rtb-1", "10.9.0.0/16", route); err != nil {
		t.Fatalf("UpdateRoute: %v", err)
	}
	if _, err := client.Predict.RemoveRoute(ctx, "n-1", "cs-1", "aws-lab", "rtb-1", "10.9.0.0/16"); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if _, err := client.Predict.DiscardRoute(ctx, "n-1", "cs-1", "aws-lab", "rtb-1", "10.9.0.0/16"); err != nil {
		t.Fatalf("DiscardRoute: %v", err)
	}
	sgDiff, _, err := client.Predict.SecurityGroupDiff(ctx, "n-1", "cs-1", "aws-lab", "sg-1")
	if err != nil {
		t.Fatalf("SecurityGroupDiff: %v", err)
	}
	if len(sgDiff.Entries) != 1 || sgDiff.Entries[0].B.Key() != "tcp|8443|10.140.0.0/16" {
		t.Errorf("security-group diff = %+v", sgDiff)
	}
	rule := InboundRule{Protocol: "tcp", PortRange: "8443", Source: "10.140.0.0/16", Description: "demo", Action: "allow"}
	if _, err := client.Predict.AddInboundRule(ctx, "n-1", "cs-1", "aws-lab", "sg-1", rule); err != nil {
		t.Fatalf("AddInboundRule: %v", err)
	}
	if _, err := client.Predict.Commit(ctx, "n-1", "cs-1", "demo"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	predicted, _, err := client.Predict.Run(ctx, "n-1", "cs-1", "demo")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if predicted.ID != "s-2" {
		t.Errorf("predicted snapshot = %+v", predicted)
	}

	want := []string{
		"POST /api/networks/n-1/change-sets",
		"POST " + base + "/devices/aws-lab/cloud-objects?action=importTerraformPlan",
		"GET " + base + "/devices/aws-lab/cloud-objects/rtb-1/route-table-diff",
		"POST " + base + "/devices/aws-lab/cloud-objects/rtb-1/edits",
		"POST " + base + "/devices/aws-lab/cloud-objects/rtb-1/edits",
		"POST " + base + "/devices/aws-lab/cloud-objects/rtb-1/edits",
		"POST " + base + "/devices/aws-lab/cloud-objects/rtb-1/edits",
		"GET " + base + "/devices/aws-lab/cloud-objects/sg-1/security-group-diff",
		"POST " + base + "/devices/aws-lab/cloud-objects/sg-1/edits",
		"POST " + base + "/commits?note=demo",
		"POST " + base + "?action=predict&note=demo",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	wantEdits := []CloudObjectEdit{
		{Type: "ROUTE_TABLE", Op: CloudObjectEditAdd, Route: &route},
		{Type: "ROUTE_TABLE", Op: CloudObjectEditModify, RowKey: "10.9.0.0/16", Route: &route},
		{Type: "ROUTE_TABLE", Op: CloudObjectEditRemove, RowKey: "10.9.0.0/16"},
		{Type: "ROUTE_TABLE", Op: CloudObjectEditDiscard, RowKey: "10.9.0.0/16"},
	}
	if len(edits) != len(wantEdits) {
		t.Fatalf("edits = %+v", edits)
	}
	for i, want := range wantEdits {
		got := edits[i]
		if got.Op != want.Op || got.RowKey != want.RowKey || (got.Route == nil) != (want.Route == nil) ||
			(got.Route != nil && *got.Route != *want.Route) {
			t.Errorf("edit[%d] = %+v, want %+v", i, got, want)
		}
	}
	if got := client.Capabilities.Support(CapabilityCloudPredict).Support; got != CapabilitySupported {
		t.Errorf("cloud-predict capability after success = %q", got)
	}
}

// TestCloudPredictRefusedByProfile proves the build-profile gate short-circuits
// before any request leaves the client.
func TestCloudPredictRefusedByProfile(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached the server: %s %s", r.Method, r.URL)
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL: server.URL, Username: "user", Password: "pass",
		Capabilities: CapabilityProfile{Track: "stable", Build: "26.4", Features: map[Capability]CapabilitySupport{
			CapabilityCloudPredict: CapabilityUnsupported,
		}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, _, err = client.Predict.ImportTerraformPlan(context.Background(), "n-1", "cs-1", "aws-lab", []byte(`{}`))
	var unsupported *UnsupportedCapabilityError
	if !errors.As(err, &unsupported) || unsupported.Capability != CapabilityCloudPredict {
		t.Fatalf("err = %v, want UnsupportedCapabilityError for cloud-predict", err)
	}
	if _, _, err := client.Predict.ImportTerraformPlan(context.Background(), "n-1", "cs-1", "aws-lab", nil); err == nil {
		t.Error("empty plan accepted")
	}
}
