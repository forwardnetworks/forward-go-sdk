package forward

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The NetBox plugin saw, and forward-skills reproduced against a fake, a bare string where Forward's source at ~/src/fwd writes a list
// ({"org":"ADMIN"}); which build sends it is not known. One such field made
// the whole response fail with "cannot unmarshal string into … []string", so inspect-access could not answer at all. The org (and
// system) roles are read as a list, a bare string (one role, never split into characters) or null/empty, on both the session read and
// the user roles read, and an empty answer decodes to no roles, which callers must treat as "Forward did not say".
func TestRoleFieldsAcceptListStringAndNull(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
		want  []string
	}{
		{"list", `["ADMIN"]`, []string{"ADMIN"}},
		{"two", `["ADMIN","MEMBER"]`, []string{"ADMIN", "MEMBER"}},
		{"bare string", `"ADMIN"`, []string{"ADMIN"}},
		{"padded string", `" ADMIN "`, []string{"ADMIN"}},
		{"padded list element", `[" ADMIN "]`, []string{"ADMIN"}},
		{"empty string", `""`, nil},
		{"null", `null`, nil},
		{"empty list", `[]`, []string{}},
	}
	for _, tc := range cases {
		session := `{"user":{"username":"u"},"roles":{"org":` + tc.field + `,"system":` + tc.field + `,"network":{"7":"ADMIN"}}}`
		var got UserSession
		if err := json.Unmarshal([]byte(session), &got); err != nil {
			t.Fatalf("%s: UserSession: %v", tc.name, err)
		}
		if !sameRoles(got.Roles.Org, tc.want) || !sameRoles(got.Roles.System, tc.want) || got.Roles.Network["7"] != "ADMIN" {
			t.Errorf("%s: session roles %+v, want %v", tc.name, got.Roles, tc.want)
		}
		var roles UserRoles
		if err := json.Unmarshal([]byte(`{"org":`+tc.field+`,"system":`+tc.field+`,"network":{}}`), &roles); err != nil {
			t.Fatalf("%s: UserRoles: %v", tc.name, err)
		}
		if !sameRoles(roles.Org, tc.want) || !sameRoles(roles.System, tc.want) {
			t.Errorf("%s: user roles %+v, want %v", tc.name, roles, tc.want)
		}
	}
}

// sameRoles is exact, so an explicit empty list ([]string{}) and null (nil) stay distinguishable.
func sameRoles(got, want []string) bool { return reflect.DeepEqual(got, want) }

// The exact response forward-skills reproduced against a fake: a bare-string org role and an empty network map.
func TestSessionWithBareStringOrgRole(t *testing.T) {
	t.Parallel()

	var session UserSession
	if err := json.Unmarshal([]byte(`{"roles":{"org":"ADMIN","network":{}}}`), &session); err != nil {
		t.Fatal(err)
	}
	if !session.Roles.HasOrgAdmin() || len(session.Roles.Org) != 1 {
		t.Fatalf("a bare string is one role: %+v", session.Roles)
	}
}

// A response with no roles at all decodes to no roles, and a network's role may arrive as a one-element list; a list of several (or a
// field that is neither string nor list) is an error naming the field rather than a silent pick.
func TestSessionRolesEdges(t *testing.T) {
	t.Parallel()

	var empty UserSession
	if err := json.Unmarshal([]byte(`{"roles":{}}`), &empty); err != nil || empty.Roles.HasOrgAdmin() || len(empty.Roles.Org) != 0 || empty.Roles.Network != nil {
		t.Fatalf("empty roles: %v %+v", err, empty.Roles)
	}
	var list UserSession
	if err := json.Unmarshal([]byte(`{"roles":{"org":[],"network":{"7":["OPERATOR"],"8":null}}}`), &list); err != nil || list.Roles.Network["7"] != "OPERATOR" || list.Roles.Network["8"] != "" {
		t.Fatalf("list-form network role: %v %+v", err, list.Roles)
	}
	for name, body := range map[string]string{
		"two network roles": `{"roles":{"network":{"7":["OPERATOR","ADMIN"]}}}`,
		"number org":        `{"roles":{"org":5}}`,
		"object org":        `{"roles":{"org":{"a":1}}}`,
	} {
		var session UserSession
		err := json.Unmarshal([]byte(body), &session)
		if err == nil {
			t.Errorf("%s must fail, got %+v", name, session.Roles)
		} else if !strings.Contains(err.Error(), "forward:") {
			t.Errorf("%s: error should name the field: %v", name, err)
		}
	}
}

// The browser session read (GET /api/users/current through the cookie flow) has the same roles object and must not fail on a bare string.
func TestBrowserSessionRolesAcceptABareStringOrgRole(t *testing.T) {
	t.Parallel()

	var session BrowserSession
	if err := json.Unmarshal([]byte(`{"user":{"username":"u"},"roles":{"org":"ADMIN","network":{"7":"ADMIN"}}}`), &session); err != nil {
		t.Fatal(err)
	}
	if len(session.Roles.Org) != 1 || session.Roles.Org[0] != "ADMIN" || session.Roles.Network["7"] != "ADMIN" {
		t.Fatalf("%+v", session.Roles)
	}
}
