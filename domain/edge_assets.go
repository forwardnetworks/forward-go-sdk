package domain

// EdgeAsset is an edge asset with its network-level criticality and its state in the network's latest snapshot.
type EdgeAsset struct {
	Name string `json:"name"`
	// Include says the asset is matched to hosts in later snapshots.
	Include     bool     `json:"include"`
	IPs         []string `json:"ips,omitempty"`
	MACAddress  string   `json:"macAddress,omitempty"`
	CloudHostID string   `json:"cloudHostId,omitempty"`
	// Criticality is BUSINESS_CRITICAL, CRITICAL_INFRA or CROWN_JEWEL; empty when none is set.
	Criticality string `json:"criticality,omitempty"`
	// EnrichmentSources name where extra information about the asset comes from (RAPID7, CLOUD, TENABLE_SC, TENABLE_VM).
	EnrichmentSources        []string   `json:"enrichmentSources,omitempty"`
	IncludedInLatestSnapshot bool       `json:"includedInLatestSnapshot"`
	CreatedByID              Identifier `json:"createdById,omitempty"`
	CreatedAt                string     `json:"createdAt,omitempty"`
	CreatedBy                string     `json:"createdBy,omitempty"`
	UpdatedByID              Identifier `json:"updatedById,omitempty"`
	UpdatedAt                string     `json:"updatedAt,omitempty"`
	UpdatedBy                string     `json:"updatedBy,omitempty"`
}

// EdgeAssetPatch changes an edge asset; a nil or empty field is left alone, and at least one is required.
type EdgeAssetPatch struct {
	// Include turns matching to later snapshots on or off.
	Include *bool `json:"include,omitempty"`
	// Name renames the asset.
	Name string `json:"name,omitempty"`
}
