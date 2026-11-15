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
	// HeaderFieldsWithDefaults lists header fields Forward assumes a default for when the query gives none. Forward allows only ip_tos,
	// tp_src, app_id, url and user_id here (a 400 otherwise).
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
// optional parameters. An NQE check takes its name from the query: Forward answers 400 for a name, note or tags on one
// ("'name' not allowed for non-parameterized NQE checks", "'note' not allowed for NQE checks", "'tags' not allowed for NQE checks";
// ErrCheckFieldNotAllowed), so leave NewCheck.Name, Note and Tags empty with this definition; ValidateNewCheck says so before a request.
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

// ValidateNewCheck reports, without a request, the combinations Forward refuses with a 400 that CreatePersistent and Create would
// otherwise only learn from the response: a name, note or tags on an NQE or Predefined check. Create and CreatePersistent do not call it
// (a build may accept what another refuses); call it where an early, plain message is worth it.
func ValidateNewCheck(check NewCheck) error {
	if check.Definition == nil {
		return errors.New("forward: check definition is required")
	}
	checkType, _ := check.Definition["checkType"].(string)
	if checkType != CheckTypeNetworkQuery && !strings.EqualFold(checkType, CheckTypePredefined) {
		return nil
	}
	if check.Name != "" || check.Note != "" || len(check.Tags) != 0 {
		return fmt.Errorf("forward: a %s check takes no name, note or tags (Forward answers 400): the name comes from the query", checkType)
	}
	return nil
}
