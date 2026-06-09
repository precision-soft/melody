# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.2.0] - 2026-06-09 - Redis Lock, Revocable Token Store, and Server-Sent Events Backplane

### Added

- `v3/service_resolver.go` + `v3/cache/service_resolver.go` — plug-and-play container registration: `RegisterClientService` (registers the Redis client under `ServiceClient`, with `ClientMustFromResolver`/`ClientMustFromContainer`), `RegisterLockerService` (under the core `lock.ServiceLocker`), `RegisterTokenStoreService` (under `ServiceTokenStore`, with `TokenStoreMustFromResolver`), and `cache.RegisterBackendService` (under the core `cache.ServiceCacheBackend`), so an application configures the Redis client once and resolves the lock, token store, and cache backend from many services.
- `v3/lock.go` — Redis-backed implementation of the core `lock/contract.Locker`/`Lock`. `NewLocker(client)` creates named locks; `Acquire` runs an atomic compare-and-set Lua script that grants the lock when the key is absent **or** already held by this same token (refreshing its TTL), `Release` runs a `GET`-and-`DEL` compare-and-delete Lua script (releases only when this instance still owns the key), and `Refresh` runs a `GET`-and-`PEXPIRE` compare-and-extend script that errors when the lock is no longer held. Each lock owns a 16-byte crypto-random token so it can never release or refresh another holder's lock.
- `v3/token_store.go` — Redis-backed implementation of the core `security/contract.RevocableTokenStore`. `NewTokenStore(client, ...options)` stores JSON-encoded claims at `{<prefix>}:token:<token>` with `PX` matching the ttl (zero ttl persists) and indexes each token under its owner in a `{<prefix>}:user:<id>` set. The whole keyspace is pinned to one Redis Cluster slot through the `{<prefix>}` hash tag, so the multi-key `Put`/`Delete` Lua scripts — which also touch the per-user index, including the previous owner's index set when a token is re-issued to a different user, a key the script computes dynamically and so cannot pre-declare — never raise `CROSSSLOT` on a cluster deployment. `Put`/`Delete`/`DeleteByUser` use Lua so the token key and the user index stay consistent in one round trip — including re-indexing when a token is re-issued to a different user. `DeleteByUser` returns the count of live tokens revoked and drops the set (members whose key Redis already expired are not counted but are still cleaned). `PurgeExpired` reconciles the user index by pruning members whose token key has already expired (Redis expires the token keys natively; it returns the number of stale members pruned). The context-less mutators use a constructor-bound context (`WithTokenStoreContext`, default background) whose cancellation is detached via `context.WithoutCancel` — values carry through but a cancelled parent can never permanently brick the mutators; `Lookup` uses the per-request `runtime.Context()`. The key namespace is configurable via `WithTokenStorePrefix` (default `melody:token`).
- `v3/server_sent_event_backplane.go` — `NewServerSentEventBackplane(client, hub, ...options)` is a Redis pub/sub backplane for the core `http.ServerSentEventHub`, so Server-Sent Events/WebSocket broadcasts fan out across every application instance behind a load balancer instead of reaching only the instance that emitted them. Each broadcast is published to a shared channel (`WithServerSentEventBackplaneChannel`, default `melody:sse`) tagged with a per-instance random origin; a dedicated subscription forwards other instances' events into the hub via `DeliverLocal` and ignores the echo of its own origin, so no event is delivered twice. The subscription re-establishes itself with bounded backoff (1s→30s) after a connection drop; `Close` stops it and detaches the backplane from the hub (`SetBackplane(nil)`), so a `Broadcast` issued after `Close` is no longer replicated to a torn-down backplane on a cancelled context (which would otherwise keep incrementing `BackplaneFailures()`). The Redis client is caller-owned and is not closed. `WithServerSentEventBackplaneLogger` surfaces subscription/decode failures.

### Fixed

- `v3/token_store.go` — `DeleteByUser` no longer revokes another user's live token. The user-index set member is only re-pointed away from the previous owner inside the `if existing` branch of the put script, so when a token string's prior value had already TTL-expired before the same string was re-issued to a different user, the stale membership lingered in the old owner's set — and `PurgeExpired` could not prune it (the token key exists again). `DeleteByUser` then deleted that token (now owned by the new user) and counted it for the wrong user. `tokenDeleteByUserScript` now re-reads each member's stored `UserIdentifier` and deletes only tokens still owned by the requested user, so a stale index member can no longer cross-revoke.
- `v3/lock.go` + `v3/token_store.go` — a positive but sub-millisecond TTL is now floored to 1ms instead of truncating to `0` via `time.Duration.Milliseconds()`. A `0` reached Redis with three different broken outcomes: the token store's persist branch treated `"0"` as "no expiry", so a token meant to expire was stored **permanently**; `Lock.Acquire` built `SET … PX 0`, which Redis rejects (`invalid expire time`), so the acquire failed instead of taking a short-lived lock; and `Lock.Refresh` ran `PEXPIRE key 0`, which deletes the key immediately, silently dropping the lock while the holder still believed it held it. A shared `floorPositiveMilliseconds` helper guarantees a positive TTL never collapses to `0`.
- `v3/lock.go` — `Acquire` is now reentrant for the same lock instance, matching the in-memory locker's behaviour that the shared `lock` contract asserts (and tests). The previous `SET … NX` returned `false` on a re-acquire of a lock this instance already held; a caller reading that as "not held" would skip `Release` and orphan the key until its TTL elapsed (or forever for a non-expiring lock). The compare-and-set Lua script now re-grants and refreshes the TTL when the key already holds this token.
- `v3/cache/backend.go` — `SetCtx`/`SetMultipleCtx` now floor a positive sub-millisecond TTL to 1ms. rueidis derives the `PX` argument as `int64(ttl/time.Millisecond)`, so a positive ttl below 1ms became `SET … PX 0`, which Redis rejects (`invalid expire time`) — failing the whole write — whereas the in-memory backend accepts the same ttl. Flooring keeps the two backends consistent (mirrors the existing `floorPositiveMilliseconds` guard on the lock and token store).
- `README.md` — the v3 plug-and-play section resolved the cache backend with `cache.CacheMustFromResolver` (service `cache.ServiceCache`), but `cache.RegisterBackendService` registers only `cache.ServiceCacheBackend`, so following the documented wiring would panic at resolution. The README now resolves the backend with the matching `cache.CacheBackendMustFromResolver`.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.2.0...HEAD

[v3.2.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.1.0...integrations/rueidis/v3.2.0

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.0.1...integrations/rueidis/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v3.0.0...integrations/rueidis/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v1.0.0
