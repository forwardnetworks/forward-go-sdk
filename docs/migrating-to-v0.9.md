# Migrating to v0.9.0

v0.9.0 splits the SDK into hexagonal packages under the same module path. Calls read as before, for example
`client.Networks.List(ctx)` and `client.Diffs.MaterialSummary(...)`, and every request sends exactly what v0.8.59 sent. What
changes is where the types live. Most consumers need one command.

## Run the migrate tool

From your module root, with your `go.mod` requiring v0.9.0:

```sh
go run github.com/forwardnetworks/forward-go-sdk/cmd/migrate-v09@v0.9.0 .
```

It rewrites your Go files in place:

| Before | After |
|---|---|
| `forward.Snapshot`, `forward.ErrPermissionDenied` and the other value types and sentinel errors | `domain.Snapshot`, `domain.ErrPermissionDenied` |
| `*forward.ErrorResponse`, `forward.Response` and the other values that carry a transport type | `*ports.ErrorResponse`, `ports.Response` |
| `*forward.NetworksService` and every other `*forward.XService` | `*usecases.Networks`, `*usecases.X` |
| `github.com/forwardnetworks/forward-go-sdk/perfpayload` | `github.com/forwardnetworks/forward-go-sdk/domain/perfpayload` |
| `forward.Client`, `forward.Config`, `forward.NewClient`, `forward.NewUnavailable`, `forward.RetryPolicy`, `forward.Hook`, `forward.LoadAccessTable` | unchanged: the root package still declares them |
| `forward.AuthMode` and its constants, `forward.CapabilityProfile` | rewritten to `domain.AuthMode` and so on; the root keeps aliases, so leaving them as they were also compiles |

It adds the `domain`, `ports` and `usecases` imports a file needs, and drops the root import when nothing uses it any more. If a
file already has a package called `domain`, `ports` or `usecases` of its own, the SDK's comes in as `fwddomain`, `fwdports` or
`fwdusecases`, so your own package keeps its name. A name the tool does not know is reported with its `file:line`, left as it is,
and the run exits 1, so nothing changes without the tool saying so.

Flags:

- `-prefer-ports` writes every moved name as `ports.X`. Use it for code that should depend only on the SDK's contracts, such
  as adapters and fakes. A name `ports` does not re-export is reported.
- `-testdata` also rewrites files under `testdata/` that your tests compile. It is off by default, because a fixture may
  name an old identifier on purpose.

## The new layout

| Package | Holds |
|---|---|
| `forward` (the root) | `Client`, `Config`, the constructors, and the composition that wires the others |
| `domain` | the API's value types and the rules about them; no I/O |
| `ports` | one interface per API family (what a `Client` field sends), `Session`, and the transport-carrying values |
| `usecases` | one struct per family, embedding its port, plus the composites, `Poller` and the resumable operations |
| `internal/adapters/secondary/rest`, `.../accessfile` | the HTTP client and the access-table loaders; internal, not importable |

To fake one API family in a test, implement its port and embed the interface, so a method added in a later release does not
break your fake's build. A port may grow in any release.

## Behaviour changes

v0.9.0 changes behaviour in two places, and fixes one bug:

- **A fail-closed client fails first.** On a client from `NewUnavailable`, every method that returns an error now returns
  `*domain.UnavailableError` (carrying your cause) before any argument check. Before, some methods returned an argument error
  first, and `Diffs.MaterialSummary` returned no error at all.
- **Some methods on a nil service now panic.** A method that answered a nil service (for example a field of a zero-value
  `forward.Client{}`) only because its argument check ran before the nil receiver was dereferenced now panics, as a method on any
  nil struct pointer does. The deliberate nil checks, such as `"forward: admin service is nil"`, still answer exactly as before.
  Build clients with `NewClient` or `NewUnavailable`; a zero-value `forward.Client{}` literal was never supported, and its
  `ForNetwork`, `NewRequest` and `Do` now fail closed.
- **Fix: `CloudManagedSetups.ListMist` returns Mist setups only.** Forward's list route answers every cloud-managed setup type, so
  before, a Meraki or VeloCloud setup came back decoded as a Mist one.

`CoverageOperation` is no longer exported. It described the SDK's own coverage catalog, which only the SDK's tests read.

## New in v0.9.0

Meraki cloud-managed setups:

- `CloudManagedSetups.ListMeraki`, `GetMeraki`, `CreateMeraki`, `PatchMeraki`, `TestMeraki`, `DiscoverMeraki` and
  `DeleteMeraki`;
- `domain.NewMerakiSetup` with `DisablePrivateAPICollection`, for Forward 26.11's public-API-only collection;
- `domain.MerakiSetupPatch`, where an absent field is left unchanged and a Clear flag resets it.
