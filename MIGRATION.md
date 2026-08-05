# Skyforge migration plan

## Outcome and boundary

Skyforge keeps its existing `internal/changedemo/forward.Client` seam and its
`New`, `NewFor`, `NewUnavailable`, `ForNetwork`, and `Network` entry points.
Their implementation becomes a thin application adapter around
`*github.com/forwardnetworks/forward-go-sdk.Client`. Changedemo call sites do not
move.

`Collect` and `CollectInto` remain a deliberate Skyforge orchestration wrapper:
they own progress messages, change-pack device scope, cleanup policy, and the
order of workspace operations. They must make every Forward request through the
SDK services. They are not a second HTTP client and must contain no auth,
request construction, response decoding, ID conversion, or error sniffing.

Do not release or deploy a partial cutover. Endpoint families can be ported and
verified one at a time on the migration branch, but the production change is
atomic: merge only after the old `req`, `putText`, multipart code, `rawToStr`, and
string-based error classifier are gone. This prevents two transports and two
error models from becoming a supported state.

The migration is source-only. It makes no Forward, Kubernetes, or other live
infrastructure changes.

## Adapter construction

Keep Skyforge's credential resolver and URL-normalization policy at the service
boundary. `NewFor` resolves the owning user's base URL and Basic credential pair,
normalizes the saved host value, and constructs:

```go
sdk, err := forward.NewClient(forward.Config{
	BaseURL:            normalizedBaseURL,
	Username:           accessKey,
	Password:           secret,
	NetworkID:          networkID,
	HTTPClient:         skyforgeHTTPClient,
	InsecureSkipVerify: true, // existing lab/self-signed policy only
	Capabilities:       profileForTrack(track, expectedBuild),
})
```

The wrapper retains a construction error rather than falling back to global
credentials. `NewUnavailable(networkID, err)` wraps
`forward.NewUnavailable(networkID, err)`. `ForNetwork(id)` shallow-copies the
Skyforge wrapper and assigns `inner = inner.ForNetwork(id)`; the SDK clone shares
transport, credentials, hooks, and capability evidence. `Network()` delegates to
`inner.Network()`.

`New` may continue to read the current environment variables for local/CI use,
but it must route them into the same SDK constructor. It must never be the
fallback for a failed per-user resolution.

## Version and capability profiles

Primary and stable must have separate checked-in compatibility profiles keyed
by deployment track and expected appserver build. At client initialization,
read `/api/version` once through `Version.Get`, record the reported build, and
surface a clear mismatch in diagnostics. Do not infer feature support by parsing
the release string.

The minimum profile entries for changedemo are:

- workspace networks;
- snapshot subset export and multipart merge;
- persistent snapshot checks;
- Predict;
- structured BGP advertisements;
- collector progress.

For mutation endpoints, Skyforge should fail closed when the selected profile is
unknown or unsupported. The SDK permits unknown capabilities for general forward
compatibility, but an explicitly unsupported feature returns
`UnsupportedCapabilityError` before sending a request. Successful calls add
runtime evidence shared by scoped clones.

## Port sequence

1. **Freeze the seam and characterize behavior.** Keep existing changedemo tests
   and add adapter tests for per-user credentials, unavailable clients, and
   `ForNetwork`. Do not change call sites.

2. **Replace transport, IDs, and errors.** Put the SDK client inside the existing
   wrapper. Map collection conflicts with
   `errors.Is(err, forward.ErrCollectionAlreadyInProgress)`, unprocessed exports
   with `ErrSnapshotNotProcessed`, missing tenants with `ErrNetworkNotFound`, and
   credential failures with `ErrAuthentication`. Remove all caller-side message
   matching. Treat SDK `Identifier` as the only string-or-number decoder.

3. **Port read-only snapshot, NQE, and check access.** Delegate snapshot listing
   and `ResolveSnapshot` to `Snapshots.ResolveID`; delegate NQE to `NQE.Run`
   and use `NQEResult.RowsAny` to preserve the existing call-site shape without
   local response decoding.
   Replace `GetChecks` in the gate with `Checks.ForScoring`, pass
   `CheckRequirements` derived from the pack selectors, and consume
   `CheckSet.Checks()`. `ErrNoChecks` and `ErrCheckCorpusIncomplete` are error
   verdicts, never PASS 0/0.

4. **Port corpus promotion.** Change the promoter's desired body to SDK
   `NewCheck` and call `Checks.ExistingNames` plus `Checks.CreatePersistent`.
   Promote on a real processed snapshot. Because checks attach per snapshot and
   persist only onto later snapshots of that network, explicitly promote the
   corpus onto a workspace's processed seed/base snapshot before predicting or
   scoring in that workspace. Do not assume the parent's corpus moved sideways
   into the new workspace.

5. **Port collection and workspace lifecycle.** Use
   `Networks.CreateWorkspace`, `Networks.Delete`, `CollectorTasks.Start`,
   `CollectorTasks.Progress`, `Snapshots.List`, and the collection operation
   handle. An already-running collection is non-fatal only through the typed
   error; continue polling for a newer real `processingTrigger=COLLECTION`
   snapshot. Preserve caller cancellation and current observable cleanup.

6. **Port subset export and merge.** Use `Snapshots.ExportSubset` for the fresh
   include archive and parent exclude archive. Upload both with `Snapshots.Upload`
   using `Async` and `SkipSnapshotProcessing`; this is the snapshot-merge call.
   Keep `Collect`/`CollectInto` as the application wrapper described above. Test
   include/exclude bodies, repeated multipart `file` parts, cleanup on failure,
   workspace reuse, and post-merge resolution with a fake server.

7. **Port Predict as one family.** Use `Predict.CreateChangeSet`,
   `StageCommands`, `StageBGPAdvertisement`, `Run`, and
   `ListPredictedSnapshots`. The SDK keeps the create body to the exact four
   `NewChangeSetMeta` fields and removes interactive CLI wrappers. Never encode a
   BGP origination as CLI `network <prefix>`: affected builds divert the resulting
   CONFIG seed and Predict installs no CONFIG strategy, so it disappears from the
   predicted model. Only the structured advertisement endpoint is valid. Forward
   Predict does not model withdrawal; reject a change requiring withdrawal before
   claiming a Predict result.

8. **Port the remaining typed families.** Map `AddTags`, `AddTagsTo`, and
   `ListTags` to `DeviceTags`; map `UploadPerformance` to `Performance.Upload`
   so its collector credentials remain per-call and never replace the shared API
   credentials. Map AI chat/assist methods to `AI` and `AIAssist`; keep answer
   polling and progress emission in the application wrapper, using
   `AIFinalAnswer.AnswerText` for build-tolerant answer decoding.

9. **Delete the old client implementation and cut over atomically.** Remove
   `req`, `putText`, local multipart/export handling, `rawToStr`, duplicated
   snapshot structs, and `IsCollectionAlreadyInProgress`. Search the changedemo
   Forward package for direct `/api/` paths and require zero hits outside tests or
   SDK adapter mapping comments. Only then update the module dependency and make
   the cutover eligible to merge.

## Verification gates

During SDK work, run only fake/recorded-server tests:

```sh
go test ./...
go vet ./...
```

During the later Skyforge migration, its backend verification command is:

```sh
cd ~/src/skyforge/components/server
encore test ./...
```

Do not substitute `go test` in the Encore repository. Add focused tests for each
family before the final full suite. No test may call a live Forward appliance.

The final migration acceptance criteria are: one transport, one typed error
model, no local scalar-ID decoder, no empty-check scoring path, both track/build
profiles explicit, every changedemo endpoint routed through a typed SDK service,
and `encore test ./...` green.

## Rollback

Rollback is a source revert of the Skyforge dependency/adapter change. Because
the migration performs no infrastructure or data mutation beyond the same API
operations changedemo already performs, there is no infrastructure rollback.
Do not keep a runtime switch that selects the retired hand-rolled transport; it
would recreate the dual-client state this migration removes.
