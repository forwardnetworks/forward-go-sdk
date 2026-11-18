# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: regression**

- check_regressed (checks): C-3

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-3 | app reaches db | Existential | PASS | FAIL | regressed |
| C-4 | new path check | Existential | - | PASS | added |
| C-2 | stale BGP adverts | NQE | FAIL | PASS | fixed |

Regressed 1, fixed 1, added 1, removed 0, unchanged 1, other 0; not evaluated 0, skipped 0, unlicensed 0.

## Connectivity

Subnet pairs evaluated: 42 of 42.

| Change | From | To | Subnet pairs |
| --- | --- | --- | --- |
| DELETED | site-a | site-b | 5 |
| DELETED | site-b | site-a | 3 |

## Routing

| Device | Added | Removed | Modified |
| --- | --- | --- | --- |
| edge-1 | 1 | 0 | 2 |
| edge-2 | 0 | 6 | 0 |

## Config

| Device | Files changed |
| --- | --- |
| edge-2 | running_config.txt |

## Devices

Added: none.
Removed: none.
Modified: edge-1, edge-2.

## Vulnerabilities

Introduced 0, resolved 0, exposed devices 0.

## Diff counts

| Family | Count | Complete |
| --- | --- | --- |
| devices | 0 | yes |
| interfaces | 1 | yes |
| topology | 2 | no (lower bound) |

---

- Flows: not requested
- State: not requested
- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
