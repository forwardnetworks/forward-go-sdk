# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: incomplete**

- check_not_evaluated (checks): C-6
- flow_unavailable (flows): bulk search

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-6 | long path search | Existential | PASS | TIMEOUT | other |
| C-7 | licensed check | Predefined | PASS | UNLICENSED | other |

Regressed 0, fixed 0, added 0, removed 0, unchanged 1, other 2; not evaluated 1, skipped 0, unlicensed 1.

## Flows

| Flow | Expectation | Base valid | After valid | Change |
| --- | --- | --- | --- | --- |
| bulk search | valid | - | - | unavailable |
| app to db | valid | 3 | 3 | unchanged |

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

- State: not requested
- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
