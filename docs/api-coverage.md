# API coverage

The goal is one canonical Go client for the complete Forward API. “Published”
means the route is present in `fwd/api/apis`; “preview” means it was sourced
from application controllers or an existing consumer and may vary by release.
All untyped routes remain callable through `Client.Raw`.

The audited consumer inventory is separately complete: see
[`../COVERAGE.md`](../COVERAGE.md) and `coverage_manifest.json`. That narrower
claim is enforced by tests and must not be confused with complete coverage of
every controller shipped by every Forward release.

## Typed now

| Domain | Contract | Coverage |
| --- | --- | --- |
| Version and networks | Published | version; network CRUD; workspace creation; scoped clients |
| Modeled devices | Published | list/get; collected-file list/download |
| Classic devices | Published | core CRUD; arbitrary request/response fields retained |
| Credentials | Mixed | CLI/HTTP CRUD; SNMP preview CRUD |
| Collection | Mixed | task list/start/get/stop; active progress; typed conflict; resumable task-to-snapshot operation |
| Cloud accounts | Preview | list/create/update/credential/delete; AWS external ID |
| Snapshots | Mixed | tolerant list/resolve; lifecycle CRUD/import; subset export and multipart merge; resumable upload |
| Checks | Preview | snapshot list/evaluate; persistent create; name index; non-empty scoring guard |
| Device tags/performance | Preview | tag definitions/assignments; metrics/history/health; synthetic generation; collector-authenticated binary upload |
| NQE | Published | sync, async, wait, results/JSONL, diff, query listing |
| Predict | Preview | change sets, normalized CLI/structured-BGP drafts, execution/history/wait; no withdrawal |
| AI and AI Assist | Preview | chats/messages/wait; NQE/docs/Predict/diff assists |
| Organizations/properties | Preview | current/admin org lifecycle; dynamic property keys |
| Webhooks | Preview | list/create/update/delete/test |
| Preview families | Preview | collectors/attachment, endpoints/profiles, locations/clusters/atlas, proxies, topology, users/admin, CBR, browser sessions, deployment config, banners, NQE repository mutation, Infoblox/Rapid7/ServiceNow |
| Compatibility | SDK | version diagnostics; explicit track/build profiles; tri-state capability support |

## Next typed migrations

The published OpenAPI families still needing dedicated services include:

- additional classic-device aliases, data connectors, and collection schedules
  not exercised by the audited consumer;
- snapshot metrics, topology/overrides, advanced reachability, path
  search, vulnerability analysis, L2/L3 VPNs, WAN circuits, endpoints, and
  internet/intranet nodes;
- complete-seed, encryptors, and additional source families.

Known preview/internal migrations beyond the checked subset include
cloud-provider-specific models, vCenter/NSX, collector setup packages,
collection logs/history, network membership/roles, Verify, NQE parameters/tags,
AI feedback, and the full Predict draft model.

Typed request structs use pointers or patch maps when omission differs from
false/zero/null. Dynamic/versioned fields are not closed enums: they use string
keys, raw JSON, `Fields`, `ExtraQuery`, or the `Raw` service.
