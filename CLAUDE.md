# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`github.com/forwardnetworks/forward-go-sdk` (package `forward`) is a hand-written Go client for the Forward Networks REST API, used by Terraform providers, Encore services, MCP servers and, primarily, Skyforge (`skyforge/components/server`). The root package has no third-party runtime dependencies; cobra is only used by `cmd/fwdctl`. Keep it that way.

## Commands

```sh
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

**Client and services.** `client.go` defines `Client`. It has one exported pointer field per API family (`Networks`, `Predict`, `Diffs`, ...). Each service is declared as `type XService service` (where `service` is just `{client *Client}`) and wired up in `bindServices()`. Adding a service means adding the struct field *and* the `bindServices` line. Each service lives in its own file (`diffs.go`, `predict_cloud.go`, ...). Methods usually return `(*T, *Response, error)`.

**Request construction.** Go through the client helpers (`newJSONRequest`, `newScopedRequest`/`newScopedJSONRequest`, `doRequired`, `doAccepted`, `doWithTimeout`). Don't build `http.Request`s by hand. Paths are relative and restricted: `NewRequest` and `Raw` only accept `/api/...`. Root-scoped routes (CBR backups, browser/cookie/CSRF) use the internal `pathScopeBackup`/`pathScopeBrowser` scopes, and only the typed `Backups`/`Browser` services use them. Never widen this so credentials could reach another host or a non-API route. Escape path segments with `url.PathEscape` and validate required IDs before making a request (see `diffsPath` in `diffs.go`).

**Principals.** `AuthMode` picks the credential: zero value = per-user, `AuthModeService`, `AuthModeBrowser`, `AuthModeNone`. Collector-authenticated calls (e.g. `Performance.UploadWithIdentity`) use the `CollectorRegistration.Identity()` returned at registration, not the API user's credential.

**Multi-tenant clones.** `ForNetwork(id)` returns a cheap clone that shares transport, credentials and the capability registry. `NewUnavailable` returns a client that fails closed. Network-scoped methods resolve an empty network ID from the client's bound network via `resolveNetworkID`.

**Errors.** `errors.go` holds `ErrorResponse` plus the `ErrorKind` classifier (`classifyErrorResponse`), with helpers like `IsStatus`, `IsErrorKind` and `IsCollectionAlreadyInProgress`. Consumers must never sniff error strings. Add a new kind here instead.

**Tolerant decoding.** Forward builds differ in response shapes. `decoding.go` provides `listResponse[T]`, which accepts either a bare array or one of several named envelope keys, and `optionalValue[T]`. Where the "unstated" vs "false/zero" distinction matters to a reconciler, keep it with pointer fields (`pointers.go`) rather than defaulting.

**Capabilities.** `capabilities.go`/`compatibility.go`: callers can bind a `CapabilityProfile` (track/build/features). Gated methods call `s.client.requireCapability(...)` before the request and `observeCapability(...)` on success (see `predict.go`). An unlisted capability means "unknown, try it". Only an explicit unsupported entry blocks the call.

**Long-running operations.** `poller.go` has the generic `Poller[T]` (context-cancelable `Wait` with `OnUpdate` callbacks). `operations.go` has the composite, JSON-serializable handles for collection and snapshot import, which can be resumed after a process restart (`ResumeCollectionOperation`, `Snapshots.ResumeOperation`).

**Retries are opt-in.** The zero `RetryPolicy` (`retry.go`) retries nothing. When a policy is set, only 429 and 503 are retried for every method, because both mean the request was refused before it was processed. Transport errors, 502, 504 and other 5xx leave the outcome unknown, so they are retried only for idempotent methods: a proxy that timed out says nothing about whether a POST already created its snapshot or change set. 501 is never retried. `Retry-After` is honored up to `MaxDelay` (default 5 minutes, so pre-existing schedules such as Skyforge's `fwdai.BusyRetry` keep every wait); a longer requested wait returns the response to the caller. Don't loosen any of this.

**Stable vs preview.** Routes in Forward's published OpenAPI are stable. Routes taken from appserver controllers or from consumer usage are marked `Preview` in the Go doc comments. Doc comments often name the appserver controller the wire shape was verified against. Keep doing this, because it records the evidence for a route or body shape.

## Coverage manifest (enforced by tests)

`coverage_manifest.json` maps each method+route to the typed SDK symbol(s) that send it. `go generate` produces `coverage_catalog_gen.go` and `COVERAGE.md` from it. Never hand-edit either file. Three tests enforce the manifest:
- `TestCoverageManifest` (`coverage_manifest_test.go`) fails if any entry isn't `COVERED`, if `distinct_method_routes` doesn't match the entry count, if a symbol isn't an exported method on an exported `Client` service field, if a symbol is `Raw.*` or takes `RawRequest`, or if the generated catalog disagrees with the manifest (fix: `go generate ./...`).
- `TestCoverageManifestIsComplete` (`coverage_completeness_test.go`) fails if any exported service method that issues a request (directly or through helpers) has no manifest route. A composite method must also list its callees' routes (e.g. `Diffs.MaterialSummary`), and a manifest symbol that sends nothing is flagged. Skyforge's route-liveness gate can only check calls it can map through this manifest.

- `TestCoverageManifestRoutesMatchTheWire` (`coverage_routes_test.go`) calls every manifest symbol against a recording server, with arguments synthesized by reflection and each auth mode tried in turn. It fails on any request the manifest doesn't declare for that symbol. If a method validates an argument against an enum, or the manifest pins a dynamic segment, add it to `wireArgOverrides`. `minExercised` is a floor on how many symbols actually reach the wire; raise it when you improve coverage, and never lower it just to get a new method past the check.

So every new request-issuing method needs a manifest entry: add it, bump `distinct_method_routes` and `semantic_call_sites`, append a dated note to `derived_from`, then run `go generate ./...`. `consumer_go_sha256` pins Skyforge's Go tree on purpose, so any consumer change forces a reviewed refresh through `cmd/skyforge-coverage`. The source of truth for corrections is the Skyforge audit document's Part 5 table, not that command's code.

## Where the SDK lives

- `origin` (Forgejo, `skyforge/forward-go-sdk`) is where development happens. Skyforge vendors it as a git submodule at `~/src/skyforge/components/server/third_party/forward-go-sdk` (see `replace` in the server's `go.mod`), and commits are sometimes made directly in that checkout.
- `github` (`forwardnetworks/forward-go-sdk`) is the published module. Consumers outside Skyforge pin its `vX.Y.Z` tags, so push `main` and the tag there when releasing.

## Tests

Tests are in the root package and use `httptest.NewServer` plus `NewClient(Config{BaseURL: server.URL, Username: "u", Password: "p"})`. They assert exact wire paths, query strings and bodies. Each test usually opens with a comment explaining the behavioral reason it exists (e.g. "omitting X must send no parameter so the appserver applies its own default"). Match that style.

## Related docs

- `MIGRATION.md`: the Skyforge cutover plan. Skyforge keeps its `internal/changedemo/forward.Client` seam as a thin adapter over this SDK, and every Forward request goes through SDK services (no second HTTP client in Skyforge).
- `docs/api-coverage.md`: the broader coverage direction beyond the Skyforge inventory.

## Commit style

The subject is `<Service or area>: <what changed, in plain words>`, often stating the wire fact that was learned (e.g. `licensing: the body field is signedLicenseKey and the verdict is status`). The body explains why and which appserver controller confirmed the shape.
