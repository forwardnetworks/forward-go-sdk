package domain

import (
	"encoding/json"
)

// ACLDiffDevice is one device whose ACLs differ between two snapshots, with the
// count of changed entries by diff type (ADDED, DELETED, MODIFIED). Stats says
// how many entries changed; DeviceACL lists them.
type ACLDiffDevice struct {
	InventoryDeviceInfo
	DeviceName      string         `json:"deviceName"`
	HasConfigChange bool           `json:"hasConfigChange"`
	Stats           map[string]int `json:"stats"`
}

// ACLLineRange is a run of lines, inclusive at both ends.
type ACLLineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ACLMatch is one match condition of an ACL rule: a header Field (for example
// IPV4_SRC or TP_DST), its Values and whether it is Negated. Values is left
// raw because its shape follows the field's type (a list of strings for
// addresses, ranges or ports, a plain string for some others); Forward leaves
// Index out when it is 0.
type ACLMatch struct {
	Field   string          `json:"field"`
	Index   int             `json:"index,omitempty"`
	Values  json.RawMessage `json:"values,omitempty"`
	Negated bool            `json:"negated,omitempty"`
}

// ACLDiffEntry is one ACL rule in a device ACL diff: its Name (the rule name
// when the vendor has one), Action (PERMIT or DENY), the match conditions
// (Matches, NegatedMatches, with the interfaces split out as InputInterfaces
// and OutputInterfaces), ACLContext, and FileLines, which names the stored
// config files and line ranges the rule came from
// (cv.diff.result.AclDiffEntry). The ACL a rule belongs to is not a field:
// FileLines is the pointer back to it.
type ACLDiffEntry struct {
	Name             string                    `json:"name,omitempty"`
	Action           string                    `json:"action"`
	ACLContext       string                    `json:"aclContext,omitempty"`
	Matches          []ACLMatch                `json:"matches,omitempty"`
	NegatedMatches   []ACLMatch                `json:"negatedMatches,omitempty"`
	InputInterfaces  []string                  `json:"inputInterfaces,omitempty"`
	OutputInterfaces []string                  `json:"outputInterfaces,omitempty"`
	FileLines        map[string][]ACLLineRange `json:"fileLines,omitempty"`
}

// DeviceACLDiff is one device's ACL diff: the changed entries and their counts
// by diff type. For an ADDED entry only After is set, for DELETED only Before,
// and for MODIFIED both.
type DeviceACLDiff struct {
	Diffs []DiffEntry[ACLDiffEntry] `json:"diffs"`
	Stats map[string]int            `json:"stats,omitempty"`
}
