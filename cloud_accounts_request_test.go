package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// fwd 075aa01 ("Serialize cloud test times as ISO-8601") moved testInstant
// from epoch millis to Jackson's Instant string. Builds on both sides of it
// are live (Skyforge's pins predate it; Forward primary 1.0.0-261001 has it),
// so create and list must decode either, into the same epoch millis.
func TestCloudAccountTestInstantISO8601(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"type":"AWS","name":"aws-demo","collect":true,"regions":{"us-east-1":{"testInstant":"2026-10-01T12:34:56.789Z","error":"NONE"},"us-west-2":null}}`)
			return
		}
		_, _ = io.WriteString(w, `[{"type":"GCP","name":"g","collect":true,"regions":{"us-central1":{"testInstant":"1970-01-01T00:00:00Z","error":"PROJECT_VIEW_PERMISSION_MISSING"}}},
		  {"type":"AZURE","name":"a","collect":true,"testResults":{"sub-1":{"testInstant":"2026-10-01T12:34:56.123456789Z","error":"NONE"}}},
		  {"type":"IBM","name":"i","collect":true,"regions":{"us-south":{"testInstant":"1759322096789","error":"NONE"}}}]`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	want := time.Date(2026, 10, 1, 12, 34, 56, 789_000_000, time.UTC)

	created, _, err := client.CloudAccounts.Create(ctx, "N1", CloudAccountRequest{Type: "AWS", Name: "aws-demo", Collect: true, Regions: map[string]int{"us-east-1": 0}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	region := created.Regions["us-east-1"]
	if at, ok := region.TestedAt(); !ok || !at.Equal(want) || region.TestInstant != want.UnixMilli() || region.Error != "NONE" {
		t.Fatalf("aws region = %+v (TestedAt %v %v)", region, at, ok)
	}
	if _, ok := created.Regions["us-west-2"].TestedAt(); ok {
		t.Fatal("an untested (null) region must report no test time")
	}

	accounts, _, err := client.CloudAccounts.List(ctx, "N1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if r := accounts[0].Regions["us-central1"]; r.TestInstant != 0 || r.Error != "PROJECT_VIEW_PERMISSION_MISSING" {
		t.Fatalf("gcp epoch-zero region = %+v", r)
	}
	if r := accounts[1].TestResults["sub-1"]; r.TestInstant != time.Date(2026, 10, 1, 12, 34, 56, 123_000_000, time.UTC).UnixMilli() {
		t.Fatalf("azure nanosecond instant = %+v", r)
	}
	if r := accounts[2].Regions["us-south"]; r.TestInstant != 1759322096789 {
		t.Fatalf("numeric-string millis = %+v", r)
	}

	var bad []CloudAccount
	if err := json.Unmarshal([]byte(`[{"regions":{"x":{"testInstant":"yesterday"}}}]`), &bad); err == nil || !strings.Contains(err.Error(), "yesterday") {
		t.Fatalf("an unreadable testInstant must fail naming the value, got %v", err)
	}
}
