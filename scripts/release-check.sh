#!/usr/bin/env bash
# The gate every release must pass. Each step is judged by its own exit code; nothing is piped into
# something that could swallow a failure (v0.3.41 was tagged with a failing race test because
# `go test ... | head` returned head's status).
#
#   scripts/release-check.sh
#
# Environment:
#   ALLOW_DIRTY=1   skip the clean-tree check (for a gate run mid-work; never for a release)
#   RACE_RUNS=3     how many times to run `go test -race -count=2 ./...`
#   FWD_SRC=...     Forward source git dir for the route check (default ~/src/fwd)
#   ROUTECHECK=1    fail instead of skipping when the route check cannot run
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
RACE_RUNS="${RACE_RUNS:-3}"
FWD_SRC="${FWD_SRC:-$HOME/src/fwd}"
EXPECTED_EMAIL="craigjohnson@forwardnetworks.com"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { printf '\nrelease-check FAILED: %s\n' "$*" >&2; exit 1; }

step "identity and tree"
email="$(git config user.email || true)"
[[ "${email}" == "${EXPECTED_EMAIL}" ]] || fail "git user.email is '${email}', want ${EXPECTED_EMAIL} (see commit-identity memory)"
if [[ "${ALLOW_DIRTY:-}" != "1" ]]; then
  [[ -z "$(git status --porcelain)" ]] || { git status --short >&2; fail "working tree is not clean (ALLOW_DIRTY=1 to override for a mid-work run)"; }
fi

step "gofmt"
unformatted="$(gofmt -l .)"
[[ -z "${unformatted}" ]] || { printf '%s\n' "${unformatted}" >&2; fail "gofmt -l lists the files above"; }

step "go vet and go build"
go vet ./... || fail "go vet"
go build ./... || fail "go build"

step "generated files are current"
generated=(COVERAGE.md coverage_catalog_gen.go)
before="$(sha256sum "${generated[@]}")"
go generate ./... || fail "go generate"
after="$(sha256sum "${generated[@]}")"
[[ "${before}" == "${after}" ]] || fail "go generate changed ${generated[*]}: commit the regenerated files"

step "go test (plain)"
if ! go test -count=1 ./... >"${tmp}/plain.txt" 2>&1; then
  grep -E '^(--- FAIL|FAIL|panic)|_test.go:[0-9]+:' "${tmp}/plain.txt" | head -40 >&2 || true
  fail "go test ./... failed"
fi
tail -n 3 "${tmp}/plain.txt"

step "go test -race (${RACE_RUNS} runs of -count=2)"
for ((i = 1; i <= RACE_RUNS; i++)); do
  if ! go test -race -count=2 ./... >"${tmp}/race-${i}.txt" 2>&1; then
    grep -E '^(--- FAIL|FAIL|panic)|_test.go:[0-9]+:|DATA RACE' "${tmp}/race-${i}.txt" | head -40 >&2 || true
    fail "go test -race failed on run ${i} of ${RACE_RUNS}"
  fi
  printf 'race run %d/%d ok\n' "${i}" "${RACE_RUNS}"
done

step "route check against Forward's source"
if [[ ! -d "${FWD_SRC}/.git" && ! -f "${FWD_SRC}/HEAD" ]]; then
  if [[ "${ROUTECHECK:-}" == "1" ]]; then fail "ROUTECHECK=1 but ${FWD_SRC} is not a git repo (set FWD_SRC)"; fi
  printf 'SKIPPED: %s is not a git repo; routes were NOT checked against Forward (set FWD_SRC, or ROUTECHECK=1 to require it)\n' "${FWD_SRC}"
elif [[ ! -d cmd/routecheck ]]; then
  printf 'SKIPPED: cmd/routecheck does not exist yet\n'
else
  go run ./cmd/routecheck -fwd "${FWD_SRC}" -pins scripts/forward-pins -manifest coverage_manifest.json -allow scripts/routecheck-allow.txt || fail "route check"
fi

printf '\nrelease-check OK\n'
