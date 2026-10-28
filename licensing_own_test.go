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

// GET /api/licenses works on every deployment (the org-scoped
// /api/orgs/{id}/licenses is SaaS-only and 404s "No endpoint" on-prem).
func TestLicensingOwnOrg(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/licenses":
			_, _ = io.WriteString(w, `[{"id":"L1","license":{"id":"L1","tier":"ENTERPRISE","expiresAt":"2027-01-01T00:00:00Z"},"status":"ACTIVE"}]`)
		case "/api/licenses?view=status":
			_, _ = io.WriteString(w, `{"licenseTier":"ENTERPRISE","licenseStatus":"ACTIVE"}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	licensing := newTestClient(t, server.URL).Licensing
	ctx := context.Background()

	list, _, err := licensing.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "L1" || list[0].Status != "ACTIVE" || list[0].License == nil || list[0].License.Tier != "ENTERPRISE" {
		t.Fatalf("List() = %+v, %v", list, err)
	}
	tier, _, err := licensing.TierAndStatus(ctx)
	if err != nil || tier.LicenseTier != "ENTERPRISE" || tier.LicenseStatus != "ACTIVE" {
		t.Fatalf("TierAndStatus() = %+v, %v", tier, err)
	}
}

// The server sends no top-level id and no "stackable": the id is inside the licence and a tier
// licence says replaces. A support listing therefore has to be read through those. An activated
// licence carries signedLicenseKey (Forward refuses a second activation on exactly that), and the
// support UI calls a licence "Available" when it is unsigned, not invalidated, not EXPIRED and of
// a type that can still be activated -- including a signed licence from before 23.6 that has no
// usedAt, which a usedAt test would wrongly offer.
func TestLicenseDetailsDecodeTheWireShape(t *testing.T) {
	t.Parallel()

	rows := `[
		{"license":{"id":"12","type":"TIER","tier":"NSP","startsAt":"2026-11-01T00:00:00Z","expiresAt":"2027-11-01T00:00:00Z","replaces":false,"standardDeviceCap":500,"liteDeviceCap":20,"cloudCreditHours":10,"edgeAssetCap":7},"status":"NOT_ACTIVE_YET"},
		{"license":{"id":"11","type":"TIER","tier":"NS","replaces":true,"standardDeviceCap":100},"status":"ACTIVE","usedAt":"2026-01-02T00:00:00Z","signedLicenseKey":"LicenseKey__abc","fingerprint":""},
		{"license":{"id":"10","type":"TIER","tier":"NSP","replaces":true,"standardDeviceCap":1},"status":"ACTIVE","signedLicenseKey":"LicenseKey__old"},
		{"license":{"id":"9","type":"TIER","tier":"NSP","replaces":true,"standardDeviceCap":1},"status":"EXPIRED"},
		{"license":{"id":"8","type":"DEVICE","tier":"NS"},"status":"ACTIVE"},
		{"license":{"id":"7","type":"TIER","tier":"NSP","replaces":true,"standardDeviceCap":1},"status":"ACTIVE","invalidated":true}
	]`
	for name, body := range map[string]string{"bare array": rows, "licenses envelope": `{"licenses":` + rows + `}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RequestURI() != "/api/orgs/o1/licenses" {
				t.Errorf("unexpected %s", r.URL.RequestURI())
			}
			_, _ = io.WriteString(w, body)
		}))
		list, _, err := newTestClient(t, server.URL).Licensing.ListForOrg(context.Background(), "o1")
		server.Close()
		if err != nil || len(list) != 6 {
			t.Fatalf("%s: ListForOrg = %+v, %v", name, list, err)
		}
		for _, d := range list {
			if d.License == nil {
				t.Fatalf("%s: row %q has no license", name, d.ID)
			}
		}
		stored := list[0]
		if stored.ID != "12" || stored.License.ID != "12" {
			t.Fatalf("%s: id = %q / %q, want 12 taken from the inner licence", name, stored.ID, stored.License.ID)
		}
		l := stored.License
		if l.Replaces == nil || *l.Replaces || !l.Stackable || l.Tier != "NSP" || l.StandardDeviceCap != 500 || l.LiteDeviceCap != 20 || l.CloudCreditHours != 10 || l.EdgeAssetCap != 7 {
			t.Fatalf("%s: license = %+v", name, l)
		}
		var available []string
		for _, d := range list {
			if d.Available() {
				available = append(available, d.ID)
			}
		}
		// 11 is signed, 10 is signed without usedAt (pre-23.6), 9 is expired, 8 is an unsupported type, 7 is invalidated.
		if len(available) != 1 || available[0] != "12" {
			t.Fatalf("%s: available = %v, want [12]", name, available)
		}
		active := list[1]
		if !active.Signed || active.UsedAt != "2026-01-02T00:00:00Z" || active.License.Stackable || active.License.Replaces == nil || !*active.License.Replaces {
			t.Fatalf("%s: active = %+v %+v", name, active, active.License)
		}
		if !list[2].Signed || list[2].UsedAt != "" || stored.Signed || !list[5].Invalidated {
			t.Fatalf("%s: signed/usedAt/invalidated = %+v %+v %+v", name, list[2], stored, list[5])
		}
		// Non-tier types state no replaces: unstated, not "stacks".
		if list[4].License.Replaces != nil || list[4].License.Stackable {
			t.Fatalf("%s: device licence = %+v", name, list[4].License)
		}
	}

	// A body that does not state replaces leaves it unstated rather than reading as "stacks".
	var bare License
	if err := json.Unmarshal([]byte(`{"id":"1","tier":"NS"}`), &bare); err != nil || bare.Replaces != nil || bare.Stackable {
		t.Fatalf("bare = %+v, %v", bare, err)
	}
	// The signed key is not kept anywhere on the value.
	var withKey LicenseDetails
	if err := json.Unmarshal([]byte(`{"license":{"id":"1"},"signedLicenseKey":"SECRET"}`), &withKey); err != nil {
		t.Fatal(err)
	}
	if encoded, _ := json.Marshal(withKey); strings.Contains(string(encoded), "SECRET") || !withKey.Signed {
		t.Fatalf("withKey = %+v, encoded %s", withKey, encoded)
	}
}

// Activating a stored license has two forms and Forward chooses by the org, not the caller: no body for a SaaS license, a
// {"fingerprint"} body for an on-prem one. Sending a body when none is wanted (or none when it is) is a 400, so the SDK must send
// exactly what it was given. A blank org or license never reaches the wire.
func TestActivateForOrgSendsTheRightForm(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	client := newTestClient(t, predictRecorder(t, 200, `{"license":{"id":"lic/1","type":"PERPETUAL"}}`, &seen).URL)
	ctx := context.Background()

	got, _, err := client.Licensing.ActivateForOrg(ctx, "org 1", "lic/1", "")
	if err != nil || got == nil || got.ID != "lic/1" {
		t.Fatalf("activate = %+v, %v", got, err)
	}
	if _, _, err := client.Licensing.ActivateForOrg(ctx, "org 1", "lic/1", " fp-9 "); err != nil {
		t.Fatal(err)
	}
	want := []predictSeen{
		{"POST", "/api/orgs/org%201/licenses/lic%2F1?action=activate", "", ""},
		{"POST", "/api/orgs/org%201/licenses/lic%2F1?action=activate", `{"fingerprint":"fp-9"}`, "application/json"},
	}
	if len(seen) != 2 {
		t.Fatalf("seen %+v", seen)
	}
	// Content-Type decides which Forward handler runs, so the no-body form must send none at all.
	for i, w := range want {
		if seen[i].method != w.method || seen[i].uri != w.uri || seen[i].body != w.body || seen[i].contentType != w.contentType {
			t.Errorf("request %d = %+v, want %+v", i, seen[i], w)
		}
	}
	if _, _, err := client.Licensing.ActivateForOrg(ctx, " ", "x", ""); !errors.Is(err, ErrLicenseOrgIDRequired) {
		t.Errorf("blank org: %v", err)
	}
	if _, _, err := client.Licensing.ActivateForOrg(ctx, "o", " ", ""); err == nil || len(seen) != 2 {
		t.Errorf("blank license was sent: %v", err)
	}
}
