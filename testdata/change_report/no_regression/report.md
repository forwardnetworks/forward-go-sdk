# Change report

Base `1001` → after `1002` (predicted), network `42`, change set `CS-7`.

**Verdict: no_regression**

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-5 | bgp state copied from base | NQE | PASS | SKIPPED | other |
| C-2 | stale BGP adverts | NQE | FAIL | PASS | fixed |

Regressed 0, fixed 1, added 0, removed 0, unchanged 1, other 1; not evaluated 0, skipped 1, unlicensed 0.

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

## Security rules

No changes.

## Vulnerabilities

Introduced 0, resolved 0, exposed devices -.

## Diff counts

No changes.

## Predict

Processing: PROCESSED.

Failures: none.

Modeling problems: none.

---

- Flows: not requested
- State: not requested
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
