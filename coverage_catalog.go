package forward

// CoverageOperation is one method+route an exported typed symbol sends. The
// generated catalog maps each symbol to every operation it can send (more than
// one for kind-dependent and composite methods). coverage_manifest_test.go
// requires this catalog, the manifest, and the actual exported method set to
// agree; coverage_completeness_test.go requires every request-issuing exported
// method to be in it.
type CoverageOperation struct {
	Method string
	Route  string
}

//go:generate go run ./cmd/coveragegen -manifest coverage_manifest.json -catalog coverage_catalog_gen.go -markdown COVERAGE.md
