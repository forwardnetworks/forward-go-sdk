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

// The credential endpoint is the only way to rotate a secret on an existing
// account, so every provider's shape must be expressible -- GCP's
// service-account key and IBM's api key were missing, which left a GCP account
// holding whatever key it was created with forever.
func TestCloudAccountCredentialRequestWireForm(t *testing.T) {
	cases := map[string]struct {
		req    CloudAccountCredentialRequest
		want   map[string]any
		absent []string
	}{
		"aws": {
			req:    CloudAccountCredentialRequest{Type: "AWS", Username: "AKIA", Password: "s"},
			want:   map[string]any{"type": "AWS", "username": "AKIA", "password": "s"},
			absent: []string{"clientId", "tenant", "clientEmail", "privateKey", "apiKey"},
		},
		"azure": {
			req:    CloudAccountCredentialRequest{Type: "AZURE", ClientID: "c", Tenant: "d", Password: "s"},
			want:   map[string]any{"type": "AZURE", "clientId": "c", "tenant": "d", "password": "s"},
			absent: []string{"username", "clientEmail", "privateKey", "apiKey"},
		},
		"gcp": {
			req: CloudAccountCredentialRequest{Type: "GCP", ClientID: "1", ClientEmail: "sa@p.iam.gserviceaccount.com", PrivateKeyID: "k", PrivateKey: "PEM"},
			want: map[string]any{"type": "GCP", "clientId": "1", "clientEmail": "sa@p.iam.gserviceaccount.com",
				"privateKeyId": "k", "privateKey": "PEM"},
			absent: []string{"username", "password", "tenant", "apiKey"},
		},
		"ibm": {
			req:    CloudAccountCredentialRequest{Type: "IBM_CLOUD", APIKey: "key"},
			want:   map[string]any{"type": "IBM_CLOUD", "apiKey": "key"},
			absent: []string{"username", "password", "clientId", "privateKey"},
		},
	}
	for name, tc := range cases {
		b, err := json.Marshal(tc.req)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var got map[string]any
		_ = json.Unmarshal(b, &got)
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", name, k, got[k], v)
			}
		}
		for _, k := range tc.absent {
			if _, present := got[k]; present {
				t.Errorf("%s: %s must be omitted", name, k)
			}
		}
	}
}

// Forward stores each region's (and each Azure subscription's) last test on
// the account; the error is what says whether the credential works.
func TestCloudAccountDecodesTestResults(t *testing.T) {
	var accounts []CloudAccount
	body := `[{"type":"GCP","name":"g","collect":true,"regions":{"us-central1":{"testInstant":1758600000000,"error":"PROJECT_VIEW_PERMISSION_MISSING"},"us-east1":null}},
	          {"type":"AZURE","name":"a","collect":true,"testResults":{"sub-1":{"testInstant":1758600000001,"error":"NONE"}}}]`
	if err := json.Unmarshal([]byte(body), &accounts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r := accounts[0].Regions["us-central1"]; r.Error != "PROJECT_VIEW_PERMISSION_MISSING" || r.TestInstant != 1758600000000 {
		t.Fatalf("gcp region result = %+v", r)
	}
	if r, ok := accounts[0].Regions["us-east1"]; !ok || r.Error != "" || r.TestInstant != 0 {
		t.Fatalf("an untested region decodes to the zero result, got %+v (present=%v)", r, ok)
	}
	if r := accounts[1].TestResults["sub-1"]; r.Error != "NONE" || r.TestInstant != 1758600000001 {
		t.Fatalf("azure subscription result = %+v", r)
	}
}
