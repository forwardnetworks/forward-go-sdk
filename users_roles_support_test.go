package forward

import (
	"encoding/json"
	"reflect"
	"testing"
)

// A support grant is read back from GET /api/users/{id}/roles: Forward adds system roles and supportedOrgIds (only the unexpired
// grants) next to the org and network roles. Forward has no per-org delete, so removing one means a PUT of the remaining set, which needs
// this read (and renews every kept org to 30 days).
func TestUserRolesCarrySystemRolesAndSupportedOrgs(t *testing.T) {
	t.Parallel()

	var roles UserRoles
	body := `{"system":["SUPPORT_ADMIN"],"org":[],"network":{"7":"ADMIN"},"supportedOrgIds":["205","310"]}`
	if err := json.Unmarshal([]byte(body), &roles); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roles.System, []string{"SUPPORT_ADMIN"}) || !reflect.DeepEqual(roles.SupportedOrgIDs, []string{"205", "310"}) || !reflect.DeepEqual(roles.NetworkRoles("7"), []string{"ADMIN"}) {
		t.Fatalf("decoded %+v", roles)
	}
	// Decoding into a value that already holds grants must not leave stale ones behind.
	if err := json.Unmarshal([]byte(`{"org":[],"network":{}}`), &roles); err != nil || len(roles.System) != 0 || len(roles.SupportedOrgIDs) != 0 {
		t.Fatalf("stale fields survived a decode: %v %+v", err, roles)
	}
	var plain UserRoles
	if err := json.Unmarshal([]byte(`{"org":["ADMIN"],"network":{}}`), &plain); err != nil || len(plain.System) != 0 || len(plain.SupportedOrgIDs) != 0 || !plain.HasOrgAdmin() {
		t.Fatalf("an ordinary user has neither: %v %+v", err, plain)
	}
}
