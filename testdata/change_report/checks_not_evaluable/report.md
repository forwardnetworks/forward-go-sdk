# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: incomplete**

- check_not_evaluated (checks): C-2
- check_not_evaluated (checks): C-3
- check_regressed (checks): C-1

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-1 | app reaches db | Existential | PASS | FAIL | regressed |
| C-2 | needs a DAG | Existential | PASS | REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING | other |
| C-3 | not computed | Existential | PASS | NONE | other |
| C-5 | now evaluated | Existential | REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING | PASS | other |
| C-6 | was failing | Existential | FAIL | REQUIRES_ADDITIONAL_SNAPSHOT_PROCESSING | other |

Regressed 1, fixed 0, added 0, removed 0, unchanged 1, other 4; not evaluated 4, skipped 0, unlicensed 0.

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

- Flows: not requested
- State: not requested
- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
