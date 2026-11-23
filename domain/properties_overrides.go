package domain

import (
	"encoding/json"
)

// NetworkProperty is a property a single NETWORK can override for itself (Forward's NetworkProperty: a subset of the org properties).
type NetworkProperty string

// The network properties Forward defines (NetworkProperty at master, a subset of the org properties), in the lower-case spelling a live
// 26.10.6 appserver answers and accepts. A build may define more or fewer: an unknown one is a 400 (ErrUnknownNetworkProperty).
const (
	NetworkPropertyDisableFlowComputation          NetworkProperty = "disable_flow_computation"
	NetworkPropertyAlertNewDeviceFailurePercentage NetworkProperty = "alert_new_device_failure_percentage"
	NetworkPropertyAdvancedReachabilityAnalysis    NetworkProperty = "advanced_reachability_analysis"
)

// Values of NetworkPropertyAdvancedReachabilityAnalysis (Forward's AdvancedReachabilityAnalysisMode): ON_DEMAND (the default) computes the
// advanced analysis only when a user triggers it, ASYNC computes it after each snapshot finishes processing. An unknown value is a 400 that
// changes nothing. The retired INLINE value is read as ASYNC by Forward.
const (
	AdvancedReachabilityAsync    = "ASYNC"
	AdvancedReachabilityOnDemand = "ON_DEMAND"
)

// PropertySource says which level an effective property value came from.
type PropertySource string

const (
	// PropertySourceOrganization: the org has its own override row.
	PropertySourceOrganization PropertySource = "organization"
	// PropertySourceGlobal: no org row, and a global override is set.
	PropertySourceGlobal PropertySource = "global"
	// PropertySourceDefault: neither, so the property's built-in default applies.
	PropertySourceDefault PropertySource = "default"
)

// EffectiveOrganizationValue is one org's effective value of a property and the level it comes from.
type EffectiveOrganizationValue struct {
	// Value is what the org gets (GET /api/orgs/{orgId}/config/{property}), so it is right even if Source were not.
	Value  json.RawMessage
	Source PropertySource
}
