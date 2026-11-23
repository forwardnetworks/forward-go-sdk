package domain

// CheckPatch is the body of Checks.Update (Forward's CheckUpdate). Every field is optional and a field left out is not changed: Forward
// reads an absent field and a JSON null alike as "keep". So the strings, booleans and priority are pointers, and Tags is sent as null
// when nil (unchanged) and as a list when non-nil, where an empty non-nil list clears the tags. Definition replaces the whole
// definition; it must be the same check type, and an NQE check keeps its queryId. Note cannot be updated for an NQE check (a 400).
type CheckPatch struct {
	Definition            map[string]any `json:"definition,omitempty"`
	Name                  *string        `json:"name,omitempty"`
	Note                  *string        `json:"note,omitempty"`
	Tags                  []string       `json:"tags"`
	Enabled               *bool          `json:"enabled,omitempty"`
	Priority              *string        `json:"priority,omitempty"`
	PerfMonitoringEnabled *bool          `json:"perfMonitoringEnabled,omitempty"`
}
