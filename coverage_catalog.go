package forward

// CoverageOperation is the checked SDK-side catalog entry for one exported
// typed symbol. coverage_manifest_test.go requires this catalog, the manifest,
// and the actual exported method set to agree.
type CoverageOperation struct {
	Method string
	Route  string
}

//go:generate go run ./cmd/coveragegen -manifest coverage_manifest.json -catalog coverage_catalog_gen.go -markdown COVERAGE.md
