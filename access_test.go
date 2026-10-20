package forward

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests use an INVENTED mini checkout in the layout the parser reads (operation tables, a role ladder using both inheritance forms
// Forward's source uses): this repo is public and carries no Forward data. A test against a real checkout is skipped without one.
func miniCheckout(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, accessSourceDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("NetworkOperation.java", `package x;
public enum NetworkOperation implements LicensableOperation {
    // ----- widgets ------
    /// Look at widgets
    VIEW_WIDGETS(AccessType.READ, LicenseFacet.BASE),

    /// Tweak widgets
    TWEAK_WIDGETS(AccessType.WRITE, LicenseFacet.BASE),

    LAUNCH_WIDGETS(AccessType.WRITE, LicenseFacet.NETWORK),

    SHARED_NAME(AccessType.READ, LicenseFacet.BASE);
}
`)
	write("OrgOperation.java", `package x;
public enum OrgOperation implements LicensableOperation {
    /// See the gadget list
    VIEW_GADGETS(AccessType.READ, LicenseFacet.NO_REQUIREMENT),

    MANAGE_GADGETS(AccessType.WRITE, LicenseFacet.NO_REQUIREMENT),

    SHARED_NAME(AccessType.WRITE, LicenseFacet.BASE),

    ADMIN_ONLY_GADGETS(AccessType.WRITE, LicenseFacet.BASE);
}
`)
	write("NetworkRole.java", `package x;
public enum NetworkRole implements HasId<NetworkRole> {
    LIMITED_READ_ONLY(
        6,
        Sets.immutableEnumSet(OrgOperation.VIEW_GADGETS),
        Sets.immutableEnumSet(
            NetworkOperation.VIEW_WIDGETS
            // NetworkOperation.LAUNCH_WIDGETS is mentioned in a comment only
        )
    ),
    READ_ONLY(
        3,
        Sets.immutableEnumSet(Iterables.concat(LIMITED_READ_ONLY.allowedOrgOperations, EnumSet.of(OrgOperation.SHARED_NAME))),
        Sets.immutableEnumSet(Iterables.concat(LIMITED_READ_ONLY.allowedOperations, EnumSet.of(NetworkOperation.SHARED_NAME)))
    ),
    OPERATOR(
        5,
        Sets.immutableEnumSet(READ_ONLY.allowedOrgOperations),
        Sets.immutableEnumSet(Iterables.concat(READ_ONLY.allowedOperations, EnumSet.of(NetworkOperation.TWEAK_WIDGETS)))
    ),
    WORKSPACE_OPERATOR(
        7,
        Sets.immutableEnumSet(Iterables.concat(OPERATOR.allowedOrgOperations, EnumSet.of(OrgOperation.MANAGE_GADGETS))),
        Sets.immutableEnumSet(Iterables.concat(OPERATOR.allowedOperations, EnumSet.of(NetworkOperation.LAUNCH_WIDGETS)))
    ),
    ADMIN(
        1,
        Sets.immutableEnumSet(WORKSPACE_OPERATOR.allowedOrgOperations),
        Sets.immutableEnumSet(WORKSPACE_OPERATOR.allowedOperations)
    );

    public static final ImmutableSet<NetworkRole> VALUES = Sets.immutableEnumSet(Arrays.asList(values()));
}
`)
	return root
}

func TestAccessTableParsesOperationsAndInheritance(t *testing.T) {
	t.Parallel()

	table, err := LoadAccessTable(miniCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		write  bool
		facet  string
		lowest string
	}{
		"NetworkOperation.VIEW_WIDGETS":   {false, "BASE", "LIMITED_READ_ONLY"},
		"NetworkOperation.SHARED_NAME":    {false, "BASE", "READ_ONLY"},
		"NetworkOperation.TWEAK_WIDGETS":  {true, "BASE", "OPERATOR"},
		"NetworkOperation.LAUNCH_WIDGETS": {true, "NETWORK", "WORKSPACE_OPERATOR"},
		"OrgOperation.VIEW_GADGETS":       {false, "NO_REQUIREMENT", "LIMITED_READ_ONLY"},
		"OrgOperation.MANAGE_GADGETS":     {true, "NO_REQUIREMENT", "WORKSPACE_OPERATOR"},
		"OrgOperation.ADMIN_ONLY_GADGETS": {true, "BASE", ""},
	}
	for name, want := range cases {
		op, err := table.Operation(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if op.Write != want.write || op.Facet != want.facet || op.LowestRole != want.lowest {
			t.Errorf("%s = %+v, want %+v", name, op, want)
		}
	}
	// A comment that names an operation must not grant it to the role.
	if table.roles["LIMITED_READ_ONLY"]["NetworkOperation.LAUNCH_WIDGETS"] {
		t.Error("an operation named only in a comment was granted")
	}
	if got := len(table.Operations("NetworkOperation")); got != 4 {
		t.Errorf("%d network operations, want 4", got)
	}
	if _, err := table.Operation("SHARED_NAME"); err == nil || !strings.Contains(err.Error(), "qualify") {
		t.Errorf("a bare name in both scopes must ask to be qualified: %v", err)
	}
	if op, err := table.Operation("TWEAK_WIDGETS"); err != nil || op.Scope != "NetworkOperation" {
		t.Errorf("a bare unique name resolves: %v %+v", err, op)
	}
	if _, err := table.Operation("NOT_A_THING"); err == nil {
		t.Error("an unknown operation is an error, not a No")
	}
}

func TestAccessCanFollowsForwardsEnforcement(t *testing.T) {
	t.Parallel()

	table, err := LoadAccessTable(miniCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	check := func(roles SessionRoles, operation, network string) AccessAnswer {
		t.Helper()
		answer, err := NewAccess(table, roles).Can(operation, network)
		if err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
		return answer
	}
	cases := []struct {
		name      string
		roles     SessionRoles
		operation string
		network   string
		want      Verdict
	}{
		{"org admin holds a write", SessionRoles{Org: []string{"ADMIN"}}, "TWEAK_WIDGETS", "9", VerdictYes},
		{"org admin holds an admin-only op", SessionRoles{Org: []string{"ADMIN"}}, "OrgOperation.ADMIN_ONLY_GADGETS", "", VerdictYes},
		{"org read-only holds a read", SessionRoles{Org: []string{"READ_ONLY"}}, "VIEW_WIDGETS", "9", VerdictYes},
		{"org read-only does not hold a write", SessionRoles{Org: []string{"READ_ONLY"}}, "TWEAK_WIDGETS", "9", VerdictNo},
		{"network role holds it on that network", SessionRoles{Org: []string{}, Network: map[string]string{"9": "OPERATOR"}}, "TWEAK_WIDGETS", "9", VerdictYes},
		{"a lower network role does not", SessionRoles{Network: map[string]string{"9": "READ_ONLY"}}, "TWEAK_WIDGETS", "9", VerdictNo},
		{"the role is the one on THAT network", SessionRoles{Network: map[string]string{"8": "ADMIN", "9": "READ_ONLY"}}, "LAUNCH_WIDGETS", "9", VerdictNo},
		{"no role on that network", SessionRoles{Network: map[string]string{"8": "ADMIN"}}, "VIEW_WIDGETS", "9", VerdictNo},
		{"an org op is granted by a network role on any network", SessionRoles{Network: map[string]string{"8": "WORKSPACE_OPERATOR"}}, "MANAGE_GADGETS", "", VerdictYes},
		{"an org op is not granted by a lower network role", SessionRoles{Network: map[string]string{"8": "OPERATOR"}}, "MANAGE_GADGETS", "", VerdictNo},
		{"an admin-only org op is no for any network role", SessionRoles{Network: map[string]string{"8": "ADMIN"}}, "OrgOperation.ADMIN_ONLY_GADGETS", "", VerdictNo},
		{"empty roles are unknown, not a refusal", SessionRoles{}, "VIEW_WIDGETS", "9", VerdictUnknown},
		{"system roles are not modelled", SessionRoles{System: []string{"SUPPORT_ADMIN"}, Org: []string{}}, "VIEW_WIDGETS", "9", VerdictUnknown},
		{"an unrecognised network role is unknown", SessionRoles{Network: map[string]string{"9": "ROLE_FROM_THE_FUTURE"}}, "VIEW_WIDGETS", "9", VerdictUnknown},
		{"an unrecognised org role is unknown, not no", SessionRoles{Org: []string{"AUDITOR"}}, "VIEW_WIDGETS", "9", VerdictUnknown},
	}
	for _, tc := range cases {
		got := check(tc.roles, tc.operation, tc.network)
		if got.Verdict != tc.want || got.Reason == "" {
			t.Errorf("%s: %s (%s), want %s", tc.name, got.Verdict, got.Reason, tc.want)
		}
	}
	answer := check(SessionRoles{Network: map[string]string{"9": "OPERATOR"}}, "TWEAK_WIDGETS", "9")
	if answer.Facet != "BASE" || !answer.Write || answer.LowestRole != "OPERATOR" || answer.Operation != "NetworkOperation.TWEAK_WIDGETS" {
		t.Errorf("the answer must say which licence facet a Yes still depends on: %+v", answer)
	}
	if _, err := NewAccess(table, SessionRoles{Org: []string{"ADMIN"}}).Can("TWEAK_WIDGETS", ""); err == nil {
		t.Error("a network operation needs a network id")
	}
	if _, err := NewAccess(table, SessionRoles{}).Can("NOT_A_THING", "9"); err == nil {
		t.Error("an unknown operation must be an error")
	}
	// The zero value and a missing table are unknown with the reason, never permission.
	if none, _ := NewAccess(nil, SessionRoles{Org: []string{"ADMIN"}}).Can("TWEAK_WIDGETS", "9"); none.Verdict != VerdictUnknown || !strings.Contains(none.Reason, "FORWARD_SOURCE") {
		t.Errorf("no table: %+v", none)
	}
	var zero Verdict
	if zero != VerdictUnknown {
		t.Error("the zero verdict must be unknown")
	}
}

func TestNetworkRoleAtLeastNeedsNoTable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		have, want string
		verdict    Verdict
	}{
		{"ADMIN", "OPERATOR", VerdictYes}, {"operator", " OPERATOR ", VerdictYes}, {"READ_ONLY", "OPERATOR", VerdictNo},
		{"WORKSPACE_OPERATOR", "ADMIN", VerdictNo}, {"AUDITOR", "OPERATOR", VerdictUnknown}, {"ADMIN", "", VerdictUnknown},
	} {
		if got := NetworkRoleAtLeast(tc.have, tc.want); got != tc.verdict {
			t.Errorf("AtLeast(%q, %q) = %s, want %s", tc.have, tc.want, got, tc.verdict)
		}
	}
}

func TestAccessTableJSONRoundTripAndEnvironment(t *testing.T) {
	root := miniCheckout(t)
	table, err := LoadAccessTable(root)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := table.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "table.json")
	if err := os.WriteFile(copyPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAccessTableJSON(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	roles := SessionRoles{Network: map[string]string{"9": "OPERATOR"}}
	for _, op := range []string{"VIEW_WIDGETS", "TWEAK_WIDGETS", "LAUNCH_WIDGETS"} {
		a, _ := NewAccess(table, roles).Can(op, "9")
		b, _ := NewAccess(loaded, roles).Can(op, "9")
		if a.Verdict != b.Verdict || a.LowestRole != b.LowestRole {
			t.Errorf("%s differs after a JSON round trip: %+v vs %+v", op, a, b)
		}
	}

	t.Setenv("FORWARD_RBAC_TABLE", "")
	t.Setenv("FORWARD_SOURCE", "")
	if none, err := AccessTableFromEnv(); err != nil || none != nil {
		t.Fatalf("neither variable set is no table, not an error: %v %v", err, none)
	}
	t.Setenv("FORWARD_SOURCE", root)
	if fromSource, err := AccessTableFromEnv(); err != nil || fromSource == nil {
		t.Fatalf("FORWARD_SOURCE: %v", err)
	}
	t.Setenv("FORWARD_RBAC_TABLE", copyPath)
	if fromJSON, err := AccessTableFromEnv(); err != nil || fromJSON == nil || fromJSON.Source != root {
		t.Fatalf("FORWARD_RBAC_TABLE takes precedence: %v %+v", err, fromJSON)
	}
	t.Setenv("FORWARD_RBAC_TABLE", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := AccessTableFromEnv(); err == nil {
		t.Error("a variable set to a wrong path is an error, never a silent no-table")
	}
	t.Setenv("FORWARD_RBAC_TABLE", "")
	t.Setenv("FORWARD_SOURCE", t.TempDir())
	if _, err := AccessTableFromEnv(); err == nil {
		t.Error("a directory that is not a Forward checkout is an error")
	}
}

func TestAccessTableRefusesAnUnparseableCheckout(t *testing.T) {
	t.Parallel()

	root := miniCheckout(t)
	dir := filepath.Join(root, accessSourceDir)
	if err := os.WriteFile(filepath.Join(dir, "NetworkRole.java"), []byte("package x;\npublic enum NetworkRole {\n    ADMIN(\n        1,\n        Sets.immutableEnumSet(NetworkOperation.NO_SUCH_OPERATION)\n    );\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessTable(root); err == nil {
		t.Fatal("a role naming an operation the table lacks, and missing ladder roles, must fail rather than load a partial table")
	}
	if err := os.WriteFile(filepath.Join(dir, "OrgOperation.java"), []byte("package x;\n// changed layout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccessTable(root); err == nil || !strings.Contains(err.Error(), "no operations") {
		t.Fatalf("a layout with no operations is an error: %v", err)
	}
}

// Users.Access reads the account's own session roles once and answers every later check without another request.
func TestUsersAccessReadsTheSessionOnce(t *testing.T) {
	t.Parallel()

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/users/current" {
			t.Errorf("unexpected %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"user":{"username":"u"},"roles":{"org":"ADMIN","network":{}}}`))
	}))
	defer server.Close()
	table, err := LoadAccessTable(miniCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := newTestClient(t, server.URL).Users.Access(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"VIEW_WIDGETS", "TWEAK_WIDGETS", "LAUNCH_WIDGETS"} {
		if answer, err := access.Can(op, "9"); err != nil || answer.Verdict != VerdictYes {
			t.Errorf("%s: %v %+v", op, err, answer)
		}
	}
	if requests != 1 {
		t.Errorf("%d requests, want 1", requests)
	}
}

// Against a real Forward checkout (FORWARD_SOURCE, or ~/src/fwd) the parser must find both tables and a complete ladder, and the one
// documented fact (docs.fwd.app, Settings > Accounts > RBAC: Collection Triggering). Skipped without a checkout.
func TestAccessTableAgainstARealCheckout(t *testing.T) {
	root := os.Getenv("FORWARD_SOURCE")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, "src", "fwd")
	}
	if _, err := os.Stat(filepath.Join(root, accessSourceDir)); err != nil {
		t.Skip("no Forward source checkout (set FORWARD_SOURCE)")
	}
	table, err := LoadAccessTable(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d network and %d org operations", len(table.Operations("NetworkOperation")), len(table.Operations("OrgOperation")))
	if len(table.Operations("NetworkOperation")) < 20 || len(table.Operations("OrgOperation")) < 10 {
		t.Fatalf("suspiciously few operations: the parser may have drifted from the source layout")
	}
	trigger, err := table.Operation("NetworkOperation.TRIGGER_SNAPSHOT_COLLECTION")
	if err != nil || trigger.LowestRole != "WORKSPACE_OPERATOR" {
		t.Fatalf("collection triggering is Workspace Operator and up: %v %+v", err, trigger)
	}
}

// Forward grants an impersonated session, and a support user in a supported org, things GET /users/current does not show, so a No would
// be confidently wrong: both must answer Unknown. A blank network role is "Forward did not say", also Unknown.
func TestAccessIsUnknownWhereForwardGrantsWhatTheRolesDoNotShow(t *testing.T) {
	t.Parallel()

	table, err := LoadAccessTable(miniCheckout(t))
	if err != nil {
		t.Fatal(err)
	}
	roles := SessionRoles{Network: map[string]string{"9": "READ_ONLY"}}
	for name, access := range map[string]*Access{
		"impersonated": NewAccessForSession(table, &UserSession{Impersonator: "admin", Roles: roles}),
		"supported":    NewAccess(table, SessionRoles{Network: roles.Network, SupportedOrgIDs: []string{"7"}}),
		"blank role":   NewAccess(table, SessionRoles{Network: map[string]string{"9": ""}}),
	} {
		got, err := access.Can("TWEAK_WIDGETS", "9")
		if err != nil || got.Verdict != VerdictUnknown || got.Reason == "" {
			t.Errorf("%s: %+v %v, want unknown with a reason", name, got, err)
		}
	}
	if got, _ := NewAccessForSession(table, &UserSession{Roles: roles}).Can("TWEAK_WIDGETS", "9"); got.Verdict != VerdictNo {
		t.Errorf("a plain session is still answered: %+v", got)
	}
}

// A role built by a form the parser does not follow must fail the load, not silently grant too much or too little; and inheriting one
// scope from a role must not copy that role's other scope.
func TestAccessTableFailsClosedOnUnfollowedFormsAndInheritsPerScope(t *testing.T) {
	t.Parallel()

	for _, form := range []string{"OrgOperation.VALUES.stream().filter(x -> true)", "Sets.difference(READ_ONLY.allowedOperations, EnumSet.of(NetworkOperation.TWEAK_WIDGETS))"} {
		root := miniCheckout(t)
		path := filepath.Join(root, accessSourceDir, "NetworkRole.java")
		raw, _ := os.ReadFile(path)
		edited := strings.Replace(string(raw), "Sets.immutableEnumSet(WORKSPACE_OPERATOR.allowedOperations)", "Sets.immutableEnumSet("+form+")", 1)
		if edited == string(raw) {
			t.Fatal("fixture edit did not apply")
		}
		if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadAccessTable(root); err == nil || !strings.Contains(err.Error(), "refusing to guess") {
			t.Errorf("%q must fail closed: %v", form, err)
		}
	}

	root := miniCheckout(t)
	path := filepath.Join(root, accessSourceDir, "NetworkRole.java")
	raw, _ := os.ReadFile(path)
	// ADMIN inherits only org operations from LIMITED_READ_ONLY here (plus its own network list): LIMITED's network ops must not follow.
	edited := strings.Replace(string(raw), "Sets.immutableEnumSet(WORKSPACE_OPERATOR.allowedOrgOperations),\n        Sets.immutableEnumSet(WORKSPACE_OPERATOR.allowedOperations)",
		"Sets.immutableEnumSet(LIMITED_READ_ONLY.allowedOrgOperations),\n        Sets.immutableEnumSet(NetworkOperation.EDIT_ME)", 1)
	if edited == string(raw) {
		t.Fatal("fixture edit did not apply")
	}
	edited = strings.Replace(edited, "NetworkOperation.EDIT_ME", "NetworkOperation.LAUNCH_WIDGETS", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	table, err := LoadAccessTable(root)
	if err != nil {
		t.Fatal(err)
	}
	if table.roles["ADMIN"]["NetworkOperation.VIEW_WIDGETS"] {
		t.Error("inheriting a role's org operations copied its network operations")
	}
	if !table.roles["ADMIN"]["OrgOperation.VIEW_GADGETS"] || !table.roles["ADMIN"]["NetworkOperation.LAUNCH_WIDGETS"] {
		t.Error("the inherited org operation or the role's own network operation is missing")
	}
}
