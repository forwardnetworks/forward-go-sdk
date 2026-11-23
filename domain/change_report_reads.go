package domain

import (
	"time"
)

// ChangeReportFlow is a named path query compared on both snapshots (an opt-in section). Expectation is "valid" (the flow should keep a VALID
// path), "none" (it is expected to lose it, an isolation intent) or "any".
type ChangeReportFlow struct {
	Name        string
	Expectation string
	Query       TracePathQuery
}

// ChangeReportState asks for the state section: the BGP sessions, LLDP adjacencies, interface admin states and ACL entry counts of both
// snapshots (NQE reads). ExpectedRemoved lists identities (for example "bgp_session:core-1|default|192.0.2.9") whose loss is intended.
type ChangeReportState struct {
	LLDP            LLDPOptions
	Read            StateReadOptions
	ExpectedRemoved []string
}

// ChangeReportOptions controls Compare. The zero value reads every default section of a collected or predicted pair.
type ChangeReportOptions struct {
	// ChangeSetID names the Predict change set behind a predicted after snapshot. It is required for the security_rules section.
	ChangeSetID string
	// AfterKind is "predicted" or "collected"; empty reads it from the after snapshot.
	AfterKind string
	// MaxItems caps every list in the report (default 100).
	MaxItems int
	// CreatedAt pins meta.created_at (UTC, whole seconds); zero uses the current time.
	CreatedAt time.Time
	// SectionTimeout bounds each section's reads (default 5 minutes). A section that runs past it is unavailable with reason timeout.
	SectionTimeout time.Duration
	// Flows, when not empty, adds the flows section; State, when not nil, adds the state section.
	Flows []ChangeReportFlow
	State *ChangeReportState
}
