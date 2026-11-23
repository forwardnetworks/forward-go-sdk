package domain

// DeviceDiffSummary is one device that differs between two snapshots in a family, with the count of changed entries by diff type
// (ADDED, DELETED, MODIFIED; the map also carries UNCHANGED). It has the same fields as ACLDiffDevice, which predates it. Stats says how many entries changed; the per-device method lists them.
type DeviceDiffSummary struct {
	InventoryDeviceInfo
	DeviceName      string         `json:"deviceName"`
	HasConfigChange bool           `json:"hasConfigChange"`
	Stats           map[string]int `json:"stats"`
}

// ARPDiffEntry is one ARP entry. FileLines names the stored config files and line ranges it came from, as in ACLDiffEntry.
type ARPDiffEntry struct {
	IPAddress  string                    `json:"ipAddress"`
	VRFName    string                    `json:"vrfName,omitempty"`
	MACAddress string                    `json:"macAddress,omitempty"`
	Port       string                    `json:"port,omitempty"`
	FileLines  map[string][]ACLLineRange `json:"fileLines,omitempty"`
}

// MACDiffEntry is one MAC-table entry.
type MACDiffEntry struct {
	MACAddress string `json:"macAddress"`
	// VLAN is a string: a VLAN id ("10"), "untagged", a range such as "1-4094" for all VLANs, or empty when Forward sends none.
	VLAN      string                    `json:"vlan,omitempty"`
	Ports     []string                  `json:"ports,omitempty"`
	FileLines map[string][]ACLLineRange `json:"fileLines,omitempty"`
}

// DeviceARPDiff is one device's changed ARP entries; for an ADDED entry only After is set, for DELETED only Before. Stats counts the
// entries by diff type and also carries UNCHANGED.
type DeviceARPDiff struct {
	Diffs []DiffEntry[ARPDiffEntry] `json:"diffs"`
	Stats map[string]int            `json:"stats,omitempty"`
}

// DeviceMACDiff is one device's changed MAC-table entries.
type DeviceMACDiff struct {
	Diffs []DiffEntry[MACDiffEntry] `json:"diffs"`
	Stats map[string]int            `json:"stats,omitempty"`
}

// L3PrefixOptions pages the prefix view of the routing diff. Zero values send nothing, so Forward applies its own defaults.
// PrefixFilter keeps rows whose prefix matches (a prefix such as 10.0.0.0/8).
type L3PrefixOptions struct {
	Offset       int
	Limit        int
	PrefixFilter string
}

// VulnerabilityDiffCount is the number of CVEs with a vulnerable device, and how many of those devices the internet can reach.
type VulnerabilityDiffCount struct {
	CVECount int `json:"cveCount"`
	// ExposedVulnerableDevicesCount is nil when Forward cannot tell (no internet exposure information), not zero.
	ExposedVulnerableDevicesCount *int `json:"exposedVulnerableDevicesCount,omitempty"`
}

// VulnerabilitiesDiffSummary counts what is new in the second snapshot (GET .../vulnerabilities/counts, VIEW_CVE_DIFFS; deprecated by
// Forward in favour of the bidirectional view).
type VulnerabilitiesDiffSummary struct {
	NewCVECount int `json:"newCveCount"`
	// NewExposedVulnerableDevicesCount is nil when internet exposure cannot be determined.
	NewExposedVulnerableDevicesCount *int `json:"newExposedVulnerableDevicesCount,omitempty"`
}

// BidirectionalVulnerabilities is the change in both directions: Warnings is what the second snapshot introduced, Improvements what
// it resolved.
type BidirectionalVulnerabilities struct {
	Warnings     VulnerabilityDiffCount `json:"warnings"`
	Improvements VulnerabilityDiffCount `json:"improvements"`
}

// BidirectionalRoutingLoops is the routing-loop change in both directions.
type BidirectionalRoutingLoops struct {
	Warnings     DiffCount `json:"warnings"`
	Improvements DiffCount `json:"improvements"`
}
