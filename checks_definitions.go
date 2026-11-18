package forward

import (
	"errors"
	"fmt"
	"strings"
)

// Check definition builders for NewCheck.Definition. A path check's definition is Forward's IntentCheck JSON: the checkType, the path
// query as "filters" (the same body the GUI Search page sends, so TracePathQuery builds it) and optional noise types and header fields.
// Source: core/.../check/json/IntentCheck.java, ExistsCheck.java, NqeCheck.java and ApiCheckType.java at origin/master; the Existential and
// Isolation shapes were used live on a 26.10.6 appserver by the AutoCon6 workshop (persistent checks, judged on predicted snapshots too).

// IntentCheckOptions are the optional fields of a path check's definition.
type IntentCheckOptions struct {
	// NoiseTypes are the traffic classes to ignore as noise (Forward's NoiseType names); empty sends none.
	NoiseTypes []string
	// HeaderFieldsWithDefaults lists header fields Forward assumes a default for when the query gives none. Forward allows exactly seven
	// here (SearchUtils.ALLOWED_HEADER_FIELDS_WITH_DEFAULTS: IP_TOS, IP_FRAG_OFFSET, TP_SRC, TCP_FLAGS, APP_ID, URL, USER_ID), so ip_tos,
	// ip_frag_offset, tp_src, tcp_flags, app_id, url and user_id; anything else is a 400. (The API schema's allowableValues lists only
	// five of them; the source's allowed set has all seven.) The SDK does not enforce the list.
	HeaderFieldsWithDefaults []string
	// ReturnPath is "ANY" or "SYMMETRIC": for an Existential check, also require a return path (SYMMETRIC: through the same devices in
	// reverse). Empty means no return-path validation. Refused for the other check types.
	ReturnPath string
}

func intentDefinition(checkType string, q TracePathQuery, opts IntentCheckOptions) (map[string]any, error) {
	filters, err := q.filters()
	if err != nil {
		return nil, err
	}
	def := map[string]any{"checkType": checkType, "filters": filters}
	if len(opts.NoiseTypes) != 0 {
		def["noiseTypes"] = opts.NoiseTypes
	}
	if len(opts.HeaderFieldsWithDefaults) != 0 {
		def["headerFieldsWithDefaults"] = opts.HeaderFieldsWithDefaults
	}
	if opts.ReturnPath != "" {
		if checkType != CheckTypeExists {
			return nil, fmt.Errorf("forward: ReturnPath applies to an Existential check, not %s", checkType)
		}
		if opts.ReturnPath != "ANY" && opts.ReturnPath != "SYMMETRIC" {
			return nil, fmt.Errorf("forward: ReturnPath %q must be ANY or SYMMETRIC", opts.ReturnPath)
		}
		def["returnPath"] = opts.ReturnPath
	}
	return def, nil
}

// ExistentialCheckDefinition is the definition of a check that passes when the traffic the query describes has a path through the network
// (an Existential check). Give the query's From and To locations and header filters (and a Chain to require a hop, as in "through r2");
// see TracePathQuery.
func ExistentialCheckDefinition(q TracePathQuery, opts IntentCheckOptions) (map[string]any, error) {
	return intentDefinition(CheckTypeExists, q, opts)
}

// IsolationCheckDefinition is the definition of a check that passes when the traffic the query describes has NO path (an Isolation check).
func IsolationCheckDefinition(q TracePathQuery, opts IntentCheckOptions) (map[string]any, error) {
	return intentDefinition(CheckTypeIsolation, q, opts)
}

// ReachabilityCheckDefinition is the definition of a check that passes when ALL the traffic the query describes is delivered (a
// Reachability check).
func ReachabilityCheckDefinition(q TracePathQuery, opts IntentCheckOptions) (map[string]any, error) {
	return intentDefinition(CheckTypeReachability, q, opts)
}

// NQECheckDefinition is the definition of a check that runs a published NQE library query (queryId, such as "Q_..." from the library) with
// optional parameters. The name rule depends on the parameters (NewNetworkCheck.java): a NON-parameterized NQE check takes its name from the
// query and Forward answers 400 "'name' not allowed for non-parameterized NQE checks" for one, while a PARAMETERIZED check (params given)
// REQUIRES a name ("'name' required for parameterized NQE checks"). An NQE check never takes a note, and no tags ("'note' not allowed for
// NQE checks", "'tags' not allowed for NQE checks"; ErrCheckFieldNotAllowed). ValidateNewCheck says so before a request.
func NQECheckDefinition(queryID string, params map[string]any) (map[string]any, error) {
	if strings.TrimSpace(queryID) == "" {
		return nil, errors.New("forward: an NQE check needs a library query ID")
	}
	def := map[string]any{"checkType": CheckTypeNetworkQuery, "queryId": strings.TrimSpace(queryID)}
	if len(params) != 0 {
		def["params"] = params
	}
	return def, nil
}

// ValidateNewCheck reports, without a request, the field combinations Forward refuses with a 400 (NewNetworkCheck's constructor), so a caller can
// get a plain message early. Create and CreatePersistent do not call it (a build may accept what another refuses). The rules:
//   - Predefined: no name.
//   - NQE: no name when the definition has no params, a name REQUIRED when it has params; never a note; never tags.
//   - tags only for Existential, Isolation and Reachability checks; a note is refused only for NQE; perfMonitoringEnabled only for Existential.
func ValidateNewCheck(check NewCheck) error {
	if check.Definition == nil {
		return errors.New("forward: check definition is required")
	}
	checkType, _ := check.Definition["checkType"].(string)
	kind := canonicalCheckKind(checkType)
	switch kind {
	case "predefined":
		if check.Name != "" {
			return fmt.Errorf("forward: a Predefined check takes no name (Forward answers 400)")
		}
	case "nqe":
		params, _ := check.Definition["params"].(map[string]any)
		parameterized := len(params) != 0
		switch {
		case parameterized && check.Name == "":
			return errors.New("forward: a parameterized NQE check requires a name (Forward answers 400)")
		case !parameterized && check.Name != "":
			return errors.New("forward: a non-parameterized NQE check takes no name: it comes from the query (Forward answers 400)")
		}
		if check.Note != "" {
			return errors.New("forward: an NQE check takes no note (Forward answers 400)")
		}
	}
	intent := kind == "existential" || kind == "isolation" || kind == "reachability"
	if len(check.Tags) != 0 && !intent {
		return fmt.Errorf("forward: tags are only allowed on Existential, Isolation and Reachability checks, not %s (Forward answers 400)", checkType)
	}
	if check.PerfMonitoringEnabled != nil && kind != "existential" {
		return fmt.Errorf("forward: perfMonitoringEnabled is only allowed on Existential checks, not %s (Forward answers 400)", checkType)
	}
	return nil
}

// canonicalCheckKind lower-cases a checkType and maps the legacy spellings Forward's CheckType.fromJson still accepts (ApiCheckType.Legacy:
// Exists, ExistentialCheck, IsolationCheck, ReachabilityCheck, NetworkQuery) to the current names, so a body using one is judged by the
// same rules as one using the current name.
func canonicalCheckKind(checkType string) string {
	switch kind := strings.ToLower(strings.TrimSpace(checkType)); kind {
	case "exists", "existentialcheck":
		return "existential"
	case "isolationcheck":
		return "isolation"
	case "reachabilitycheck":
		return "reachability"
	case "networkquery":
		return "nqe"
	default:
		return kind
	}
}
