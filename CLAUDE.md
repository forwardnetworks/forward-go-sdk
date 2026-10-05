# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`github.com/forwardnetworks/forward-go-sdk` (package `forward`) is a hand-written Go client for the Forward Networks REST API, used by Terraform providers, Encore services, MCP servers and, primarily, Skyforge (`skyforge/components/server`). The root package has no third-party runtime dependencies; cobra is only used by `cmd/fwdctl`. Keep it that way.

## Commands

```sh
make release-check       # THE gate: gofmt, vet, build, generated files, plain and -race tests, route check
make route-check         # every manifest route against Forward's controllers at the pinned builds (needs ~/src/fwd)
make release TAG=vX.Y.Z MSG="..."   # gate, annotated tag, push both remotes, confirm the proxy (see "ships" below)

go generate ./...        # regenerate coverage_catalog_gen.go and COVERAGE.md from coverage_manifest.json
go test ./...
go test -run TestDiffsMaterialSummary ./   # single test (root package)
go vet ./...
go run ./cmd/fwdctl --help                 # CLI; creds from FWD_USER/FWD_PASS, URL from --url or FWD_HOST

# Cross-check the manifest against Skyforge's audit (needs a Skyforge checkout):
go run ./cmd/skyforge-coverage -audit /path/to/skyforge/docs/forward-api-sdk-migration-audit.md
#   -skyforge <root>      Skyforge repo root (default: inferred from the audit path)
#   -ignore-tree-drift    skip the consumer Go-tree SHA-256 check
```

## Architecture

**Client and services.** `client.go` defines `Client`. It has one exported pointer field per API family (`Networks`, `Predict`, `Diffs`, ...). Each service is declared as `type XService service` (where `service` is just `{client *Client}`) and wired up in `bindServices()`. Adding a service means adding the struct field *and* the `bindServices` line. Each service lives in its own file (`diffs.go`, `aliases.go`, ...). Methods usually return `(*T, *Response, error)`.

**Request construction.** Go through the client helpers (`newJSONRequest`, `newScopedRequest`/`newScopedJSONRequest`, `doRequired`, `doAccepted`, `doWithTimeout`). Don't build `http.Request`s by hand. Paths are relative and restricted: `NewRequest` and `Raw` only accept `/api/...`. Root-scoped routes (CBR backups, browser/cookie/CSRF) use the internal `pathScopeBackup`/`pathScopeBrowser` scopes, and only the typed `Backups`/`Browser` services use them. Never widen this so credentials could reach another host or a non-API route. Escape path segments with `url.PathEscape` and validate required IDs before making a request (see `diffsPath` in `diffs.go`).

**Principals.** `AuthMode` picks the credential: zero value = per-user, `AuthModeService`, `AuthModeBrowser`, `AuthModeNone`. Only the `Admin` service refuses a non-service client up front (`requireService`); every other service sends the request and lets Forward decide, so a refusal arrives as its typed 403 (`ErrPermissionDenied`, `MissingPermission`) with the real reason. Don't add a client-side principal check for a route Forward does not itself restrict to one. Collector-authenticated calls (e.g. `Performance.UploadWithIdentity`) use the `CollectorRegistration.Identity()` returned at registration, not the API user's credential.

**Multi-tenant clones.** `ForNetwork(id)` returns a cheap clone that shares transport, credentials and the capability registry. `NewUnavailable` returns a client that fails closed. Network-scoped methods resolve an empty network ID from the client's bound network via `resolveNetworkID`.

**Errors.** `errors.go` holds `ErrorResponse` plus the `ErrorKind` classifier (`classifyErrorResponse`), with helpers like `IsStatus`, `IsErrorKind` and `IsCollectionAlreadyInProgress`. Consumers must never sniff error strings. Add a new kind here instead. Order matters in `classifyErrorResponse`: specific checks go before the generic 404 network rule (`ErrEndpointNotServed`, Spring's "No endpoint <METHOD> <URL>." 404 for a route this deployment does not map, is checked first, because under `/api/networks/` it would otherwise read as a missing network). Several routes exist only on some deployments (org licences on SaaS; backups on on-prem Kubernetes); `make route-check` lists them from the controllers' `@ProfileCriteria`.

**Tolerant decoding.** Forward builds differ in response shapes. `decoding.go` provides `listResponse[T]`, which accepts either a bare array or one of several named envelope keys, and `optionalValue[T]`. Where the "unstated" vs "false/zero" distinction matters to a reconciler, keep it with pointer fields (`pointers.go`) rather than defaulting.

**Capabilities.** `capabilities.go`/`compatibility.go`: callers can bind a `CapabilityProfile` (track/build/features). Gated methods call `s.client.requireCapability(...)` before the request and `observeCapability(...)` on success (see `predict.go`). An unlisted capability means "unknown, try it". Only an explicit unsupported entry blocks the call.

**Long-running operations.** `poller.go` has the generic `Poller[T]` (context-cancelable `Wait` with `OnUpdate` callbacks). `operations.go` has the composite, JSON-serializable handles for collection and snapshot import, which can be resumed after a process restart (`ResumeCollectionOperation`, `Snapshots.ResumeOperation`).

**Retries are opt-in.** The zero `RetryPolicy` (`retry.go`) retries nothing. When a policy is set, only 429 and 503 are retried for every method, because both mean the request was refused before it was processed. Transport errors, 502, 504 and other 5xx leave the outcome unknown, so they are retried only for idempotent methods: a proxy that timed out says nothing about whether a POST already created its snapshot or change set. 501 is never retried. `Retry-After` is honored up to `MaxDelay` (default 5 minutes, so pre-existing schedules such as Skyforge's `fwdai.BusyRetry` keep every wait); a longer requested wait returns the response to the caller. `RefusedOnly` narrows retries to 429/503 alone; `Jitter` shortens backoff waits randomly but never a `Retry-After`. Don't loosen any of this.

**Stable vs preview.** Routes in Forward's published OpenAPI are stable. Routes taken from appserver controllers or from consumer usage are marked `Preview` in the Go doc comments. Doc comments often name the appserver controller the wire shape was verified against. Keep doing this, because it records the evidence for a route or body shape.

## Coverage manifest (enforced by tests)

`coverage_manifest.json` maps each method+route to the typed SDK symbol(s) that send it. `go generate` produces `coverage_catalog_gen.go` and `COVERAGE.md` from it. Never hand-edit either file. Three tests enforce the manifest:
- `TestCoverageManifest` (`coverage_manifest_test.go`) fails if any entry isn't `COVERED`, if `distinct_method_routes` doesn't match the entry count, if a symbol isn't an exported method on an exported `Client` service field, if a symbol is `Raw.*` or takes `RawRequest`, or if the generated catalog disagrees with the manifest (fix: `go generate ./...`).
- `TestCoverageManifestIsComplete` (`coverage_completeness_test.go`) fails if any exported service method that issues a request (directly or through helpers) has no manifest route. A composite method must also list its callees' routes (e.g. `Diffs.MaterialSummary`), and a manifest symbol that sends nothing is flagged. Skyforge's route-liveness gate can only check calls it can map through this manifest.
- `TestCoverageManifestRoutesMatchTheWire` (`coverage_routes_test.go`) calls every manifest symbol against a recording server, with arguments synthesized by reflection and each auth mode tried in turn. It fails on any request the manifest doesn't declare for that symbol. If a method validates an argument against an enum, or the manifest pins a dynamic segment, add it to `wireArgOverrides`; the map's argument index does not count the receiver, so index 0 is the context and 1 is the first real argument. Synthesized `time.Duration`s are zero (the SDK reads zero as "use the default"; a non-zero one is a real limit and made `Upload` time out under `-race`), and the recorder uses a private transport. `minExercised` is a floor on how many symbols actually reach the wire; raise it when you improve coverage, and never lower it just to get a new method past the check.

The completeness check has no exemptions, so the manifest and the request-issuing methods match exactly. A fourth check is not a test: `cmd/routecheck` (`make route-check`, run by the release gate) extracts every `@*Mapping` from Forward's own source at the commits in `scripts/forward-pins` and fails when a manifest route is not mapped at one of them; `scripts/routecheck-allow.txt` lists any exception, each with a reason.

So every new request-issuing method needs a manifest entry: add it, bump `distinct_method_routes` and `semantic_call_sites`, append a dated note to `derived_from`, then run `go generate ./...`. `consumer_go_sha256` pins Skyforge's Go tree on purpose, so any consumer change forces a reviewed refresh through `cmd/skyforge-coverage`. The source of truth for corrections is the Skyforge audit document's Part 5 table, not that command's code.

## Where the SDK lives and how it ships

- `origin` (Forgejo, `skyforge/forward-go-sdk`) is where development happens; `github` (`forwardnetworks/forward-go-sdk`) is the published module. A release is `main` plus an annotated `vX.Y.Z` tag pushed to **both** remotes, then confirmed with `go list -m github.com/forwardnetworks/forward-go-sdk@vX.Y.Z` (it must resolve through the proxy). Pre-1.0: a breaking change bumps the minor version, anything else the patch. Do it with `scripts/release.sh vX.Y.Z "message"` (or `make release`): it refuses a tag that exists locally or on either remote, a HEAD behind either remote, and anything but `main`, runs `scripts/release-check.sh`, tags, pushes, and waits for the proxy. It never commits; write the commit yourself. Tags are immutable on the proxy, so a bad one is fixed forward with the next version.
- **The gate is judged by exit codes, never by output.** `go test ... | grep | head && git commit` returns `head`'s status; that is how v0.3.41 was tagged with a failing race test. Run `scripts/release-check.sh` (or redirect `go test` to a file and test `$?`). Run the race tests more than once after touching timeouts or concurrency: a test that fails one run in three is a failure.
- Every consumer requires the published module, including Skyforge (`components/server/go.mod`, since 2026-09-30) and `forward-skills`. There is no vendored copy to edit in place any more: SDK changes reach Skyforge only through a tag.
- Before adding a route, check it exists on both Forward builds Skyforge pins: add it to the manifest and run `make route-check`, which reads `~/src/fwd` at those commits (`scripts/forward-pins`; move a pin only when Skyforge moves its own). Skyforge's generated `routes_{primary,stable}.tsv` are untracked there, so nothing here depends on them; `cmd/routecheck` is tested to find the same routes. Forward's source (`~/src/fwd`) and its published spec (`~/src/fwd/api/apis/*.yaml`) are the ground truth for shapes and error bodies, and the spec is sometimes wrong (a loop violation's diagnosis `query` is an object, not the string it says).
- Skyforge's `scripts/lib/check-forward-delete-sites.py` fails when the SDK gains a destructive method missing from its `DESTRUCTIVE_SDK` catalogue (names starting Delete, Remove, Clear, Deactivate, Reset, Prune, Purge, Invalidate, Wipe, Destroy or Revoke), and `internal/forwardapi/delete_audit.go` keeps a second list that must agree. Both are updated in the same Skyforge change that bumps its SDK pin, and its gate also scans this repo's `cmd/fwdctl` call sites, so removing a method or command leaves stale rows there. After a release that adds or removes one, tell the current Skyforge session (`ListAgents`; the name changes on restart).

## Tests

Tests are in the root package and use `httptest.NewServer` plus `newTestClient(t, server.URL)` (`client_test.go`), which gives each client a transport of its own. A test that must call `NewClient` itself sets `HTTPClient: privateHTTPClient()`: `httptest.Server.Close` closes idle connections on `http.DefaultTransport`, and parallel tests sharing it fail with "CloseIdleConnections called". Give timing-sensitive tests wide margins; they run under `-race`. They assert exact wire paths, query strings and bodies. Each test usually opens with a comment explaining the behavioral reason it exists (e.g. "omitting X must send no parameter so the appserver applies its own default"). Match that style.

## Related docs

- `MIGRATION.md`: the Skyforge cutover plan. Skyforge keeps its `internal/changedemo/forward.Client` seam as a thin adapter over this SDK, and every Forward request goes through SDK services (no second HTTP client in Skyforge).
- `docs/api-coverage.md`: the broader coverage direction beyond the Skyforge inventory.

## Commit style

The subject is `<Service or area>: <what changed, in plain words>`, often stating the wire fact that was learned (e.g. `licensing: the body field is signedLicenseKey and the verdict is status`). The body explains why and which appserver controller confirmed the shape.
