# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: incomplete**

- check_regressed (checks): C-10
- flow_lost_valid_path (flows): f-lost
- flow_unavailable (flows): f-unav
- state_lost (state): bgp_session:core-2|default|198.51.100.1

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-10 | pass to error | Existential | PASS | ERROR | regressed |
| C-14 | fail to error | NQE | FAIL | ERROR | other |
| C-15 | was skipped | Existential | SKIPPED | FAIL | other |
| C-12 | dropped check | Existential | PASS | - | removed |
| C-13 | new failing check | Isolation | - | FAIL | added |
| C-11 | error recovered | Reachability | ERROR | PASS | fixed |

Regressed 1, fixed 1, added 1, removed 1, unchanged 0, other 2; not evaluated 0, skipped 0, unlicensed 0.

## Flows

| Flow | Expectation | Base valid | After valid | Change |
| --- | --- | --- | --- | --- |
| f-lost | valid | 2 | 0 | lost_valid |
| f-unav | valid | - | - | unavailable |
| f-gain | any | 0 | 3 | gained_valid |
| f-count | valid | 2 | 3 | counts_changed |
| f-same | valid | 2 | 2 | unchanged |

## State

| Kind | Identity | Base | After | Change | Expected |
| --- | --- | --- | --- | --- | --- |
| bgp_session | core-2, default, 198.51.100.1 | ESTABLISHED | IDLE | lost | no |
| acl_entries | edge-4, old-acl | 3 | - | changed | no |
| bgp_session | core-2, default, 198.51.100.2 | IDLE | ESTABLISHED | changed | no |
| interface_admin | edge-4, et1 | AdminStatus.DOWN | AdminStatus.UP | changed | no |
| lldp_adjacency | core-2, et2, edge-4, et50 | - | present | gained | no |

## Connectivity

Subnet pairs evaluated: 168 of 168.

| Change | From | To | Subnet pairs |
| --- | --- | --- | --- |
| DELETED | site-a | site-c | 1 |
| MODIFIED | site-b | site-c | 4 |
| ADDED | site-c | site-a | 2 |

## Routing

No changes.

## Config

No changes.

## Devices

Added: core-9.
Removed: edge-7.
Modified: edge-1, edge-2.

## Vulnerabilities

Introduced 0, resolved 0, exposed devices -.

## Diff counts

No changes.

---

- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
