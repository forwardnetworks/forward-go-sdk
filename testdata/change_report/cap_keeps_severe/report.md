# Change report

Base `1001` → after `1002` (collected), network `42`.

**Verdict: regression**

- check_regressed (checks): C-9

## Checks

| Check | Name | Type | Base | After | Change |
| --- | --- | --- | --- | --- | --- |
| C-9 | last id, regressed | Existential | PASS | FAIL | regressed |

Regressed 1, fixed 0, added 0, removed 0, unchanged 2, other 0; not evaluated 0, skipped 0, unlicensed 0.

Showing 2 of 3.

## State

| Kind | Identity | Base | After | Change | Expected |
| --- | --- | --- | --- | --- | --- |
| acl_entries | a, zz | 1 | 2 | changed | no |
| acl_entries | ab, c | 1 | 2 | changed | no |

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
- Security rules: not applicable
- Predict: not applicable
- AI summary: not requested

Built from Forward's API by forward-sdk 0.0.0-fixture; not the Predict UI's report.
