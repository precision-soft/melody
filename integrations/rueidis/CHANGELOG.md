# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.0] - 2026-04-20 - Additive Ctx-First Cache Backend API

### Added

- `cache/backend.go` — additive ctx-first surface on `*Backend`: `GetCtx`, `SetCtx`, `DeleteCtx`, `HasCtx`, `ClearCtx`, `ClearByPrefixCtx`, `ManyCtx`, `SetMultipleCtx`, `DeleteMultipleCtx`, `IncrementCtx`, `DecrementCtx`. Each takes `ctx context.Context` as the first parameter so caller deadlines / cancellation propagate end-to-end. The legacy no-ctx methods now delegate to these (single implementation per operation) (MEL-165); mirrored in `v2/` and `v3/`
- `cache/backend_service.go` — `BackendService.Backend()` accessor exposes the underlying `*Backend` for callers that want to invoke the ctx-first surface directly without rebinding; mirrored in `v2/` and `v3/`
- `cache/backend_service_test.go` — reflection assertions that `Backend` retains its stored `ctx` field, that the eleven `*Ctx` methods exist with the agreed signatures, and that the legacy methods are preserved unchanged; compile-time `var _ func(...)` assertions pin both surfaces; mirrored in `v2/` and `v3/`

### Deprecated

- `cache.Backend` — ctx-less methods (`Get`, `Set`, `Delete`, `Has`, `Clear`, `ClearByPrefix`, `Many`, `SetMultiple`, `DeleteMultiple`, `Increment`, `Decrement`) are now marked `// Deprecated: prefer <NameCtx>, which takes ctx per call.` They continue to work by delegating to the `*Ctx` methods with the stored ctx, but new code should adopt the ctx-first API (MEL-165); mirrored in `v2/` and `v3/`

## [v3.0.1] - 2026-03-08 - Tidy v3 go.sum Dependencies

### Changed

- `v3/go.mod`, `v3/go.sum` — resolved transitive dependency checksums; no API changes

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration and ConnectionParams Rename

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/rueidis/v3` — Go v3 migration
- `rueidis.ConnectionConfig` renamed to `ConnectionParams`; now a value type (no longer pointer-based); `NewConnectionParams()` returns a value
- `provider.go` — `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (rueidis.Client, error)` to `Open(params ConnectionParams) (rueidis.Client, error)` — provider no longer couples to container/config resolution

### Changed

- Code duplicated into `integrations/rueidis/v3/`; v2 and v3 implementations maintained in parallel
- `v3/connection_params.go` — `ConnectionConfig` renamed to `ConnectionParams` with value semantics
- Dependencies pinned to `github.com/precision-soft/melody/v3 v3.0.0`

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/rueidis/v2` — Go v2 migration

### Changed

- `v2/go.mod` — code moved to `integrations/rueidis/v2/` with matching module path
- Local `replace` directive removed from `go.mod`; `github.com/precision-soft/melody` pinned to v1.6.3
- `v2/README.md` — documentation examples reformatted to be copy-paste runnable (wrapped in `main()` functions)
- `Provider.Open()` signature unchanged in v2 (still accepts `containercontract.Resolver`) — contrast with v3 where it changes

## [v1.0.0] - 2026-02-11 - Initial Release — Redis Client Integration

### Added

- `provider.go` — `rueidis.Provider` implementing Redis connection provider; `NewProvider(addressParamName, userParamName, passwordParamName)` reads credentials through Melody config; `NewProviderWithConfig()` variant accepting pre-built `ClientConfig` and `TimeoutConfig`
- `client_config.go` — `rueidis.ClientConfig` with `MaxConnPoolSize`, `MinIdleConnections`, `ReadBufferSize`, `WriteBufferSize`
- `timeout_config.go` — `rueidis.TimeoutConfig` with `ConnectTimeout`, `ReadTimeout`, `WriteTimeout`
- `connection_config.go` — `rueidis.ConnectionConfig` holding address, user, password; `SafeContext()` elides password from logs
- Builder methods: `Provider.WithClientConfig()`, `WithTimeoutConfig()`
- `cache/backend.go` — `cache.Backend` wrapper around `rueidis.Client` with `Get()`, `Set()`, `Delete()`, `Has()`, `ClearByPrefix()`, `Many()`, `SetMultiple()`, `DeleteMultiple()`, `Increment()`, `Decrement()`
- `cache/backend_service.go` — `cache.BackendService` wrapper; `WithContext()` binds a backend to a specific context; `BackendFromRuntime()` obtains a backend from the Melody runtime with bound context

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.0.1...integrations/rueidis/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.0.0...integrations/rueidis/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.0...integrations/rueidis/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.0...integrations/rueidis/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations%2Frueidis%2Fv1.0.0
