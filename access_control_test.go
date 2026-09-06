package forward

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func acClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// THE ONE THAT MATTERS. An org-admin group is `networkRoles: null` on the
// wire; a network group with no roles is `{}`. Skyforge's first client
// coerced null to {} on decode and every org-admin grant disappeared from
// view. Both directions are pinned here: what is sent, and what is read.
func TestAccessControlGroupNilNetworkRolesIsOrgAdminOnTheWireAndOnRead(t *testing.T) {
	var sent []map[string]json.RawMessage
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]json.RawMessage
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			sent = append(sent, body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b) // echo
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"g1","name":"admins","externalGroupNames":["ops"]},{"id":"g2","name":"viewers","externalGroupNames":[],"networkRoles":{},"deviceAccessLabelIds":["l1"]}]`)
	})
	ctx := context.Background()

	admin, _, err := c.AccessControl.CreateGroup(ctx, OrgAdminGroup("admins", []string{"ops"}))
	if err != nil {
		t.Fatalf("create org admin: %v", err)
	}
	if string(sent[0]["networkRoles"]) != "null" {
		t.Fatalf("org-admin request sent networkRoles=%s, want null", sent[0]["networkRoles"])
	}
	if !admin.IsOrgAdmin() {
		t.Fatal("echoed org-admin group did not read back as org admin")
	}

	viewers, _, err := c.AccessControl.CreateGroup(ctx, NetworkGroup("viewers", nil, nil, []string{"l1"}))
	if err != nil {
		t.Fatalf("create network group: %v", err)
	}
	if string(sent[1]["networkRoles"]) != "{}" {
		t.Fatalf("network group sent networkRoles=%s, want {}", sent[1]["networkRoles"])
	}
	if viewers.IsOrgAdmin() {
		t.Fatal("a network group with empty roles read back as org admin -- nil was coerced")
	}

	groups, _, err := c.AccessControl.ListGroups(ctx)
	if err != nil || len(groups) != 2 {
		t.Fatalf("list: %v %+v", err, groups)
	}
	if !groups[0].IsOrgAdmin() || groups[1].IsOrgAdmin() {
		t.Fatalf("list lost the nil/empty distinction: %+v", groups)
	}
}

// Forward forbids narrowing org-wide admin to a device subset; the SDK
// refuses before sending, naming the rule.
func TestAccessControlGroupOrgAdminWithLabelsIsRefusedLocally(t *testing.T) {
	calls := 0
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	req := OrgAdminGroup("admins", nil)
	req.DeviceAccessLabelIDs = []string{"l1"}
	if _, _, err := c.AccessControl.CreateGroup(context.Background(), req); err == nil || !strings.Contains(err.Error(), "cannot be narrowed") {
		t.Fatalf("err = %v, want the org-admin/label refusal", err)
	}
	if calls != 0 {
		t.Fatalf("a refused request reached the server (%d calls)", calls)
	}
}

// Deletes are idempotent: 404 is success, because the desired state holds.
func TestAccessControlDeletesTreat404AsSuccess(t *testing.T) {
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if _, err := c.AccessControl.DeleteGroup(context.Background(), "gone"); err != nil {
		t.Fatalf("DeleteGroup 404: %v", err)
	}
	if _, err := c.AccessControl.DeleteDeviceAccessLabel(context.Background(), "gone"); err != nil {
		t.Fatalf("DeleteDeviceAccessLabel 404: %v", err)
	}
	// Positive control: a 500 is still an error.
	c500 := acClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	if _, err := c500.AccessControl.DeleteGroup(context.Background(), "g"); err == nil {
		t.Fatal("a 500 on delete was accepted")
	}
}

// The label body is exactly {name, deviceNames, deviceGlobs}, always present.
func TestDeviceAccessLabelRequestShape(t *testing.T) {
	var body map[string]json.RawMessage
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"l1","name":"edge","deviceGlobs":["edge-*"]}`)
	})
	got, _, err := c.AccessControl.CreateDeviceAccessLabel(context.Background(), DeviceAccessLabelRequest{Name: "edge", DeviceGlobs: []string{"edge-*"}})
	if err != nil || got.ID != "l1" {
		t.Fatalf("create: %v %+v", err, got)
	}
	for _, k := range []string{"name", "deviceNames", "deviceGlobs"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("request body lacks %q: %v", k, body)
		}
	}
	if string(body["deviceNames"]) != "[]" {
		t.Fatalf("absent deviceNames must be sent as [], got %s", body["deviceNames"])
	}
	if _, _, err := c.AccessControl.CreateDeviceAccessLabel(context.Background(), DeviceAccessLabelRequest{Name: "empty"}); err == nil {
		t.Fatal("a label selecting no devices was accepted")
	}
}

// SAML: an empty body and a JSON null both mean "not configured", not error;
// a configured body decodes; PutSettings refuses an enabled config missing
// its IdP fields before sending.
func TestSAMLSettingsEmptyAndNullMeanNotConfigured(t *testing.T) {
	for _, body := range []string{"", "null"} {
		c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		})
		got, _, err := c.SAML.GetSettings(context.Background())
		if err != nil || got != nil {
			t.Fatalf("body %q: got=%+v err=%v, want nil,nil", body, got, err)
		}
	}
	c := acClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"customName":"sf-alice","samlAuthSettings":{"enabled":true,"entityId":"https://idp","ssoRedirectUrl":"https://idp/sso","verificationCert":"PEM"}}`)
	})
	got, _, err := c.SAML.GetSettings(context.Background())
	if err != nil || got == nil || got.CustomName != "sf-alice" || !got.SAMLAuthSettings.Enabled {
		t.Fatalf("configured: got=%+v err=%v", got, err)
	}
	calls := 0
	c2 := acClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	_, err = c2.SAML.PutSettings(context.Background(), SAMLSettings{CustomName: "x", SAMLAuthSettings: &SAMLAuthSettings{Enabled: true}})
	if err == nil || calls != 0 {
		t.Fatalf("an enabled config without IdP fields was sent (err=%v calls=%d)", err, calls)
	}
}

func TestUsersRolesAndAdminUserExternalGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/roles"):
			_, _ = io.WriteString(w, `{"org":["admin"],"network":{"n1":["VIEWER"]}}`)
		default:
			_, _ = io.WriteString(w, `{"id":"7","orgId":"1","username":"alice","email":"a@x","authSource":"saml","externalGroups":["ops","admins"]}`)
		}
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	roles, _, err := c.Users.Roles(context.Background(), "7")
	if err != nil || !roles.HasOrgAdmin() || roles.Network["n1"][0] != "VIEWER" {
		t.Fatalf("roles: %+v %v", roles, err)
	}
	// Admin routes require a SERVICE principal; the SDK refuses a user-mode
	// client before sending, which is its own contract and not this test's.
	svc, err := NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p", AuthMode: AuthModeService})
	if err != nil {
		t.Fatalf("service client: %v", err)
	}
	u, _, err := svc.Admin.LookupUser(context.Background(), "alice")
	if err != nil || u.AuthSource != "saml" || len(u.ExternalGroups) != 2 {
		t.Fatalf("admin user lost authSource/externalGroups: %+v %v", u, err)
	}
}
