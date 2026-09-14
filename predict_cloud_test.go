package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The cloud predict flow is four calls that have to agree on one change set:
// stage, commit, predict, and then read the predicted snapshot. These assert
// the wire contract of each, because a path or verb that is subtly wrong fails
// as a 404 at demo time rather than at compile time.
func TestPredictCloudChangesFlow(t *testing.T) {
	t.Parallel()

	var staged CloudChanges
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost &&
			r.URL.Path == "/api/networks/network-1/change-sets/CHG-1/draft/devices/aws-source/cloud-changes" &&
			r.URL.Query().Get("action") == "":
			if err := json.NewDecoder(r.Body).Decode(&staged); err != nil {
				t.Errorf("decode cloud changes: %v", err)
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost &&
			r.URL.Path == "/api/networks/network-1/change-sets/CHG-1/draft/devices/aws-source/cloud-changes" &&
			r.URL.Query().Get("action") == "fromTerraformPlan":
			body, _ := io.ReadAll(r.Body)
			// The plan is forwarded verbatim; the server does the translating.
			if !json.Valid(body) {
				t.Errorf("terraform plan was not sent as JSON: %s", body)
			}
			_, _ = io.WriteString(w, `{"ignored":[],"unpredictable":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/change-sets/CHG-1/commits":
			if got := r.URL.Query().Get("note"); got != "demo" {
				t.Errorf("commit note = %q", got)
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/networks/network-1/change-sets/CHG-1":
			if got := r.URL.Query().Get("action"); got != "predict" {
				t.Errorf("action = %q", got)
			}
			_, _ = io.WriteString(w, `{"id":"snapshot-9","state":"PROCESSING"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx := context.Background()

	priority := int64(300)
	changes := CloudChanges{
		RouteChanges: []CloudRouteChange{{
			RouteTableID:         "rtb-1",
			DestinationCidrBlock: "10.140.0.0/16",
			Target:               &CloudRouteTarget{Kind: "TRANSIT_GATEWAY", ID: "tgw-1"},
		}},
		SecurityRuleChanges: []CloudSecurityRuleChange{{
			SecurityGroupID: "nsg-1",
			Addition: &CloudRuleAddition{
				Egress: false, Protocol: "TCP", TargetKind: "cidr", TargetID: "10.140.0.0/16",
				Description: "demo", Name: "allow_demo", Priority: &priority, Deny: true,
			},
		}},
		VpnRouteChanges: []CloudVpnRouteChange{{
			VpnConnectionID: "vpn-1", DestinationCidrBlock: "172.16.0.0/16", Present: true,
		}},
	}
	if _, err := client.Predict.StageCloudChanges(ctx, "network-1", "CHG-1", "aws-source", changes); err != nil {
		t.Fatalf("StageCloudChanges() = %v", err)
	}
	if len(staged.RouteChanges) != 1 || staged.RouteChanges[0].Target.ID != "tgw-1" {
		t.Fatalf("route change did not survive the round trip: %#v", staged.RouteChanges)
	}
	// Azure orders its rulebase and lets a rule deny; both have to reach the wire.
	if staged.SecurityRuleChanges[0].Addition.Priority == nil ||
		*staged.SecurityRuleChanges[0].Addition.Priority != 300 ||
		!staged.SecurityRuleChanges[0].Addition.Deny {
		t.Fatalf("priority and deny did not survive: %#v", staged.SecurityRuleChanges[0].Addition)
	}
	if len(staged.VpnRouteChanges) != 1 || !staged.VpnRouteChanges[0].Present {
		t.Fatalf("vpn route change did not survive: %#v", staged.VpnRouteChanges)
	}

	plan := []byte(`{"format_version":"1.2","resource_changes":[]}`)
	if _, err := client.Predict.StageTerraformPlan(ctx, "network-1", "CHG-1", "aws-source", plan); err != nil {
		t.Fatalf("StageTerraformPlan() = %v", err)
	}
	if _, err := client.Predict.Commit(ctx, "network-1", "CHG-1", "demo"); err != nil {
		t.Fatalf("Commit() = %v", err)
	}
	predicted, _, err := client.Predict.Run(ctx, "network-1", "CHG-1", "demo")
	if err != nil || string(predicted.ID) != "snapshot-9" {
		t.Fatalf("Run() = %#v, %v", predicted, err)
	}
}

// An omitted category must not appear on the wire at all: sending an empty list
// is a statement that the category has no changes, and the server reads the two
// differently.
func TestCloudChangesOmitsEmptyCategories(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(CloudChanges{
		RouteChanges: []CloudRouteChange{{RouteTableID: "rtb-1", DestinationCidrBlock: "0.0.0.0/0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, absent := range []string{"securityRuleChanges", "tgwAssociationChanges", "vpnRouteChanges"} {
		if contains(body, absent) {
			t.Fatalf("%s should be omitted when empty: %s", absent, body)
		}
	}
	// A removal is an absent target, so the field has to be omitted rather than null.
	if contains(body, `"target"`) {
		t.Fatalf("an absent target should not be serialised: %s", body)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
