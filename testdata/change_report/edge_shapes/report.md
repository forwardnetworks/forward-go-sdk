# Change report

Base `1001` → after `1002` (predicted), network `42`, change set `CS-11`.

**Verdict: no_regression**

## Checks

No changes.

Regressed 0, fixed 0, added 0, removed 0, unchanged 1, other 0; not evaluated 0, skipped 0, unlicensed 0.

## Flows

No changes.

## State

No changes.

## Connectivity

No forwarding change.

No location pair changes.

## Routing

No changes.

## Config

| Device | Files changed |
| --- | --- |
| edge-1 | a_config.txt, z_routes.txt |

## Devices

Added: none.
Removed: none.
Modified: edge-1.

## Security rules

No changes.

## Vulnerabilities

Introduced 1, resolved 2, exposed devices 3.

## Diff counts

| Family | Count | Complete |
| --- | --- | --- |
| acl | 0 | yes |

## Predict

Processing: PROCESSED.

Failures:

| Device | Message |
| --- | --- |
| edge-1 | zeta step — skipped |
| edge-0 | alpha	step failed |

Modeling problems: none.

## AI summary

One device changed — nothing regressed.

---

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
