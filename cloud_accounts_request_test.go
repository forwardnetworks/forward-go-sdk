package forward

import (
	"encoding/json"
	"testing"
)

// The request was map[string]any, where a misspelled key was a silent no-op
// against the API. Typing it only helps if the wire form is unchanged, so the
// exact keys are pinned here -- including the two asymmetries that a caller
// would otherwise get wrong.
func TestCloudAccountRequestWireForm(t *testing.T) {
	req := CloudAccountRequest{
		Type:          "AWS",
		Name:          "prod",
		Collect:       true,
		ProxyServerID: "p1",
		// On the REQUEST a region set is a map to zero. The RESPONSE returns
		// an object per region, so the two cannot be fed back into each other.
		Regions:  map[string]int{"us-east-1": 0},
		Username: "AKIA",
		Password: "secret",
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for k, want := range map[string]any{
		"type": "AWS", "name": "prod", "collect": true,
		"proxyServerId": "p1", "username": "AKIA", "password": "secret",
	} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	regions, ok := got["regions"].(map[string]any)
	if !ok || regions["us-east-1"] != float64(0) {
		t.Fatalf("regions wire form wrong: %v", got["regions"])
	}
	// Azure-only fields must be ABSENT for an AWS account, not empty strings:
	// an empty clientId is a different request from no clientId.
	for _, k := range []string{"clientId", "tenant", "environment", "subscriptionIds"} {
		if _, present := got[k]; present {
			t.Errorf("%s must be omitted when unset", k)
		}
	}
	// collect:false must still be SENT -- omitting it would read as "leave
	// collection as it was" rather than "turn it off".
	off, _ := json.Marshal(CloudAccountRequest{Type: "AWS", Name: "x", Collect: false})
	var offMap map[string]any
	_ = json.Unmarshal(off, &offMap)
	if v, present := offMap["collect"]; !present || v != false {
		t.Fatalf("collect=false must be sent explicitly, got %v (present=%v)", v, present)
	}
}

// Extra reaches a property this struct does not name, but must never be able
// to override one it does -- otherwise the typing is decorative.
func TestCloudAccountRequestExtraCannotOverrideNamedFields(t *testing.T) {
	b, err := json.Marshal(CloudAccountRequest{
		Type: "AZURE", Name: "real",
		Extra: map[string]any{"name": "spoofed", "futureField": "kept"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["name"] != "real" {
		t.Fatalf("Extra overrode a named field: name = %v", got["name"])
	}
	if got["futureField"] != "kept" {
		t.Fatalf("Extra did not reach the wire: %v", got["futureField"])
	}
}
