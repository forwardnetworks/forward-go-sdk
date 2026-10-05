#!/usr/bin/env bash
# Tag and publish a release: gate, annotated tag, push main and the tag to BOTH remotes, then confirm
# the module proxy resolves it. It never commits: write the commit yourself (commit style in CLAUDE.md).
#
#   scripts/release.sh vX.Y.Z "tag message"
#
# Pre-1.0: a breaking change bumps the minor, anything else the patch.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
tag="${1:-}"
message="${2:-}"
module="github.com/forwardnetworks/forward-go-sdk"
remotes=(origin github)

[[ "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: $0 vX.Y.Z \"tag message\"" >&2; exit 2; }
[[ -n "${message}" ]] || { echo "a tag message is required" >&2; exit 2; }
[[ "$(git rev-parse --abbrev-ref HEAD)" == "main" ]] || { echo "release from main (on $(git rev-parse --abbrev-ref HEAD))" >&2; exit 1; }

for remote in "${remotes[@]}"; do
  git remote get-url "${remote}" >/dev/null || { echo "remote ${remote} is not configured" >&2; exit 1; }
  git fetch --quiet "${remote}" main --tags || { echo "cannot fetch ${remote}" >&2; exit 1; }
  git merge-base --is-ancestor "${remote}/main" HEAD || { echo "HEAD does not contain ${remote}/main: rebase or merge first" >&2; exit 1; }
  if [[ -n "$(git ls-remote --tags "${remote}" "refs/tags/${tag}")" ]]; then
    echo "tag ${tag} already exists on ${remote}; tags are immutable on the module proxy, pick the next version" >&2
    exit 1
  fi
done
if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then echo "tag ${tag} already exists locally" >&2; exit 1; fi

scripts/release-check.sh

git tag -a "${tag}" -m "${message}"
for remote in "${remotes[@]}"; do
  git push "${remote}" main
  git push "${remote}" "${tag}"
done

tmpmod="$(mktemp -d)"
trap 'rm -rf "${tmpmod}"' EXIT
(
  cd "${tmpmod}"
  go mod init x >/dev/null 2>&1
  for attempt in 1 2 3 4 5 6; do
    if go list -m "${module}@${tag}"; then exit 0; fi
    echo "proxy does not resolve ${tag} yet (attempt ${attempt}/6)" >&2
    sleep 10
  done
  exit 1
) || { echo "pushed ${tag}, but the proxy never resolved ${module}@${tag}" >&2; exit 1; }
echo "released ${tag}"
