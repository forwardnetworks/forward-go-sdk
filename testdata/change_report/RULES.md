# ChangeReport, schema_version "1": assembly and rendering rules

Shared by forward-python-sdk and forward-go-sdk. DRAFT for review. Both SDKs
implement the same PURE step, `assemble(input) -> report` and
`render_markdown(report) -> text`, and test it byte for byte against the
golden files in this directory. Reading the sections from Forward is per SDK
and tested separately. A report is a client-side composite of existing reads;
it never claims to match the Predict UI.

## Files

- `schema.json`: JSON Schema (2020-12) of `report.json`.
- `<case>/input.json`: the already-read section data, the options and the meta.
- `<case>/report.json`: the expected report (written once both SDKs agree).
- `<case>/report.md`: the expected Markdown (written once both SDKs agree).
- `MANIFEST.sha256`: SHA-256 of every file above, pinned in both repos and bumped together.

All fixture data is synthetic: documentation-range addresses (192.0.2.0/24,
198.51.100.0/24, 203.0.113.0/24), invented device and check names, no customer
or lab names.

## Input

`input.json` has `meta`, `options` and `sections`. Each section is either the
read data (`{"status": "ok", ...}`) or what happened when it could not be read
(`{"status": "unavailable", "reason": {"kind", "message"}}`). A section the
caller did not opt into is absent from `sections`. `options.created_at` pins
`meta.created_at` so output is deterministic.

## Section status

Every report section has a `status`:

| status | meaning |
| --- | --- |
| `ok` | read and assembled |
| `unavailable` | could not be read; `reason.kind` is one of `forbidden`, `not_served`, `feature_gated`, `not_ready`, `timeout`, `error` |
| `not_applicable` | `predict` and `security_rules` when `after.kind` is `collected` |
| `not_requested` | an opt-in section (`state`, `flows`, `ai_summary`) the caller did not ask for |

## Sections

| section | drives the verdict | opt-in |
| --- | --- | --- |
| `checks` | yes | no |
| `connectivity` | no (reported) | no |
| `routing` | no | no |
| `config` | no | no |
| `diff_counts` | no | no |
| `devices` | no | no |
| `vulnerabilities` | no | no |
| `state` | yes, when requested | yes |
| `flows` | yes, when requested | yes |
| `predict` | no | no (predicted only) |
| `security_rules` | no | no (predicted only) |
| `ai_summary` | never | yes, default off |

## Verdict

`verdict.result` is one of three values, decided in this order:

1. `incomplete` if any of:
   - a verdict-driving section that applies is `unavailable` (checks always;
     state and flows when requested): `{code: "section_unavailable", section,
     detail: <reason.kind>}`;
   - a check goes from `PASS` to `TIMEOUT`, `REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING`
     or `NONE` (it could not be evaluated on after): `check_not_evaluated`,
     detail = check id;
   - a requested flow's read is unavailable (`flow_unavailable`, detail = flow name).
   A refused or unfinished read never yields `no_regression`.
2. `regression` if any of:
   - a check goes `PASS` → `FAIL` or `PASS` → `ERROR` (Forward's own failure
     set, `DiffHelper.FAILURE_CHECK_STATUSES`; `check_regressed`, detail = check id);
   - a requested flow had at least one `VALID` path on base and none on after,
     when its expectation is `"valid"` (`flow_lost_valid_path`, detail = flow name).
     A flow's `expectation` is `"valid"` (it must keep a path), `"none"` (it is
     meant to lose its paths, as for an isolation intent) or `"any"` (report it,
     never let it drive the verdict); only `"valid"` can regress;
   - a requested state identity is lost, unless listed in
     `options.expectations.state_expected_removed`
     (`state_lost`, detail = `<kind>:<identity>`). Lost means: a
     `bgp_session` that was `ESTABLISHED` on base and is absent or not
     `ESTABLISHED` on after; an `lldp_adjacency` present on base and absent on
     after; an `interface_admin` that was `AdminStatus.UP` on base and is
     absent or anything else on after. An `acl_entries` count that changes is
     reported as `changed` and never counts as lost.
3. Otherwise `no_regression`.

A section that does not drive the verdict never changes it, even when
`unavailable`. `<identity>` is the identity tuple joined with `|`, so an
expectation reads `bgp_session:core-1|default|192.0.2.9`. A state item is
`gained` when present only on after. Its `expected` flag is true when its
`<kind>:<identity>` is in `state_expected_removed`. Unchanged state items
are not listed. Classification is in this order: `lost` (the per-kind rule
above), else `gained` (absent on base), else `changed` (any other difference,
for any kind: a value change that is not lost, such as a `bgp_session` going
`IDLE` → `ESTABLISHED` or an interface `DOWN` → `UP`, or an item absent on after
that the lost rule does not cover, such as an `acl_entries` row).

Rules 1 and 2 both add reasons when both apply; the result is `incomplete`.
Reasons are sorted by (section, code, detail).

Counts that change (paths, connectivity pairs, diff families, vulnerabilities)
are reported and never drive the verdict in schema_version 1. A diff family
with `complete: false` is a lower bound and is reported as such.

## Input sections

Each section's input is its report form without the derived fields:
`checks.items` without `change` and without the `counts`; `state` as `base`
and `after` lists of `{kind, identity, value}`; `flows.items` as
`{name, expectation, base, after}` without `change`, or, for a flow whose read
was unavailable, `{name, expectation, unavailable: {kind, message}}` with no
`base` or `after`. `expectation` is required and is one of `valid`, `none`,
`any`. `ai_summary.text` must be non-empty and must not end with a newline.

Input errors, in addition to those below: a duplicate identity anywhere
(two checks with one `check_id`, two flows with one name, two state rows with
one (`kind`, identity) on the same side, two security rules with one (`device`,
`rule`), two location pairs with one (`src`, `dst`) in the same list, a device
twice in one `devices` list, two routing or config rows for one device); a
missing or unknown `expectation`; an empty `ai_summary.text` or one ending in a
newline. No list section has
`truncated` or `total` in the input. A section absent from the input is
`not_requested` when opt-in and `not_applicable` when predicted-only on a
collected `after`; any other absent section is an input error. So is a
predicted-only section (`predict`, `security_rules`) present for a collected
`after`, or missing for a predicted one.

## Checks

Forward's `CheckStatus` has nine values: `NONE`, `PASS`, `FAIL`, `ERROR`,
`TIMEOUT`, `UNLICENSED`, `SKIPPED`, `PROCESSING`,
`REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING`. Only `PROCESSING` is still being
computed: the reading SDK re-reads while a row is `PROCESSING`, and one still
`PROCESSING` at the deadline makes the whole section `unavailable` (`timeout`).
`assemble` treats `PROCESSING` in its input as an input error.
`REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING` does not clear by waiting (Forward
evaluates such a check only after advanced reachability runs, which by default
it does only when a user triggers it, and a predicted snapshot skips it), and
`NONE` is Forward's "not computed" placeholder: both are judged per check, not
per section.

Joined by check id, never by name. Persistent checks keep their id across
snapshots; a non-persistent check gets a new id per snapshot and so appears as
a `removed` and `added` pair. The reader uses the per-type check diff, whose
rows carry both statuses. `change` is `regressed` (PASS → FAIL or ERROR),
`fixed` (FAIL or ERROR → PASS), `added` (after only), `removed` (base only),
`unchanged`, or `other` (any other pair). `counts` adds `not_evaluated`
(after status `TIMEOUT`, `REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING` or `NONE`), `skipped` (`SKIPPED`: a config-only predicted
snapshot skips checks that read data copied from its base, by design) and
`unlicensed` (`UNLICENSED`); the last two never drive the verdict, and the
Markdown shows their counts. `counts.not_evaluated` counts every row whose
AFTER status is `TIMEOUT`, `REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING` or `NONE`,
whatever its base; the verdict reason `check_not_evaluated` fires only when the
base was `PASS`. A row with the same not-evaluated status on both sides is
`unchanged`, counted, and gives no reason. Do not derive one from
the other. `counts` covers every row, including rows past the cap.

Only a `PASS` can regress, a deliberate limit of schema_version 1: a check whose
base was `TIMEOUT`, `SKIPPED`, `UNLICENSED`, `ERROR` or `FAIL` and is `FAIL` or
`ERROR` on after is `other` and does not drive the verdict.

## Flows

Path outcomes are Forward's `FlowType`, bucketed: `valid` = VALID, `dropped` =
DROPPED, `blackhole` = BLACKHOLE, `unreachable` = UNREACHABLE, `loop` = LOOP,
`other` = INADMISSIBLE + POTENTIAL_LOOP + IGNORED (the last two are hidden from a
default search, so normally 0). A flow's read counts only when the facets read
finished: an answer with `timedOut` true, or `totalResults` of type
`LOWER_BOUND`, makes that flow `unavailable` (kind `timeout`) rather than "no
VALID path"; its `base` and `after` are null and its `change` is `unavailable`.

## Connectivity

From the subnet-connectivity summary once it has settled (`isPartialResult`
false); a summary still partial at the deadline makes the section `unavailable`
(`timeout`), and the location pairs are read per diff type only after the wait.
`no_forwarding_change` is true when the settled `total_subnet_pairs` is 0: Forward compares only subnet pairs a route, ACL
or NAT change could affect, so zero means no forwarding change (the degenerate
case is a network with no device subnets). Location pairs are listed per diff
type (`ADDED`, `DELETED`, `MODIFIED`).

## Ordering and caps

Lists are always present (never null). `checks`, `flows` and `state` are
ordered by severity first, so that a cap can only ever drop their least severe
rows: checks by change (`regressed`, `other`, `removed`, `added`, `fixed`,
`unchanged`) then `check_id`; flows by change (`lost_valid`, `unavailable`,
`gained_valid`, `counts_changed`, `unchanged`) then name; state by change
(`lost`, `changed`, `gained`) then `kind`, then identity. Every other list is
ordered by identity: routing and config by `device`; device names in `devices`
alphabetically; location pairs by (`src`, `dst`); security rules by
(`device`, `rule`).

Sorting is bytewise on the strings (UTF-8 bytes), and an identity sorts as a
tuple, element by element, never as its joined string: (`a`, `zz`) comes
before (`ab`, `c`). `config.items[].files_changed` is sorted and never capped.
The `predict` lists (`failures`, `modeling_problems`) keep input order and are
never capped.

Every list is capped at `options.max_items` (default 100), keeping the first
items in that order. A section's `total` is the number of items before capping
and `truncated` is whether anything was dropped. Where a section holds several
lists (`devices`: added, removed, modified; `connectivity`: the three location
pair lists), each list is capped separately and `total` is their combined
length before capping. Counts (`checks.counts`) and the verdict are computed
over every item, whether or not it survives the cap.

## Markdown

The renderer reads only `report.json`. Lines end with `\n`, blocks are separated
by one blank line, and the file ends with exactly one `\n`. A block is a heading,
a paragraph line, a table, a bullet list or the `---` rule; the three Devices
lines are one block. A verdict with no reasons has no bullet list. Numbers are plain
integers. Inside a table cell, `|` is written `\|`. `null` is written `-`.

1. `# Change report`
2. ``Base `<base>` → after `<after>` (<after_kind>), network `<network_id>`.``
   With a change set, `, change set `<id>`` goes before the final full stop.
3. `**Verdict: <result>**`, then one bullet per reason, in report order:
   `- <code> (<section>): <detail>`.
4. One `## <Title>` block per section whose status is `ok`, in this order and
   with these titles: checks `Checks`, flows `Flows`, state `State`,
   connectivity `Connectivity`, routing `Routing`, config `Config`, devices
   `Devices`, security_rules `Security rules`, vulnerabilities
   `Vulnerabilities`, diff_counts `Diff counts`, predict `Predict`, ai_summary
   `AI summary`. Sections that are not `ok` appear only in the footer.
5. A truncated section ends with the line `Showing <shown> of <total>.`, where
   `<shown>` is the number of items in the report.
6. Footer: a line `---`, then one bullet per section that is not `ok`, in the
   order of step 4: `- <Title>: unavailable (<kind>: <message>)`,
   `- <Title>: not applicable`, or `- <Title>: not requested`. Then the line
   `Built from Forward's API by <sdk name> <sdk version>; not the Predict UI's report.`

Section bodies (a table is a header row, a `| --- |` row with one cell per
column, then the rows):

- **Checks**: a table `| Check | Name | Type | Base | After | Change |` of the
  rows whose `change` is not `unchanged`, in report order; when there are none,
  the line `No changes.` Then always the line
  `Regressed <n>, fixed <n>, added <n>, removed <n>, unchanged <n>, other <n>; not evaluated <n>, skipped <n>, unlicensed <n>.`
- **Flows**: no items: `No changes.` Otherwise
  `| Flow | Expectation | Base valid | After valid | Change |`, every
  flow in report order. Valid counts are
  `base.valid`/`after.valid`, `-` when null.
- **State**: `| Kind | Identity | Base | After | Change | Expected |`, in report
  order. Identity is
  the tuple joined with `, `; Expected is `yes` or `no`. No items: `No changes.`
- **Connectivity**: `No forwarding change.` when `no_forwarding_change`, else
  `Subnet pairs evaluated: <evaluated> of <total>.` Then a table
  `| Change | From | To | Subnet pairs |` of the location pairs, `DELETED` first,
  then `MODIFIED`, then `ADDED`, each in sort order; no pairs:
  `No location pair changes.`
- **Routing**: `| Device | Added | Removed | Modified |`; no items: `No changes.`
- **Config**: `| Device | Files changed |`, files joined with `, `; no items:
  `No changes.`
- **Devices**: three lines, `Added: <names>.`, `Removed: <names>.`,
  `Modified: <names>.`, names joined with `, `, or `none`.
- **Security rules**: `| Device | Rule | Change |`; no items: `No changes.`
- **Vulnerabilities**: `Introduced <n>, resolved <n>, exposed devices <n or ->.`
- **Diff counts**: `| Family | Count | Complete |`, families sorted by name,
  Complete `yes` or `no (lower bound)`; no families: `No changes.`
- **Predict**: `Processing: <processing_state>.`, then `Failures: none.` or the
  line `Failures:` and a table `| Device | Message |`, then likewise
  `Modeling problems: none.` or `Modeling problems:` and a table.
- **AI summary**: the text as given.

## Section contents from Forward's reads

- `routing`: the l3 diff's per-device stats, `ADDED`/`DELETED`/`MODIFIED` →
  `added`/`removed`/`modified`.
- `config`: the files diff, devices whose `CONFIG` files changed and the file
  names. schema_version 1 has no line counts: Forward gives none, and computing
  them client-side would let the two SDKs diverge.
- `diff_counts`: Forward's per-family counts `{count, complete}`, families
  `devices`, `interfaces`, `topology`, `l2`, `acl`, `cloud_objects`,
  `cloud_acl`, `routing_loop`. Route changes are in `routing`, not here.
- `vulnerabilities`: `introduced` (new CVEs, the bidirectional warnings),
  `resolved` (improvements), and `exposed_devices`, null where not licensed.

## Reads

How each SDK fills the input from Forward, so both feed `assemble` the same
data. Each section has its own time budget (default five minutes, settable); a
read that overruns it makes the section `unavailable` (`timeout`), never
partial. A 403 for a gating property is `feature_gated`, another 403
`forbidden`, a 404 "No endpoint" `not_served`, anything else `error`, except
where a section says otherwise.

- **checks**: for each check type (`Existential`, `Isolation`, `Reachability`,
  `QueryStringBased`, `Predefined`, `NQE`), `GET /diffs/{base}/{after}/checks?type=`,
  re-read while any row is `PROCESSING`. Each row gives `check_id` (the id of
  its `a` or `b` side), `name`, `type` (the type asked for), `base_status` (`a`'s
  status, null without `a`) and `after_status` (`b`'s). `UNCHANGED` rows are
  included. A type answering 404 "No endpoint" contributes no rows; any other
  failure on any type makes the section unavailable. Only `PROCESSING` rows are
  waited for; one still `PROCESSING` at the deadline makes the section
  unavailable (`timeout`). `REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING` and `NONE`
  rows are passed on as they are.
- **connectivity**: wait for the subnet-connectivity summary to settle
  (`isPartialResult` false; overrun = `timeout`), then read the location pairs
  with `?view=locationPairs&type=` for `ADDED`, `DELETED` and `MODIFIED`.
  `total_subnet_pairs` and `evaluated_subnet_pairs` come from the summary.
- **routing**: the l3 diff per device (`ADDED`/`DELETED`/`MODIFIED` stats);
  only devices with a nonzero count.
- **config**: the files diff for `CONFIG` files; `files_changed` holds the file
  name without its `<device>,` prefix.
- **devices**: the devices diff; `ADDED` → added, `DELETED` → removed,
  `MODIFIED` → modified, `UNCHANGED` dropped.
- **diff_counts**: each family's count read (`devices`, `interfaces`,
  `topology`, `l2`, `acl`, `cloud-objects`, `cloud-acl`, `routing-loop`) as
  `{count, complete}`. A family answering 404 "No endpoint" is omitted; any
  other failure makes the section unavailable.
- **vulnerabilities**: `introduced` = the bidirectional read's warnings CVE
  count, `resolved` = its improvements CVE count, `exposed_devices` = the
  warnings' exposed vulnerable device count, null when absent.
- **state** (opt-in): the four NQE state reads (BGP sessions, LLDP adjacencies,
  interface admin state, ACL entries; the same queries in both SDKs) on both
  snapshots. Identities: `bgp_session` (Device, VRF, NeighborIP) with the
  session state; `lldp_adjacency` (Device, LocalIntf, RemoteSys, RemotePort)
  with value `present`; `interface_admin` (Device, Interface) with the admin
  status text; `acl_entries` (Device, AclName) with the entry count. Any read
  failing makes the section unavailable.
- **flows** (opt-in): for each caller-supplied flow (name, expectation, path
  query), the trace facets on both snapshots. A bucket's count is the number of
  distinct paths in that `FlowType` group (`pathGroups[].paths`), not
  `totalResults`. A facets answer with `timedOut` true or `totalResults` of type
  `LOWER_BOUND` on either side makes that flow `unavailable` (`timeout`).
- **predict** (predicted only): `processing_state` is the predicted snapshot's
  state. `failures` is one row `{device: "-", message: "snapshot processing
  ended <STATE>"}` when that state is `FAILED`, `CANCELED`, `TIMED_OUT` or
  `RESTORE_FAILED`, else empty. `modeling_problems` is always empty in
  schema_version 1: no read gives modeling diagnostics cleanly.
- **security_rules** (predicted only): the change set's devices come from its
  draft, else its latest commit (both carry the per-device changes; failing to
  read them makes the section unavailable). For each device,
  `GET /networks/{n}/change-sets/{cs}/devices/{device}/security-rules-diff`,
  which Forward computes for the calling user's draft or latest commit. Entries
  `ADDED`/`DELETED`/`MODIFIED` give `{device, rule, change}` (`DELETED` →
  removed); `UNCHANGED` entries are dropped. `rule` is
  `<scopeId>/<rulebaseId>/<name>` from the entry's `b`, else its `a`. A 400
  "Device is not supported for firewall predict" means no rows for that device;
  a 404 "No endpoint" makes the section `not_served`; a 403 on
  `FIREWALL_PREDICT` `feature_gated`; anything else unavailable with its kind.
  Without a change set id for a predicted `after` the section is unavailable
  (`error`, "change set not given").
- **meta**: `after_kind` is `predicted` for a predicted snapshot, else
  `collected`, unless the caller says otherwise.

## JSON

`report.json` is UTF-8 with keys sorted, two-space indentation, `": "` between
key and value, no trailing spaces and one final newline. Strings use only the
escapes JSON requires (`\"`, `\\`, `\b`, `\f`, `\n`, `\r`, `\t`, and other control
characters as lowercase `\u00xx`); non-ASCII text is written as is, never as
`\u` escapes, and nothing is HTML-escaped.

## MANIFEST.sha256

`sha256sum`-compatible lines, `<hex>  <path>` (two spaces), paths relative to
this directory, sorted bytewise, covering `RULES.md`, `schema.json` and every
`<case>/input.json`, `report.json` and `report.md`. Both repos keep a
byte-identical copy and verify it with `sha256sum -c`.

