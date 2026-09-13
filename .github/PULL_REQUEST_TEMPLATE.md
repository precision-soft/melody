# Summary

What does this change do, and why?

# Version line

- [ ] v3 (new features and fixes)
- [ ] v2 (patch-level defect / security fix only)
- [ ] v1 (patch-level defect / security fix only)

New features go to **v3 only**. Back-port a fix to v1/v2 only when the defect is observable there and the fix fits a patch release — no new public symbols, no signature changes, nothing breaking. See [`CONTRIBUTING.md`](../CONTRIBUTING.md#versioning-and-where-to-make-changes).

# Checklist

- [ ] Change is scoped to one logical change-set.
- [ ] Tests added or updated for behavioral changes.
- [ ] Verified under the full build-tag matrix (see [`CONTRIBUTING.md`](../CONTRIBUTING.md#build-tags-and-verification-matrix)).
- [ ] Documentation updated where behavior, invariants, or public APIs changed.
- [ ] `CHANGELOG.md` updated for each affected version line.
- [ ] I did not attempt to consolidate or de-duplicate the v1/v2/v3 module lines.
