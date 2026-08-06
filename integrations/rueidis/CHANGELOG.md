# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `cache/backend.go` — `SetCtx`/`SetMultipleCtx` refuse a negative ttl instead of storing the value with no expiry at all. The `0 < ttl` branch folded a negative duration into the persist case, so the one entry the caller had computed to be already unreadable — `expiresAt.Sub(now)` past its deadline — was the one entry written forever, and a cached authorization decision written that way never lapsed. The in-memory backend behind the same `cache.Backend` contract refuses the identical call, so swapping the backends turned an error into a permanent entry. Zero keeps meaning no expiry on both, which is what separates it from the negative value. **Breaking** at runtime for a caller that relied on a negative ttl persisting.

### Fixed

- `cache/backend.go` — `Clear`/`ClearByPrefix` walk every node of the deployment rather than whichever one the client happened to pick. `SCAN` names no key, so a cluster client routes it to a single arbitrary node: the clear deleted that node's share of the matching keys and reported success, and an invalidation the caller read as complete left the other shards' entries live until their ttl. The scan now runs over `Client.Nodes()`, which answers the one client itself on a non-cluster deployment, so the single-node path is the same walk over one node.
- `provider.go` — the boot ping takes the default connect timeout when `TimeoutConfig.ConnectTimeout` is left at zero, instead of running on a context with no deadline. A config naming only the command timeout — the natural way to tune one of the two — left the health check unbounded, so a store that accepted the connection without answering hung boot forever while holding a client the caller could not yet close. `Ping` one screen below already read its own zero as the default, as does `WithRateLimiterCallTimeout`.
- `cache/backend.go` — a failing batch operation names the entry that failed. `DeleteMultiple`, `Clear` and `ClearByPrefix` discarded the per-key answer `rueidis.MDel` returns and re-raised the bare store error, `SetMultiple` returned the first failing response with no key attached at all (and, since the commands were built from map iteration, reported a different one of two bad items on each identical call), and `Many` dropped the key whose payload could not be read although the loop variable held it. Each now carries the key — chosen by sorting where the source is a map, so two identical failures report identically — plus how many of the batch failed, and the store's own error stays reachable through the chain. Key-validation refusals name the offending key too, which in a batch call is the only way to tell which of the keys handed in was malformed.
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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.1.0...HEAD

[v1.1.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.2...integrations/rueidis/v1.1.0

[v1.0.2]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.1...integrations/rueidis/v1.0.2

[v1.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v1.0.0...integrations/rueidis/v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v1.0.0
