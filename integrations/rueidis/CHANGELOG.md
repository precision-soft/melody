# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `Connection` — the closer the container's teardown recognizes, wrapping a client whose own `Close` returns nothing: register it as the service that owns the client and resolve the client through it, and the ordered shutdown closes the one owner exactly once. Every application wired the same wrapper by hand or leaked the client's connections at teardown
- `NewBackendWithCommandTimeout` and `NewBackendServiceWithCommandTimeout` — the ctx-less half of the cache contract runs under a bound: the `cachecontract.Backend` methods carry no context, so a request-path read against a store that accepts connections but stops answering hung the handler with nothing but the client's write timeout in the way — while the rate limiter in the same package already bounded its own calls for exactly that reason. A non-positive value reads as unbounded, the exact behaviour of the existing constructors

### Changed

- `provider.go` — the user and password parameters read through `MustString`, the convention of the bunorm siblings: a credential registered with the wrong type panics at boot naming the parameter and the type, where `String()` folded it to `""` and connected with no credential at all — green against a passwordless development store, "redis connection failed" pointing at the network against a secured one. **Breaking** at runtime for a configuration already carrying a wrong-typed credential, which was silently ignored until now
- `provider.go` — `Open` marks the configured password parameter secret through the configuration's own `MarkSecret`, so the introspection output masks the credential and every template derived from it
- `cache/backend.go` — `ClearByPrefixCtx` refuses the empty prefix the way every other operation refuses the empty key: the explicit branch routed `""` to `ClearCtx`, so a prefix assembled at run time that came out empty — `"tenant:" + id` with an unresolved id — wiped the whole namespace instead of being refused, measured live with two unrelated key groups deleted by one call. A caller that means the whole namespace has `Clear`, which says so. **Breaking** for a caller that used `ClearByPrefix("")` as a synonym of `Clear`
- `cache/backend.go` — `Close` marks the backend closed and every later operation refuses with the exact answer the in-memory backend behind the same contract gives, while the shared client — owned by the composition root — stays untouched and keeps serving its other borrowers. A closed backend served forever, so a teardown-ordering bug was caught immediately in every in-memory-backed environment and was invisible behind redis
- `cache/backend.go` — `SetMultipleCtx` judges the ttl before the empty early-return, the order the in-memory backend judges it in, and the counter refusals of `IncrementCtx`/`DecrementCtx` are wrapped with the key they happened on — the raw store error named neither the key nor the operation, and the counter path is the one a caller most often logs verbatim
- `client_config.go` — `ClientConfig`'s GoDoc states the zero-value trap out loud: the documented defaults are `PingOnStart: true` and `DisableCache: true` while a Go struct starts both at `false`, so a partial literal written to tune one field silently disarms the boot ping and turns client-side caching ON — a subsystem with a 128 MiB per-connection budget and RESP3 requirements. Start from `DefaultClientConfig()`. Measured while narrowing the finding: the dial stays bounded either way (the library normalizes a zero dial timeout to its own 5s default) and a dead address refuses eagerly at client creation even with the ping off
- `cache/backend.go` — `SetCtx`/`SetMultipleCtx` refuse a negative ttl instead of storing the value with no expiry at all. The `0 < ttl` branch folded a negative duration into the persist case, so the one entry the caller had computed to be already unreadable — `expiresAt.Sub(now)` past its deadline — was the one entry written forever, and a cached authorization decision written that way never lapsed. The in-memory backend behind the same `cache.Backend` contract refuses the identical call, so swapping the backends turned an error into a permanent entry. Zero keeps meaning no expiry on both, which is what separates it from the negative value. **Breaking** at runtime for a caller that relied on a negative ttl persisting.

### Fixed

- `cache/backend_service.go` — the service's `Close` reaches the handles `WithContext` minted: a derived handle reads its owner's closed flag alongside its own, so after the teardown ends the service, the runtime door — `BackendFromRuntime`, which mints a handle per request — refuses like every other path instead of quietly serving through a client whose owner already ended this backend. A sibling backend built directly over the same client stays independent, deliberately: the client belongs to whoever built it
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
