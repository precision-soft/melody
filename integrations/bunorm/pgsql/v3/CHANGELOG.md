# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.1] - 2026-06-15 - Honor Zero ConnectTimeout on the Connection Ping

### Added

- `v3/README.md` — added a v3 module README documenting the option-based `Provider`, the secure-by-default TLS controls (`WithInsecure`/`WithTlsConfig`), the typed pool/timeout/retry configs, and the post-build hook; also documented that the package ships no self-registering application module (provider-only — PostgreSQL exposes no application-level service, unlike the MySQL advisory-lock module), so it is registered through the core registry.

### Changed

- `v3/provider.go` — the retry/backoff fallbacks in `openWithRetry`/`computeBackoffDelay` now read from `DefaultRetryConfig()` instead of repeating the `3` / `500ms` / `5s` / `2.0` literals inline, so the documented defaults and the zero-value fallbacks cannot drift apart. Behaviour is unchanged.

### Fixed

- `v3/provider.go` — `Open` no longer fails the connection ping when `ConnectTimeout` is `0`. The ping context was built unconditionally with `context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)`, so a configured zero timeout (`WithTimeoutConfig(NewTimeoutConfig(0))`, which the framework treats elsewhere — and in the same function's post-build-hook block — as "no deadline / wait indefinitely") produced an already-expired context and `PingContext` returned `context.DeadlineExceeded` against a perfectly healthy database, surfacing as `"database connection failed"`. The ping context is now guarded with `if 0 < timeoutConfig.ConnectTimeout`, mirroring the bunorm `mysql/v3` provider.
- `v3/provider.go` — `openWithRetry` no longer panics when `Open` is called with a `nil` logger and a `RetryConfig`. The retry path called `logger.Info`/`Warning`/`Error` directly, so a transient connection error dereferenced the nil logger; the logger is now normalized through `logging.EnsureLogger`, matching the framework's nil-logger contract that the non-retry path (and the example wiring) already rely on.

## [v3.1.0] - 2026-04-23 - Default TLS Handshake (MEL-161)

### Fixed

- `provider.go` — default TLS handshake is now enabled. The legacy hardcoded `pgdriver.WithInsecure(true)` silently disabled TLS on every Postgres connection; `insecure` now defaults to `false`, so new `NewProvider(...)` callers negotiate TLS out of the box. Operators who still rely on plain-TCP can opt in with `WithInsecure(true)`. This is a **behavioural change**: deployments without a TLS-capable Postgres endpoint must either expose TLS on the server or explicitly pass `WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

### Changed

- `provider.go` — `Open(...)` now builds the `pgdriver` connector from `instance.insecure` / `instance.tlsConfig` instead of hardcoding `pgdriver.WithInsecure(true)` (MEL-161); mirrored in `v2/` and `v3/`

### Added

- `provider_option.go` — `WithInsecure(insecure bool) ProviderOption` lets callers toggle the `pgdriver.WithInsecure(...)` flag (default `false`) (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option.go` — `WithTlsConfig(config *tls.Config) ProviderOption` lets callers pass a `*crypto/tls.Config` that is forwarded to `pgdriver.WithTLSConfig(...)`. When a non-nil `tls.Config` is supplied, it takes precedence over `WithInsecure(...)` (MEL-161); mirrored in `v2/` and `v3/`
- `provider_option_test.go` — coverage for default (`insecure=false`, `tlsConfig=nil`), `WithInsecure(true)` override, and `WithTlsConfig(...)` field storage; mirrored in `v2/` and `v3/`

## [v3.0.1] - 2026-03-08 - Tidy v2 and v3 go.sum Dependencies

### Changed

- `v2/go.sum`, `v3/go.sum` — resolved transitive dependency checksums; no API changes
- `v2/provider.go`, `v3/provider.go` — no logic changes (module tidy only)

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/pgsql/v3` — Go v3 migration

### Changed

- Code duplicated into `integrations/bunorm/pgsql/v3/`; v2 and v3 implementations maintained in parallel
- Dependencies pinned to `bunorm/v3` and `melody/v3`

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.1...HEAD

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.1.0...integrations/bunorm/pgsql/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.1...integrations/bunorm/pgsql/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/pgsql/v3.0.0...integrations/bunorm/pgsql/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/pgsql/v3.0.0
