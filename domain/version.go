package domain

// APIVersion is the version reported by the Forward appliance.
type APIVersion struct {
	Build   string `json:"build"`
	Release string `json:"release"`
	Version string `json:"version"`
}
