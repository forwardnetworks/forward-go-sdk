# Change report

Base `1001` → after `1002` (predicted), network `42`, change set `CS-9`.

**Verdict: no_regression**

## Checks

No changes.

Regressed 0, fixed 0, added 0, removed 0, unchanged 1, other 0; not evaluated 0, skipped 0, unlicensed 0.

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

| Device | Rule | Change |
| --- | --- | --- |
| fw-0 | old-temp | removed |
| fw-1 | allow-app | modified |
| fw-1 | deny \| all | added |

## Vulnerabilities

Introduced 0, resolved 0, exposed devices -.

## Diff counts

No changes.

## Predict

Processing: PROCESSED.

Failures: none.

Modeling problems:

| Device | Message |
| --- | --- |
| fw-1 | unsupported NAT rule kept as-is |

---

- Flows: not requested
- State: not requested
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
