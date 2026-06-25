# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.0.2] - 2026-06-25 - Floor Sub-Millisecond Set TTL

### Fixed

- `cache/backend.go` — `SetCtx`/`SetMultipleCtx` passed a positive sub-millisecond TTL straight to `PX`, which rueidis truncates to `PX 0` (Redis rejects it with `ERR invalid expire time`), so the value was never stored. A sub-millisecond TTL is now floored to one millisecond via `floorPositiveExpiry`, matching the `v2`/`v3` fix.
- `cache/backend.go` — `Backend.Close` closed the `rueidis.Client`, but that client is owned by the `Provider` (which closes it in `Provider.Close`) and is shared with the backend, so at shutdown the container closed the same client twice — once through `BackendService.Close` → `Backend.Close`, once through the provider. `Backend.Close` is now a no-op (`return nil`); the backend does not own the client. Ported from the `v2`/`v3` fix.

## [v1.0.1] - 2026-06-16 - Glob-Escape the Cache Clear Prefix

### Fixed

- `cache/backend.go` — `Clear`/`ClearByPrefix` now glob-escape the literal key prefix before appending the `*` wildcard for `SCAN MATCH`, so a prefix (or a `ClearByPrefix` argument) containing a glob metacharacter (`*`, `?`, `[`, `]`, `\`) no longer mismatches the literally-stored keys (silently skipping the delete) or over-matches siblings. Ported from the `v3` fix.

## [v1.0.0] - 2026-02-11 - Initial Release — Redis Client Integration

### Added

- `provider.go` — `rueidis.Provider` implementing Redis connection provider; `NewProvider(addressParamName, userParamName, passwordParamName)` reads credentials through Melody config; `NewProviderWithConfig()` variant accepting pre-built `ClientConfig` and `TimeoutConfig`
- `client_config.go` — `rueidis.ClientConfig` with `MaxConnPoolSize`, `MinIdleConnections`, `ReadBufferSize`, `WriteBufferSize`
- `timeout_config.go` — `rueidis.TimeoutConfig` with `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `connection_config.go` — `rueidis.ConnectionConfig` holding address, user, password; `SafeContext()` elides password from logs
- Builder methods: `Provider.WithClientConfig()`, `WithTimeoutConfig()`
- `cache/backend.go` — `cache.Backend` wrapper around `rueidis.Client` with `Get()`, `Set()`, `Delete()`, `Has()`, `ClearByPrefix()`, `Many()`, `SetMultiple()`, `DeleteMultiple()`, `Increment()`, `Decrement()`
- `cache/backend_service.go` — `cache.BackendService` wrapper; `WithContext()` binds a backend to a specific context; `BackendFromRuntime()` obtains a backend from the Melody runtime with bound context

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.2...HEAD

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.1...integrations/rueidis/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.0...integrations/rueidis/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v1.0.0
