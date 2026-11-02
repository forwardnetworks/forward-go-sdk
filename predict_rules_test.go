package forward

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A rule read back from Forward keeps every field of its rule model on the way out again: ranges, dynamic objects, regions, schedule, URL
// objects, an application's dynamic groups and a user's identity references. A struct that drops one would silently widen the rule when
// the read is sent back.
func TestSecurityRuleKeepsEveryField(t *testing.T) {
	t.Parallel()
	in := `{"name":"r","description":"d","enabled":true,"action":"RESET_CLIENT_AND_SERVER",` +
		`"sourceAddresses":{"subnets":["10.0.0.0/8"],"ipRanges":[{"start":"10.1.0.1","end":"10.1.0.9"}],"dynamicAddressObjects":[{"scopeId":"LOCAL","name":"dyn"}],"regions":[{"scopeId":"LOCAL","name":"US"}],"negated":true},` +
		`"destinationAddresses":{},"sourceUsers":{"identityReferences":["alice"]},` +
		`"applications":{"dynamicGroups":[{"scopeId":"PREDEFINED","name":"g"}]},"services":{},` +
		`"schedule":{"scopeId":"LOCAL","name":"work"},"urlObjects":[{"scopeId":"LOCAL","name":"u"}]}`
	var rule SecurityRuleDefinition
	if err := json.Unmarshal([]byte(in), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Action != SecurityActionResetClientAndServer || len(rule.SourceAddresses.IPRanges) != 1 || rule.SourceAddresses.IPRanges[0].End != "10.1.0.9" ||
		rule.SourceAddresses.DynamicAddressObjects[0].Name != "dyn" || rule.SourceAddresses.Regions[0].Name != "US" || !rule.SourceAddresses.Negated ||
		rule.SourceUsers.IdentityReferences[0] != "alice" || rule.Applications.DynamicGroups[0].Name != "g" ||
		rule.Schedule == nil || rule.Schedule.Name != "work" || rule.URLObjects[0].Name != "u" {
		t.Fatalf("rule = %+v", rule)
	}
	out, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ipRanges":[{"start":"10.1.0.1","end":"10.1.0.9"}]`, `"dynamicAddressObjects"`, `"regions"`, `"identityReferences":["alice"]`, `"dynamicGroups"`, `"schedule":{"scopeId":"LOCAL","name":"work"}`, `"urlObjects"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%s missing from %s", want, out)
		}
	}
}

// The Any helpers stage as every field empty (Forward writes that to the device as "any"); they must stay empty on the wire.
func TestAnyHelpersAreEmpty(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{"address": AnyAddress(), "user": AnyUser(), "application": AnyApplication(), "service": AnyService()} {
		b, _ := json.Marshal(v)
		if string(b) != "{}" {
			t.Errorf("%s any = %s", name, b)
		}
	}
}

// AllowedCommands: a 500 is Forward failing to load the tree and says so, while the underlying error stays reachable; the empty text of
// a vendor with no tree is not an error.
func TestAllowedCommandsFailureAndEmpty(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	broken := newTestClient(t, predictRecorder(t, 500, `{"message":"boom"}`, &seen).URL)
	_, _, err := broken.Predict.AllowedCommands(context.Background(), "arista_eos", PredictProtocolBGP)
	if !errors.Is(err, ErrAllowedCommandsUnavailable) || !IsStatus(err, 500) {
		t.Fatalf("err = %v", err)
	}
	empty := newTestClient(t, predictRecorder(t, 200, ``, &seen).URL)
	if text, _, err := empty.Predict.AllowedCommands(context.Background(), "pan_os", PredictProtocolBGP); err != nil || text != "" {
		t.Fatalf("empty = %q, %v", text, err)
	}
}

func TestSupportsPredictCommands(t *testing.T) {
	t.Parallel()
	if got := PredictCommandOSes(); len(got) != 8 {
		t.Fatalf("command OSes = %v", got)
	} else {
		got[0] = "changed" // a caller's copy must not change what is supported
	}
	if !SupportsPredictCommands("cisco_ios") {
		t.Error("the shared list was changed through a returned copy")
	}
	for _, os := range []string{"cisco_ios", "ARISTA_EOS", " juniper_srx "} {
		if !SupportsPredictCommands(os) {
			t.Errorf("%s not supported", os)
		}
	}
	for _, os := range []string{"pan_os", "f5_bigip", ""} {
		if SupportsPredictCommands(os) {
			t.Errorf("%q supported", os)
		}
	}
}
