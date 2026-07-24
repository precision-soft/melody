# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.1.1] - 2026-07-25 - Rate Limiter Call Timeout on the Runtime Path

### Fixed

- `rate_limit.go` — `RateLimiter.AllowWithRuntime` now applies the configured call timeout. It passed the runtime context straight through, and melody's http kernel attaches no deadline to a request context, so `WithRateLimiterCallTimeout` was dead on the exact path every http middleware uses: a Redis TCP black-hole (a partial partition, not a clean refusal) made every gated request hang under the recommended `FailureModeClosed` instead of failing fast. The runtime context is now capped with the call timeout (`context.WithTimeout` keeps the earlier deadline, so a request carrying a tighter one still wins), governing both entry points.

## [v1.1.0] - 2026-07-11 - Distributed Rate Limiter and Forwarded-Client-IP Resolver

### Added

- `rate_limit.go` — `NewRateLimiter(client, limit, window, options...)`: a Redis-backed fixed-window rate limiter implementing both `http/contract.RateLimiter` and the new `http/contract.RuntimeRateLimiter`, so N application instances enforce one shared limit — the distributed drop-in for the in-process middleware limiters, whose per-replica counters make a login rate limit meaningless behind a load balancer. The counter is one atomic Lua round trip (`INCR` + `PEXPIRE` on creation); the fixed window admits up to 2x the limit across a window edge. Store failures follow a configurable policy defaulting to fail-closed (deny when Redis is unreachable) — `WithRateLimiterFailureMode(FailureModeOpen)` selects availability instead; `WithRateLimiterOnError` observes failures on the void `Allow` path, `WithRateLimiterCallTimeout` (default 250ms) bounds it, and `WithRateLimiterKeyPrefix` namespaces the keys. Requires the melody version with `RuntimeRateLimiter` (unreleased at the time of this
  entry). Back-port from `v3`.

### Fixed

- `rate_limit.go` — `WithRateLimiterCallTimeout` falls back to the default call timeout for a non-positive duration instead of building an already-cancelled context. That context forced every `Allow`/`Reset` onto the store-failure path — deny-all under `FailureModeClosed`, allow-all under `FailureModeOpen` — no matter whether Redis was actually reachable.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.1.1...HEAD

[v1.1.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.1.0...integrations/rueidis/v1.1.1

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.2...integrations/rueidis/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.1...integrations/rueidis/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.0...integrations/rueidis/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v1.0.0
