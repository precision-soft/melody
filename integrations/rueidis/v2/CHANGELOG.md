# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.0.1] - 2026-06-16 - Glob-Escape the Cache Clear Prefix

### Fixed

- `v2/cache/backend.go` — `Clear`/`ClearByPrefix` now glob-escape the literal key prefix before appending the `*` wildcard for `SCAN MATCH`, so a prefix (or a `ClearByPrefix` argument) containing a glob metacharacter (`*`, `?`, `[`, `]`, `\`) no longer mismatches the literally-stored keys (silently skipping the delete) or over-matches siblings. Ported from the `v3` fix.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/rueidis/v2` — Go v2 migration

### Changed

- `v2/go.mod` — code moved to `integrations/rueidis/v2/` with matching module path
- Local `replace` directive removed from `go.mod`; `github.com/precision-soft/melody` pinned to v1.6.3
- `v2/README.md` — documentation examples reformatted to be copy-paste runnable (wrapped in `main()` functions)
- `Provider.Open()` signature unchanged in v2 (still accepts `containercontract.Resolver`) — contrast with v3 where it changes

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.1...HEAD

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.0...integrations/rueidis/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v2.0.0
