# Forward Networks Go SDK

`forward-go-sdk` is a reusable Go client for the Forward Networks REST API.
It is intended for Terraform providers, Encore services, MCP servers, and
other Go programs that need the same authenticated API contract.

The SDK currently provides:

- explicit user, service, collector, browser-cookie, and unauthenticated
  principal modes; Basic credentials and API tokens (`accessKey:secret`) remain
  supported
- safe appliance URL and request-path handling
- caller-supplied `http.Client` support for tracing, proxies, custom CAs, and
  application-specific timeouts
- typed API errors with HTTP status inspection and stable classifications for
  collection conflicts, unprocessed snapshots, missing networks, and auth
- cheap `ForNetwork` clones that share transport/credentials for multi-tenant
  services, plus a fail-closed unavailable client
- typed Version, Networks, Snapshots, and NQE services
- device, credential, cloud-source, collection-task, and workspace lifecycle
- device-tag assignment and collector-authenticated performance upload
- stateful pollers and callbacks for long-running operations
- typed preview bindings for Predict, AI Chat, AI Assist, webhooks,
  organizations, and organization properties
- snapshot-bound persistent checks with a non-empty scoring guard
- explicit per-track capability profiles and runtime capability evidence
- typed coverage for every Forward method+route currently called by Skyforge,
  enforced by `coverage_manifest_test.go`
- a safe `Raw` service for other `/api` endpoints not typed yet

The module has no third-party runtime dependencies.

## Install

```sh
go get github.com/forwardnetworks/forward-go-sdk
```

## Typed API usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	forward "github.com/forwardnetworks/forward-go-sdk"
)

func main() {
	client, err := forward.NewClient(forward.Config{
		BaseURL:  "https://fwd.example.com",
		APIToken: "access-key:secret",
		UserAgent: "my-forward-tool/1.0",
	})
	if err != nil {
		log.Fatal(err)
	}

	networks, _, err := client.Networks.List(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, network := range networks {
		fmt.Println(network.ID, network.Name)
	}
}
```

## Stateful operations and callbacks

Long-running operations return a `Poller[T]` that retains the most recent
server state. `Wait` is context-cancelable and can publish each transition to
Encore events/SSE, Terraform diagnostics, logs, or metrics.

For collection, use the composite operation. It captures the newest existing
snapshot as a baseline, follows the collector task, identifies only the newer
`COLLECTION` snapshot, and waits for processing. Its handle is JSON-safe and
can be stored by Skyforge before a worker returns:

```go
operation, _, err := client.CollectorTasks.StartCollectionOperation(
	ctx,
	networkID,
	forward.CollectionStartOptions{},
)
if err != nil {
	return err
}

handle := operation.Handle() // persist this durable value
snapshot, err := operation.Wait(ctx, forward.CollectionWaitOptions{
	PollInterval: 5 * time.Second,
	OnUpdate: func(update forward.CollectionOperationUpdate) {
		log.Printf("collection %s -> %s", update.Previous.Phase, update.Current.Phase)
	},
})

// After a process restart:
operation, err = client.CollectorTasks.ResumeCollectionOperation(handle)
```

Collection waiting has independent snapshot-appearance and snapshot-processing
deadlines. Once a snapshot has appeared, it must be absent from several
successful observations before the SDK reports `ErrSnapshotDisappeared`.

Asynchronous imports use the same model. `StartUploadOperation` forces
`async=true`, returns a durable `SnapshotOperationHandle`, and supports
`Snapshots.ResumeOperation`.

Lower-level task-only operations still use the generic poller:

```go
operation, _, err := client.CollectorTasks.StartOperation(ctx, networkID)
if err != nil {
	return err
}

task, _, err := operation.Wait(ctx, forward.PollOptions[forward.CollectorTask]{
	Interval: 5 * time.Second,
	OnUpdate: func(update forward.PollUpdate[forward.CollectorTask]) {
		log.Printf("collection %s -> %s", update.Previous.Status, update.Value.Status)
	},
})
```

Equivalent generic handles exist for asynchronous NQE, Predict snapshot
processing, and AI chats. `Config.Hooks` observes sanitized request/response
events without exposing authentication, query values, or bodies. Forward
outbound callbacks are managed separately through `client.Webhooks`.

`Event.Operation` and `Event.AuthMode` describe the logical SDK call. An
injected `http.RoundTripper` can read `MetadataFromRequest` on every transport
attempt. This keeps operation-level telemetry in hooks and attempt-level retry
telemetry in the transport without selecting a metrics backend.

## Principal modes and sessions

The zero `AuthMode` preserves per-user behavior. Factories should set
`AuthModeService` for support/admin credentials, `AuthModeBrowser` for cookie
sessions, and `AuthModeNone` only for deliberate reachability/CSRF operations.
Collector registration returns a `CollectorRegistration.Identity()`; pass that
identity to `Performance.UploadWithIdentity` rather than reusing the API user's
credential.

CBR and legacy browser routes are exposed only by typed `Backups` and `Browser`
methods. `Raw` and public `NewRequest` remain restricted to `/api`, so adding
root-route coverage did not broaden arbitrary credential-bearing requests.

## Direct REST binding

Use `client.Raw` for a Forward endpoint outside the checked Skyforge inventory
while it is not yet exposed as a typed service:

```go
var result json.RawMessage
resp, err := client.Raw.DoJSON(ctx, "POST", "/api/new-release-feature",
	map[string]any{"enabled": true}, &result)
if err != nil {
	if forward.IsStatus(err, 404) {
		// Handle a missing resource.
	}
	return err
}
_ = resp
```

`Raw.Do` also accepts arbitrary headers and an `io.Reader`. The lower-level
`NewRequest` and `Do` remain public. All paths are restricted to `/api` on the
configured appliance so Basic credentials cannot be redirected to another
host.

## Stable and preview APIs

Published Forward OpenAPI descriptions define the stable surface. Endpoints
found only in application controllers or current consumers are marked
`Preview` in Go documentation because their routes, request bodies, feature
flags, and permissions can change between Forward releases.

| Service | Contract | Initial coverage |
| --- | --- | --- |
| `Version` | Published | appliance version |
| `Networks` | Published | list, create, update, delete |
| `Networks.CreateWorkspace` | Published | workspace networks and source selection |
| `Devices` | Published | modeled devices and streaming raw files |
| `ClassicDevices` | Published | collection-source CRUD with raw field preservation |
| `Credentials` | Mixed | typed CLI/HTTP CRUD; dynamic preview SNMP CRUD |
| `CollectorTasks` | Mixed | list, start, get, stop, progress, wait/callback |
| `CloudAccounts` | Preview | AWS-oriented CRUD plus dynamic provider fields |
| `Snapshots` | Mixed | tolerant list/resolve, subset ZIP export, multipart merge/upload, download |
| `Checks` | Preview | list/evaluate, persistent create, name lookup, non-vacuous scoring |
| `DeviceTags` | Preview | definition batch, assignment batch, list/expansion |
| `Performance` | Preview | typed metrics/history/health, synthetic generation, collector-authenticated binary upload |
| `NQE` | Published | sync queries, async executions, results/JSONL, diffs, query library |
| `Predict` | Preview | change sets, CLI and BGP drafts, predict, result history |
| `AI` | Preview | chat lifecycle, prompts, polling and messages |
| `AIAssist` | Preview | NQE, documentation, Predict and diff assists |
| `Properties` | Preview | current-org, supported-org and global properties |
| `Organizations` | Preview | current tenant plus support/admin lifecycle |
| `Admin`, `Users` | Preview | support user/network lifecycle and current-user tokens/password |
| `Collectors`, `Collections` | Preview | registration identity, attachment, settings, legacy collection/progress |
| `Endpoints`, `Locations`, `Proxies`, `Topology` | Preview | Skyforge source/site/proxy/topology contracts |
| `Backups`, `Browser` | Preview | root-scoped CBR and cookie/CSRF/session workflows |
| `Integrations`, `Configuration`, `Banners` | Preview | Infoblox/Rapid7/ServiceNow, deployment config, custom banners |
| `Webhooks` | Preview | outbound callback CRUD and connectivity tests |
| `Raw` | Any | forward-compatible access to every `/api` route |

Snapshot compatibility flags include `async`, `excludeFailedDevices`,
`skipSnapshotProcessing`, and `skipUnprocessed`. `ExtraQuery` is available on
snapshot list/upload options so a caller can use a version-specific flag before
the SDK grows a typed field. Typed fields take precedence over duplicate
`ExtraQuery` values.

```go
async := true
snapshots, _, err := client.Snapshots.List(ctx, networkID, forward.SnapshotListOptions{
	SkipUnprocessed: &async, // Preview
})

execution, _, err := client.NQE.Start(ctx, networkID, snapshotID,
	forward.NQEExecutionRequest{QueryID: "FQ_example"})

chat, _, err := client.AI.StartChat(ctx, networkID, snapshotID,
	"Which devices have critical configuration risks?") // Preview
```

AI APIs require the corresponding organization properties and a deployment
with AI capability. Predict and its assists likewise require their Predict
properties and normal network permissions. Property names are deliberately
dynamic because Forward versions expose different sets; pass the name returned
by that appliance as an `OrgProperty` value.

Primary and stable appliances can run different appserver builds. Bind a tested
profile rather than inferring feature support from a release string:

```go
client, err := forward.NewClient(forward.Config{
	BaseURL:   applianceURL,
	APIToken:  token,
	NetworkID: tenantNetworkID,
	Capabilities: forward.CapabilityProfile{
		Track: "stable",
		Build: expectedBuild,
		Features: map[forward.Capability]forward.CapabilitySupport{
			forward.CapabilityPredict: forward.CapabilitySupported,
			forward.CapabilityStructuredBGPAdvertisements: forward.CapabilityUnsupported,
		},
	},
})
```

Unlisted capabilities remain `unknown` and are attempted normally. Explicitly
unsupported capabilities fail before a request. Successful calls become runtime
evidence shared by every `ForNetwork` clone. `Version.Get` records build/release
for diagnostics; it does not guess feature support.

Checks attach to snapshots, not directly to networks. Use
`Checks.CreatePersistent` on a real snapshot to carry a corpus onto later
snapshots of that same network. Scoring code should use `Checks.ForScoring`; an
empty corpus returns `ErrNoChecks` instead of allowing a vacuous 0/0 pass. Pass
`CheckRequirements` with the gate's required names/patterns to reject a partial
corpus as `ErrCheckCorpusIncomplete` too.

Predict callers must use `Predict.StageBGPAdvertisement` for new BGP
originations. On affected builds, CLI `network <prefix>` becomes a CONFIG seed
that Predict silently discards. Forward Predict does not model withdrawal, and
the SDK deliberately exposes no withdrawal action.

## Design direction

The published OpenAPI descriptions under the Forward application source tree
are the schema and endpoint source of truth. The initial SDK keeps its transport
hand-written and small while typed services are added from those descriptions.
This lets consumers migrate incrementally without blocking on complete API
generation or forcing a generated transport abstraction on Terraform and
Encore.

Automatic retries are intentionally not part of the default client. Retrying a
POST, PATCH, or DELETE can duplicate a mutation. Applications that know their
idempotency and backoff requirements can supply an `http.Client` with an
appropriate transport.

## Development

```sh
go generate ./...
go test ./...
go vet ./...
go run ./cmd/skyforge-coverage \
  -audit /path/to/skyforge/docs/forward-api-sdk-migration-audit.md
```

The complete Forward controller surface is larger than Skyforge's inventory.
See `COVERAGE.md` for the machine-generated Skyforge matrix and
`docs/api-coverage.md` for broader SDK direction. The manifest's consumer-tree
fingerprint deliberately makes any Skyforge production Go change require a
reviewed refresh rather than allowing coverage drift to stay silent.
