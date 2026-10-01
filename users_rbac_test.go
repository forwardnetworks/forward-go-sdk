package forward

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET /api/users/current is a UserSession: the user plus the SESSION roles
// (groups merged), group IDs, impersonator and password time. Current reads
// only the user; CurrentSession is how a token client learns its roles.
func TestUsersCurrentSession(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"user":{"id":"12","username":"mary@example.com","email":"mary@example.com","enabled":true,"lastActive":"2026-10-01T10:00:00Z",
		  "authSource":"SAML","externalGroups":["netops"]},
		  "roles":{"org":["ADMIN"],"network":{"3347":"OPERATOR"},"supportedOrgIds":["7"]},"groupIds":["G1"],"impersonator":"support@fwd","passwordSetAt":"2026-09-01T00:00:00Z"}`)
	}))
	defer server.Close()

	got, _, err := newTestClient(t, server.URL).Users.CurrentSession(context.Background())
	if err != nil || got.User.ID != "12" || !got.User.Enabled || got.User.AuthSource != "SAML" || got.User.ExternalGroups[0] != "netops" {
		t.Fatalf("CurrentSession() = %+v, %v", got, err)
	}
	if !got.Roles.HasOrgAdmin() || got.Roles.Network["3347"] != "OPERATOR" || got.GroupIDs[0] != "G1" || got.Impersonator != "support@fwd" || got.PasswordSetAt == "" {
		t.Fatalf("session = %+v", got)
	}
}

func TestUsersReadViews(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/api/users?view=roles":
			_, _ = io.WriteString(w, `[{"id":"12","username":"mary","email":"mary@example.com","enabled":true,"orgAdmin":false,
			  "accessibleNetworks":[{"id":"3347","name":"ATOS","role":"READ_ONLY"}],"groupIds":["G1"],"apiTokenLastUsedAt":"2026-10-01T09:00:00Z"}]`)
		case "/api/users?view=2fa":
			_, _ = io.WriteString(w, `{"12":{"setUp":true,"anyTrustedDevices":false}}`)
		case "/api/users/404":
			w.WriteHeader(http.StatusNotFound)
		case "/api/users/12/roles?type=directlyAssigned":
			_, _ = io.WriteString(w, `{"org":[],"network":{"3347":"READ_ONLY"}}`)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	list, _, err := client.Users.ListWithRoles(ctx)
	if err != nil || len(list) != 1 || list[0].Username != "mary" || list[0].OrgAdmin || list[0].AccessibleNetworks[0].Role != "READ_ONLY" || list[0].APITokenLastUsedAt == "" {
		t.Fatalf("ListWithRoles() = %+v, %v", list, err)
	}
	twoFA, _, err := client.Users.TwoFactorStatuses(ctx)
	if err != nil || !twoFA["12"].SetUp || twoFA["12"].AnyTrustedDevices {
		t.Fatalf("TwoFactorStatuses() = %+v, %v", twoFA, err)
	}
	if u, _, err := client.Users.Get(ctx, "404"); u != nil || err != nil {
		t.Fatalf("absent Get() = %+v, %v", u, err)
	}
	direct, _, err := client.Users.RolesDirect(ctx, "12")
	if err != nil || len(direct.NetworkRoles("3347")) != 1 {
		t.Fatalf("RolesDirect() = %+v, %v", direct, err)
	}
}

// The writes send what Forward's handlers bind, and nothing else; GrantOrgAdmin
// tolerates "already admin" so it can undo RevokeOrgAdmin.
func TestUsersWrites(t *testing.T) {
	t.Parallel()

	type call struct{ method, uri, body string }
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.RequestURI(), string(b)})
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/users/12/roles/org/ADMIN":
			w.WriteHeader(http.StatusConflict)
		case r.Method == http.MethodPatch && r.URL.Path == "/api/users/12":
			_, _ = io.WriteString(w, `{"id":"12","username":"mary","email":"new@example.com","enabled":false}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()
	email, no := "new@example.com", false

	u, _, err := client.Users.Patch(ctx, "12", UserPatch{Email: &email, Enabled: &no})
	if err != nil || u.Email != "new@example.com" || calls[0].body != `{"email":"new@example.com","enabled":false}` {
		t.Fatalf("Patch() = %+v, %v; sent %+v", u, err, calls[0])
	}
	if _, err := client.Users.BulkSetEnabled(ctx, []string{"12", " ", "13"}, false); err != nil || calls[1].uri != "/api/users" || calls[1].method != http.MethodPatch {
		t.Fatalf("BulkSetEnabled: %+v, %v", calls[1], err)
	}
	var bulk map[string]any
	_ = json.Unmarshal([]byte(calls[1].body), &bulk)
	if bulk["enabled"] != false || len(bulk["userIds"].([]any)) != 2 {
		t.Fatalf("BulkSetEnabled body = %s", calls[1].body)
	}
	if _, err := client.Users.Reset2FA(ctx, "12"); err != nil || calls[2] != (call{http.MethodDelete, "/api/users/12/2fa", ""}) {
		t.Fatalf("Reset2FA: %+v, %v", calls[2], err)
	}
	if _, err := client.Users.RevokeOrgAdmin(ctx, "12"); err != nil || calls[3] != (call{http.MethodDelete, "/api/users/12/roles/org/ADMIN", ""}) {
		t.Fatalf("RevokeOrgAdmin: %+v, %v", calls[3], err)
	}
	if _, err := client.Users.GrantOrgAdmin(ctx, "12"); err != nil {
		t.Fatalf("GrantOrgAdmin must treat 409 (already admin) as success: %v", err)
	}
	if _, err := client.Users.BulkSetEnabled(ctx, nil, true); err == nil {
		t.Fatal("an empty user list must be refused")
	}
}

func TestAccessControlNamesRolesAndLabels(t *testing.T) {
	t.Parallel()

	var method, uri, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, uri, body = r.Method, r.URL.RequestURI(), string(b)
		switch r.URL.RequestURI() {
		case "/api/access-control-groups?view=names":
			_, _ = io.WriteString(w, `{"G1":"netops","G2":"sec"}`)
		case "/api/device-access-labels?view=names":
			_, _ = io.WriteString(w, `{"L1":"dc-east"}`)
		case "/api/access-control-groups/G1/network-roles/9001?role=OPERATOR":
			_, _ = io.WriteString(w, `{"id":"G1","name":"netops","networkRoles":{"9001":"OPERATOR"},"orgAdmin":false,"createdBy":"mary"}`)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := context.Background()

	if names, _, err := client.AccessControl.GroupNames(ctx); err != nil || names["G2"] != "sec" {
		t.Fatalf("GroupNames() = %v, %v", names, err)
	}
	if names, _, err := client.AccessControl.DeviceAccessLabelNames(ctx); err != nil || names["L1"] != "dc-east" {
		t.Fatalf("DeviceAccessLabelNames() = %v, %v", names, err)
	}
	group, _, err := client.AccessControl.SetGroupNetworkRole(ctx, "G1", "9001", NetworkRoleOperator)
	if err != nil || method != http.MethodPost || group.NetworkRoles["9001"] != "OPERATOR" || group.OrgAdmin == nil || *group.OrgAdmin || group.CreatedBy != "mary" {
		t.Fatalf("SetGroupNetworkRole() = %+v, %v", group, err)
	}
	if _, _, err := client.AccessControl.SetGroupNetworkRole(ctx, "G1", "9001", NetworkRole("GOD")); err == nil {
		t.Fatal("an invalid role must be refused locally")
	}
	if _, err := client.AccessControl.RemoveLabels(ctx, []string{"G1"}, []string{"L1"}); err != nil || uri != "/api/access-control-groups?action=removeLabels" || body != `{"groupIds":["G1"],"labelIds":["L1"]}` {
		t.Fatalf("RemoveLabels: %s %s %s %v", method, uri, body, err)
	}
	if _, err := client.AccessControl.AddLabels(ctx, []string{"G1"}, nil); err == nil {
		t.Fatal("AddLabels without labels must be refused")
	}
}

// Forward's three operation denials (AccessEnforcer, UserPrincipal.verifyCan)
// name the operation; a TOKENS operation is a permission problem, never an
// authentication failure, and an unlicensed operation is told apart.
func TestMissingPermission(t *testing.T) {
	t.Parallel()

	cases := []struct {
		message, op string
		denied      bool
		unlicensed  bool
	}{
		{message: "Missing permission: OrgOperation.MANAGE_USER_ACCOUNTS", op: "OrgOperation.MANAGE_USER_ACCOUNTS", denied: true},
		{message: "Missing permission: NetworkOperation.EDIT_CHECKS", op: "NetworkOperation.EDIT_CHECKS", denied: true},
		{message: "Missing permission: OrgOperation.MANAGE_API_TOKENS", op: "OrgOperation.MANAGE_API_TOKENS", denied: true},
		{message: "No permission for network operation EDIT_CHECKS", op: "EDIT_CHECKS", denied: true},
		{message: "Unlicensed operation: NetworkOperation.USE_PREDICT", op: "NetworkOperation.USE_PREDICT", unlicensed: true},
		{message: "Permission to update Forward Admin account denied."},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"`+tc.message+`"}`)
		}))
		_, _, err := newTestClient(t, server.URL).Users.ListWithRoles(context.Background())
		server.Close()
		op, denied := MissingPermission(err)
		uop, unlicensed := UnlicensedOperation(err)
		if denied != tc.denied || unlicensed != tc.unlicensed || (tc.denied && op != tc.op) || (tc.unlicensed && uop != tc.op) {
			t.Errorf("%q: MissingPermission=%q,%v UnlicensedOperation=%q,%v", tc.message, op, denied, uop, unlicensed)
		}
		if (tc.denied || tc.unlicensed) && errors.Is(err, ErrAuthentication) {
			t.Errorf("%q also reads as an authentication failure", tc.message)
		}
		if !IsStatus(err, http.StatusForbidden) {
			t.Errorf("%q: IsStatus(403) = false", tc.message)
		}
	}
}

// Users.Delete is the org admin's own-credential delete: 204 deletes, 404 is
// already gone, and Forward's refusals (yourself, a Forward admin) surface
// with their status and message rather than being swallowed.
func TestUsersDelete(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		body   string
		ok     bool
	}{
		{status: http.StatusNoContent, ok: true},
		{status: http.StatusNotFound, ok: true},
		{status: http.StatusBadRequest, body: `{"message":"Can't delete yourself."}`},
		{status: http.StatusForbidden, body: `{"message":"User does not have permission to delete a Forward Admin."}`},
	} {
		var method, path string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.EscapedPath()
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, err := newTestClient(t, server.URL).Users.Delete(context.Background(), " 12 ")
		server.Close()
		if method != http.MethodDelete || path != "/api/users/12" {
			t.Errorf("%d: sent %s %s", tc.status, method, path)
		}
		if tc.ok != (err == nil) {
			t.Errorf("%d: err = %v", tc.status, err)
		}
		var apiErr *ErrorResponse
		if !tc.ok && (!errors.As(err, &apiErr) || !IsStatus(err, tc.status) || apiErr.Message == "") {
			t.Errorf("%d: refusal must keep its status and message: %v", tc.status, err)
		}
	}
	if _, err := newTestClient(t, "http://127.0.0.1:1").Users.Delete(context.Background(), " "); err == nil {
		t.Fatal("an empty user ID must be refused")
	}
}
