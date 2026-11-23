// Package accessfile is the SDK's file-system adapter (the v0.9 hexagonal split): it reads Forward's access table from a source
// checkout, a JSON copy or the environment, and hands the bytes to the domain's parser through ports. It imports nothing of this
// module but ports. Only the root's composition-root.go imports it.
package accessfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/forwardnetworks/forward-go-sdk/ports"
)

// "May this account do X?" without trying X. Forward has no route that lists the operations an account may perform, but it enforces
// role-based access from two things its source states: an operation table (NetworkOperation, OrgOperation: whether each is a read or
// a write, and its licence facet) and a role ladder (which operations each network role holds, each role adding to the one below).
// AccessTable reads those from a local checkout of Forward's source, or from a JSON copy of that, AT RUN TIME. The table is Forward's
// per-operation permission matrix: it is deliberately NOT embedded in this public module and this repo carries no Forward data, only
// the parser, the evaluator and tests on an invented mini checkout. With no table every answer is Unknown, with the reason.
//
// The evaluator mirrors the enforcement in OrgAuthority, NetworkAuthority and UserPrincipal.isPermitted: org role ADMIN holds every
// operation; org role READ_ONLY holds every read; a NETWORK operation is checked against the role on that network; an ORG operation is
// granted if the org role allows it or ANY network role the user holds allows it (the network is ignored). Roles are the SESSION roles
// of GET /api/users/current: access-control-group grants merged in (for sign-in sources that use groups), and a parent network's role
// copied onto its workspaces when the org property WORKSPACE_NETWORK_ROLE_AUTO_PROPAGATION is on.
//
// The answer is about the account's OWN org: Can has no org context, so for a network of another org an org ADMIN is told Yes though
// Forward refuses it. It is also about the operation, not the route: ownership rules (a dashboard's creator, NQE directory permissions)
// can allow an action a No denies. The table is read from a checkout's working tree, which may differ from the deployment's build.
//
// A Yes does not promise the call succeeds: a licence facet, an organization property or the object's state can still refuse it, which
// is why the facet is reported. System roles are not modelled (answer Unknown), and neither are unrecognised role names or empty roles
// (Forward did not say), none of which is a refusal.

const accessSourceDir = "app/src/main/java/com/forwardnetworks/cv/access"

// LoadAccessTable parses the table from a Forward source checkout: root is the checkout, or the access package directory itself.
// An error means the path is not a Forward checkout of a layout this parser knows, never an empty table.
func LoadAccessTable(root string) (*ports.AccessTable, error) {
	dir := root
	if info, err := os.Stat(filepath.Join(root, accessSourceDir)); err == nil && info.IsDir() {
		dir = filepath.Join(root, accessSourceDir)
	}
	read := func(name string) (string, error) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("forward: %q does not look like a Forward source checkout: %w", root, err)
		}
		return string(raw), nil
	}
	return ports.ParseAccessTable(root, read)
}

// LoadAccessTableJSON reads a copy made by WriteJSON.
func LoadAccessTableJSON(path string) (*ports.AccessTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("forward: access table: %w", err)
	}
	return ports.ParseAccessTableJSON(raw, path)
}

// AccessTableFromEnv loads the table from FORWARD_RBAC_TABLE (a JSON copy) if set, else from FORWARD_SOURCE (a Forward checkout) if
// set. It returns (nil, nil) when neither is set, which the checker treats as "every answer unknown"; a variable that is set to a
// wrong path is an error, never a silent fallback to no table.
func AccessTableFromEnv() (*ports.AccessTable, error) {
	if path := strings.TrimSpace(os.Getenv("FORWARD_RBAC_TABLE")); path != "" {
		return LoadAccessTableJSON(path)
	}
	if path := strings.TrimSpace(os.Getenv("FORWARD_SOURCE")); path != "" {
		return LoadAccessTable(path)
	}
	return nil, nil
}
