# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.0.2] - 2026-06-24 - Guard openWithRetry Against a Nil Logger

### Fixed

- `provider.go` — `openWithRetry` called `logger.Info`/`Warning`/`Error` directly, so a direct `Provider.Open(params, nil)` call (a nil logger) with a retry config and a transient open failure panicked with a nil-pointer dereference. It now normalizes the logger via `logging.EnsureLogger`, matching the `v1`/`v3` providers.

## [v2.0.1] - 2026-06-16 - Honor Zero ConnectTimeout on the Connection Ping

### Fixed

- `v2/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a healthy database. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, back-porting the `v3` fix.

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Simplify Provider.Open Signature

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/mysql/v2` — Go v2 migration
- `v2/provider.go` — `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads config from resolver
- `v2/provider.go` — `NewProvider()` no longer accepts parameter names; takes `...ProviderOption` variadic args instead
- `v2/provider.go` — removed builder methods `WithPoolConfig()`, `WithTimeoutConfig()`, `WithRetryConfig()` — options now supplied through `ProviderOption`

### Changed

- Code moved to `integrations/bunorm/mysql/v2/` with matching module path
- Dependencies: `github.com/precision-soft/melody/integrations/bunorm/v2 v2.0.0`, `github.com/precision-soft/melody/v2 v2.0.0`

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v2.0.2...HEAD

[v2.0.2]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v2.0.1...integrations/bunorm/mysql/v2.0.2

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/mysql/v2.0.0...integrations/bunorm/mysql/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/mysql/v2.0.0
