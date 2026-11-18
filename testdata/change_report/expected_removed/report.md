# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: regression**

- state_lost (state): interface_admin:edge-1|et3
- state_lost (state): lldp_adjacency:core-1|et1|edge-1|et49

## Checks

No changes.

Regressed 0, fixed 0, added 0, removed 0, unchanged 1, other 0; not evaluated 0, skipped 0, unlicensed 0.

## Flows

| Flow | Expectation | Base valid | After valid | Change |
| --- | --- | --- | --- | --- |
| guest to mgmt | none | 1 | 0 | lost_valid |
| app to db | valid | 4 | 2 | counts_changed |

## State

| Kind | Identity | Base | After | Change | Expected |
| --- | --- | --- | --- | --- | --- |
| bgp_session | core-1, default, 192.0.2.9 | ESTABLISHED | - | lost | yes |
| interface_admin | edge-1, et3 | AdminStatus.UP | AdminStatus.DOWN | lost | no |
| lldp_adjacency | core-1, et1, edge-1, et49 | present | - | lost | no |
| acl_entries | edge-1, edge-in | 12 | 13 | changed | no |

## Connectivity

No forwarding change.

No location pair changes.

## Routing

No changes.

## Config

No changes.

## Devices

Added: none.
Removed: none.
Modified: none.

## Vulnerabilities

Introduced 0, resolved 0, exposed devices -.

## Diff counts

No changes.

---

- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
