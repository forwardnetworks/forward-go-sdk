# Release machinery. See CLAUDE.md ("Where the SDK lives and how it ships").
.PHONY: test release-check route-check release

test:
	go test -count=1 ./...

# The gate: gofmt, vet, build, generated files, plain and -race tests, and the route check.
release-check:
	scripts/release-check.sh

# Check every manifest route against Forward's controllers at the pinned builds (needs ~/src/fwd).
route-check:
	go run ./cmd/routecheck -fwd "$${FWD_SRC:-$$HOME/src/fwd}" -pins scripts/forward-pins -manifest coverage_manifest.json -allow scripts/routecheck-allow.txt

# make release TAG=v0.4.0 MSG="v0.4.0 ..."
release:
	@test -n "$(TAG)" || (echo "TAG=vX.Y.Z is required" >&2; exit 2)
	@test -n "$(MSG)" || (echo "MSG=\"tag message\" is required" >&2; exit 2)
	scripts/release.sh "$(TAG)" "$(MSG)"
