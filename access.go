package forward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// Verdict is the three-valued answer of an access check. The zero value is Unknown, so a forgotten check never reads as permission.
type Verdict int

const (
	// VerdictUnknown: the SDK cannot tell (no table, no or unreadable roles, a system role, an unrecognised role). Leave it to Forward.
	VerdictUnknown Verdict = iota
	// VerdictYes: the roles hold the operation.
	VerdictYes
	// VerdictNo: the roles are known and none holds the operation.
	VerdictNo
)

func (v Verdict) String() string {
	switch v {
	case VerdictYes:
		return "yes"
	case VerdictNo:
		return "no"
	default:
		return "unknown"
	}
}

// NetworkRoleLadder is Forward's network roles from the least to the most privileged; each holds everything the one before it does.
var NetworkRoleLadder = []string{"LIMITED_READ_ONLY", "READ_ONLY", "OPERATOR", "WORKSPACE_OPERATOR", "ADMIN"}

// NetworkRoleAtLeast reports whether the network role have is at or above want on NetworkRoleLadder. It needs no table. Unknown when
// either name is not on the ladder (compared ignoring case and surrounding space).
func NetworkRoleAtLeast(have, want string) Verdict {
	h, w := ladderIndex(have), ladderIndex(want)
	switch {
	case h < 0 || w < 0:
		return VerdictUnknown
	case h >= w:
		return VerdictYes
	default:
		return VerdictNo
	}
}

func ladderIndex(role string) int {
	role = strings.ToUpper(strings.TrimSpace(role))
	for i, name := range NetworkRoleLadder {
		if name == role {
			return i
		}
	}
	return -1
}

// AccessOperation is one operation of Forward's table. Scope is "NetworkOperation" or "OrgOperation"; Write says it changes things (a
// read is permitted for a while after the licence lapses); Facet is the licence facet it needs; LowestRole is the lowest NETWORK role
// that holds it, or empty when no network role does (an org ADMIN still does).
type AccessOperation struct {
	Scope      string `json:"scope"`
	Name       string `json:"name"`
	Write      bool   `json:"write"`
	Facet      string `json:"facet"`
	LowestRole string `json:"lowestRole,omitempty"`
}

// Qualified is "Scope.Name".
func (o AccessOperation) Qualified() string { return o.Scope + "." + o.Name }

// AccessTable is Forward's operations and role ladder, loaded at run time. Load it with LoadAccessTable (a Forward checkout),
// LoadAccessTableJSON (a copy made by WriteJSON) or AccessTableFromEnv, never embed it.
type AccessTable struct {
	// Source says where it was read from.
	Source string
	ops    map[string]AccessOperation // key Qualified()
	// roles maps a network role to the qualified operations it holds, inheritance applied.
	roles map[string]map[string]bool
}

// Operation looks an operation up by "NetworkOperation.NAME", "OrgOperation.NAME" or a bare NAME that exists in only one of them.
func (t *AccessTable) Operation(name string) (AccessOperation, error) {
	name = strings.TrimSpace(name)
	if t == nil {
		return AccessOperation{}, errors.New("forward: no access table loaded")
	}
	if op, ok := t.ops[name]; ok {
		return op, nil
	}
	if !strings.Contains(name, ".") {
		network, org := t.ops["NetworkOperation."+name], t.ops["OrgOperation."+name]
		switch {
		case network.Name != "" && org.Name != "":
			return AccessOperation{}, fmt.Errorf("forward: operation %q is both a NetworkOperation and an OrgOperation; qualify it", name)
		case network.Name != "":
			return network, nil
		case org.Name != "":
			return org, nil
		}
	}
	return AccessOperation{}, fmt.Errorf("forward: %q is not an operation in the access table", name)
}

// Operations lists the table's operations of one scope ("NetworkOperation", "OrgOperation", or "" for both), sorted by name.
func (t *AccessTable) Operations(scope string) []AccessOperation {
	if t == nil {
		return nil
	}
	var out []AccessOperation
	for _, op := range t.ops {
		if scope == "" || op.Scope == scope {
			out = append(out, op)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Qualified() < out[j].Qualified() })
	return out
}

var (
	operationLine = regexp.MustCompile(`(?m)^\s*([A-Z][A-Z0-9_]*)\(AccessType\.(READ|WRITE),\s*LicenseFacet\.([A-Z][A-Z0-9_]*)\)\s*[,;]`)
	roleStart     = regexp.MustCompile(`(?m)^ {4}([A-Z][A-Z0-9_]*)\(\s*\n\s*-?\d+,`)
	orgOperation  = regexp.MustCompile(`\bOrgOperation\.([A-Z][A-Z0-9_]*)\b`)
	netOperation  = regexp.MustCompile(`\bNetworkOperation\.([A-Z][A-Z0-9_]*)\b`)
	inheritOrg    = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*)\.allowedOrgOperations\b`)
	inheritNet    = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*)\.allowedOperations\b`)
	lineComment   = regexp.MustCompile(`//[^\n]*`)
	blockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// unsupportedForm is anything in a role block that builds a set some way this parser does not follow; it fails closed instead of
	// silently granting too much or too little.
	unsupportedForm = regexp.MustCompile(`\b(?:Org|Network)Operation\.(?:VALUES|values|valueOf)\b|\.stream\(|\b(?:difference|complementOf|union|intersection|filter|removeAll|retainAll|copyOf|noneOf|range)\b`)
)

const accessSourceDir = "app/src/main/java/com/forwardnetworks/cv/access"

// LoadAccessTable parses the table from a Forward source checkout: root is the checkout, or the access package directory itself.
// An error means the path is not a Forward checkout of a layout this parser knows, never an empty table.
func LoadAccessTable(root string) (*AccessTable, error) {
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
	table := &AccessTable{Source: root, ops: map[string]AccessOperation{}, roles: map[string]map[string]bool{}}
	for _, scope := range []string{"NetworkOperation", "OrgOperation"} {
		text, err := read(scope + ".java")
		if err != nil {
			return nil, err
		}
		matches := operationLine.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			return nil, fmt.Errorf("forward: no operations found in %s.java; the layout is not one this parser knows", scope)
		}
		for _, m := range matches {
			op := AccessOperation{Scope: scope, Name: m[1], Write: m[2] == "WRITE", Facet: m[3]}
			table.ops[op.Qualified()] = op
		}
	}
	roleText, err := read("NetworkRole.java")
	if err != nil {
		return nil, err
	}
	if err := table.parseRoles(roleText); err != nil {
		return nil, err
	}
	table.setLowestRoles()
	return table, nil
}

func (t *AccessTable) parseRoles(text string) error {
	starts := roleStart.FindAllStringSubmatchIndex(text, -1)
	if len(starts) == 0 {
		return errors.New("forward: no network roles found in NetworkRole.java; the layout is not one this parser knows")
	}
	end := len(text)
	if i := strings.Index(text, "\n    public static final"); i > starts[len(starts)-1][0] {
		end = i
	}
	type block struct {
		name       string
		own        map[string]bool
		inheritOrg map[string]bool // roles whose org operations this role inherits
		inheritNet map[string]bool // roles whose network operations this role inherits
	}
	var blocks []block
	for i, s := range starts {
		to := end
		if i+1 < len(starts) {
			to = starts[i+1][0]
		}
		body := lineComment.ReplaceAllString(blockComment.ReplaceAllString(text[s[0]:to], ""), "")
		b := block{name: text[s[2]:s[3]], own: map[string]bool{}, inheritOrg: map[string]bool{}, inheritNet: map[string]bool{}}
		if form := unsupportedForm.FindString(body); form != "" {
			return fmt.Errorf("forward: role %s builds its operations with %q, which this parser does not follow; refusing to guess", b.name, form)
		}
		for _, m := range orgOperation.FindAllStringSubmatch(body, -1) {
			b.own["OrgOperation."+m[1]] = true
		}
		for _, m := range netOperation.FindAllStringSubmatch(body, -1) {
			b.own["NetworkOperation."+m[1]] = true
		}
		for _, m := range inheritOrg.FindAllStringSubmatch(body, -1) {
			if m[1] != b.name {
				b.inheritOrg[m[1]] = true
			}
		}
		for _, m := range inheritNet.FindAllStringSubmatch(body, -1) {
			if m[1] != b.name {
				b.inheritNet[m[1]] = true
			}
		}
		blocks = append(blocks, b)
	}
	// Roles are declared from the lowest up and each inherits only from earlier ones, so one pass in order resolves inheritance.
	for _, b := range blocks {
		set := map[string]bool{}
		for q := range b.own {
			if _, ok := t.ops[q]; !ok {
				return fmt.Errorf("forward: role %s names %s, which is not in the operation tables", b.name, q)
			}
			set[q] = true
		}
		for scope, parents := range map[string]map[string]bool{"OrgOperation.": b.inheritOrg, "NetworkOperation.": b.inheritNet} {
			for parent := range parents {
				inherited, ok := t.roles[parent]
				if !ok {
					return fmt.Errorf("forward: role %s inherits from %s, which is not declared before it", b.name, parent)
				}
				for q := range inherited {
					if strings.HasPrefix(q, scope) {
						set[q] = true
					}
				}
			}
		}
		t.roles[b.name] = set
	}
	for _, name := range NetworkRoleLadder {
		if _, ok := t.roles[name]; !ok {
			return fmt.Errorf("forward: NetworkRole.java does not declare %s", name)
		}
	}
	return nil
}

func (t *AccessTable) setLowestRoles() {
	for q, op := range t.ops {
		for _, role := range NetworkRoleLadder {
			if t.roles[role][q] {
				op.LowestRole = role
				break
			}
		}
		t.ops[q] = op
	}
}

type accessTableJSON struct {
	Source     string              `json:"source,omitempty"`
	Operations []AccessOperation   `json:"operations"`
	Roles      map[string][]string `json:"roles"`
}

// WriteJSON writes the table as JSON, for a private copy that LoadAccessTableJSON reads. The output is Forward's permission matrix:
// keep it out of public repositories.
func (t *AccessTable) WriteJSON(w io.Writer) error {
	if t == nil {
		return errors.New("forward: no access table loaded")
	}
	doc := accessTableJSON{Source: t.Source, Operations: t.Operations(""), Roles: map[string][]string{}}
	for role, set := range t.roles {
		for q := range set {
			doc.Roles[role] = append(doc.Roles[role], q)
		}
		sort.Strings(doc.Roles[role])
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(encoded, '\n'))
	return err
}

// LoadAccessTableJSON reads a copy made by WriteJSON.
func LoadAccessTableJSON(path string) (*AccessTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("forward: access table: %w", err)
	}
	var doc accessTableJSON
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("forward: access table %s: %w", path, err)
	}
	if len(doc.Operations) == 0 || len(doc.Roles) == 0 {
		return nil, fmt.Errorf("forward: access table %s has no operations or roles", path)
	}
	table := &AccessTable{Source: doc.Source, ops: map[string]AccessOperation{}, roles: map[string]map[string]bool{}}
	for _, op := range doc.Operations {
		if (op.Scope != "NetworkOperation" && op.Scope != "OrgOperation") || op.Name == "" {
			return nil, fmt.Errorf("forward: access table %s: operation %+v has no valid scope and name", path, op)
		}
		if _, dup := table.ops[op.Qualified()]; dup {
			return nil, fmt.Errorf("forward: access table %s: operation %s appears twice", path, op.Qualified())
		}
		table.ops[op.Qualified()] = op
	}
	for _, role := range NetworkRoleLadder {
		if _, ok := doc.Roles[role]; !ok {
			return nil, fmt.Errorf("forward: access table %s has no role %s", path, role)
		}
	}
	for role, ops := range doc.Roles {
		set := map[string]bool{}
		for _, q := range ops {
			if _, ok := table.ops[q]; !ok {
				return nil, fmt.Errorf("forward: access table %s: role %s names unknown operation %s", path, role, q)
			}
			set[q] = true
		}
		table.roles[role] = set
	}
	return table, nil
}

// AccessTableFromEnv loads the table from FORWARD_RBAC_TABLE (a JSON copy) if set, else from FORWARD_SOURCE (a Forward checkout) if
// set. It returns (nil, nil) when neither is set, which the checker treats as "every answer unknown"; a variable that is set to a
// wrong path is an error, never a silent fallback to no table.
func AccessTableFromEnv() (*AccessTable, error) {
	if path := strings.TrimSpace(os.Getenv("FORWARD_RBAC_TABLE")); path != "" {
		return LoadAccessTableJSON(path)
	}
	if path := strings.TrimSpace(os.Getenv("FORWARD_SOURCE")); path != "" {
		return LoadAccessTable(path)
	}
	return nil, nil
}

// AccessAnswer is one check's result. Facet and Write describe the operation when it is known, even for Unknown and No, so a caller can
// say which licence facet a Yes still depends on.
type AccessAnswer struct {
	Verdict Verdict
	Reason  string
	// Operation is the qualified operation name checked, empty when it could not be resolved.
	Operation string
	Facet     string
	Write     bool
	// LowestRole is the lowest network role that holds the operation, empty when none does.
	LowestRole string
}

// Access answers "may this account do X?" for one set of session roles.
type Access struct {
	table        *AccessTable
	roles        SessionRoles
	impersonated bool
}

// NewAccess checks roles against table. A nil table is allowed: every answer is Unknown.
func NewAccess(table *AccessTable, roles SessionRoles) *Access {
	return &Access{table: table, roles: roles}
}

// NewAccessForSession checks a session's roles. An impersonated session (UserSession.Impersonator set) also holds impersonation grants
// that its roles do not show, so every answer for it is Unknown.
func NewAccessForSession(table *AccessTable, session *UserSession) *Access {
	return &Access{table: table, roles: session.Roles, impersonated: session.Impersonator != ""}
}

// Access reads the calling credential's own session roles (GET /api/users/current, Users.CurrentSession) and returns a checker over
// table (nil allowed). It is one request; the checks that follow make none.
func (s *UsersService) Access(ctx context.Context, table *AccessTable) (*Access, *Response, error) {
	session, resp, err := s.CurrentSession(ctx)
	if err != nil {
		return nil, resp, err
	}
	return NewAccessForSession(table, session), resp, nil
}

// Can reports whether the roles hold operation (see AccessTable.Operation for the accepted names). networkID is required for a network
// operation (the role is the one held on that network) and ignored for an org operation. It returns an error, not No, for an operation
// name the table does not have.
func (a *Access) Can(operation, networkID string) (AccessAnswer, error) {
	if a == nil || a.table == nil {
		return AccessAnswer{Reason: "no access table is loaded (set FORWARD_SOURCE to a Forward checkout or FORWARD_RBAC_TABLE to a JSON copy)"}, nil
	}
	op, err := a.table.Operation(operation)
	if err != nil {
		return AccessAnswer{}, err
	}
	answer := AccessAnswer{Operation: op.Qualified(), Facet: op.Facet, Write: op.Write, LowestRole: op.LowestRole}
	networkID = strings.TrimSpace(networkID)
	if op.Scope == "NetworkOperation" && networkID == "" {
		return answer, fmt.Errorf("forward: %s is a network operation: a network ID is required", op.Qualified())
	}
	r := a.roles
	switch {
	case a.impersonated:
		answer.Reason = "the session is impersonated: Forward adds impersonation grants (snapshot debug and invalidation) that the roles do not show"
		return answer, nil
	case len(r.SupportedOrgIDs) > 0:
		answer.Reason = "the account is a Forward support user for other orgs, where Forward admits it to support routes without checking the operation"
		return answer, nil
	case len(r.System) > 0:
		answer.Reason = fmt.Sprintf("the account holds system roles %v, which are not modelled", r.System)
		return answer, nil
	case len(r.Org) == 0 && len(r.Network) == 0:
		answer.Reason = "Forward returned no role information for this account"
		return answer, nil
	}
	unknownRole := ""
	for _, role := range r.Org {
		switch strings.ToUpper(strings.TrimSpace(role)) {
		case "ADMIN":
			answer.Verdict, answer.Reason = VerdictYes, "the account is an organization administrator, who holds every operation"
			return answer, nil
		case "READ_ONLY":
			if !op.Write {
				answer.Verdict, answer.Reason = VerdictYes, "the account's org role READ_ONLY holds every read operation"
				return answer, nil
			}
		default:
			unknownRole = "org role " + role
		}
	}
	held := func(role string) (bool, bool) {
		set, ok := a.table.roles[strings.ToUpper(strings.TrimSpace(role))]
		return set[op.Qualified()], ok
	}
	if op.Scope == "NetworkOperation" {
		if role, present := r.Network[networkID]; present && strings.TrimSpace(role) == "" {
			answer.Reason = "Forward gave no role for this account on network " + networkID
			return answer, nil
		} else if strings.TrimSpace(role) != "" {
			has, known := held(role)
			switch {
			case has:
				answer.Verdict, answer.Reason = VerdictYes, fmt.Sprintf("the account is %s on network %s", role, networkID)
				return answer, nil
			case !known:
				unknownRole = "network role " + role
			}
		}
	} else {
		for network, role := range r.Network {
			has, known := held(role)
			switch {
			case has:
				answer.Verdict, answer.Reason = VerdictYes, fmt.Sprintf("the account is %s on network %s, and an org operation is granted by any network role", role, network)
				return answer, nil
			case !known && strings.TrimSpace(role) != "":
				unknownRole = "network role " + role
			}
		}
	}
	if unknownRole != "" {
		answer.Reason = fmt.Sprintf("the account holds %s, which this table does not know", unknownRole)
		return answer, nil
	}
	answer.Verdict = VerdictNo
	answer.Reason = "none of the account's roles holds " + op.Qualified()
	if op.Scope == "NetworkOperation" {
		answer.Reason += " on network " + networkID
	}
	return answer, nil
}
