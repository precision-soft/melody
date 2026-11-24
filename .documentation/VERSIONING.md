# Versioning and support policy

This document holds the full support policy that the root [`README.md`](../README.md) summarizes.

## Version lines

| Version line | Module path                           | Released              | Latest  | Status                                                                                                                         | Supported until                          |
|--------------|---------------------------------------|-----------------------|---------|--------------------------------------------------------------------------------------------------------------------------------|------------------------------------------|
| v4           | `github.com/precision-soft/melody/v4` | **Q1 2027** (planned) | —       | Planned. The deprecations v3 accumulates are removed there; no code exists yet                                                 | —                                        |
| **v3**       | `github.com/precision-soft/melody/v3` | 2026-03-08            | v3.14.6 | **Stable, actively maintained.** v3.14.0 is its last feature release; security fixes, patch-level defect fixes, and deprecations land here | Set the day v4 ships: 18 months after it |
| v2           | `github.com/precision-soft/melody/v2` | 2026-02-17            | v2.13.4 | Feature-frozen since 2026-08-18 (`v2.13.0`)                                                                                    | **2027-09-08**                           |
| v1           | `github.com/precision-soft/melody`    | 2026-01-17            | v1.19.4 | Feature-frozen since 2026-08-18 (`v1.19.0`)                                                                                    | **2027-08-17**                           |

## The rule

The current line receives new features, security fixes, and deprecations. The day a new major ships, the line it
replaces keeps receiving security fixes and patch-level defect fixes for **18 more months**, counted from the
successor's release date. From v4 on, the replaced line also stops receiving features that day. (v1 and v2 were frozen
later, on 2026-08-18, after their last minor releases.)

"Supported until" covers security fixes and patch-level defect fixes only. A feature-frozen line receives no new
capability for the rest of its window.

## How the dates are derived

Every end-of-support date in the table is the rule above applied to the release dates beside it:

- v1 was replaced by v2 on 2026-02-17, so it is supported through **2027-08-17**.
- v2 was replaced by v3 on 2026-03-08, so it is supported through **2027-09-08**.
- v3 has no end date because it is the current line. Its 18-month clock starts the day v4 ships.

Q1 2027 for v4 is a plan, not a commitment.

## Deprecations and the path to v4

Within v3, evolution follows the standard Go approach. APIs that need to change are first marked with a
`/* Deprecated: ... */` doc comment and kept working. v4 will be cut once enough breaking changes have accumulated.

Every deprecated symbol names its successor. An application that adopts those replacements while on v3 crosses
to v4 without changing its own code.

## Why three lines exist side by side

Earlier major versions introduced changes that were not backwards compatible, and each was maintained in parallel.

The lines are intentionally **self-contained duplicates** rather than shared code, so that every module and
integration binds to exactly one framework version. **This duplication is by design and is not to be consolidated.**

## Migrating off a frozen line

Start at the "Migrating to v3" section of the relevant upgrade guide:

- v1: [`.documentation/UPGRADE.md`](./UPGRADE.md)
- v2: [`v2/.documentation/UPGRADE.md`](../v2/.documentation/UPGRADE.md)

See also [`SECURITY.md`](../SECURITY.md) and [`CONTRIBUTING.md`](../CONTRIBUTING.md).
