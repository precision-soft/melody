# Changelog

All notable changes to `precision-soft/melody/integrations/rueidis/v2` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v2.1.0] - 2026-07-11 - Distributed Rate Limiter and Forwarded-Client-IP Resolver

### Added

- `v2/rate_limit.go` — `NewRateLimiter(client, limit, window, options...)`: a Redis-backed fixed-window rate limiter implementing both `http/contract.RateLimiter` and the new `http/contract.RuntimeRateLimiter`, so N application instances enforce one shared limit — the distributed drop-in for the in-process middleware limiters, whose per-replica counters make a login rate limit meaningless behind a load balancer. The counter is one atomic Lua round trip (`INCR` + `PEXPIRE` on creation); the fixed window admits up to 2x the limit across a window edge. Store failures follow a configurable policy defaulting to fail-closed (deny when Redis is unreachable) — `WithRateLimiterFailureMode(FailureModeOpen)` selects availability instead; `WithRateLimiterOnError` observes failures on the void `Allow` path, `WithRateLimiterCallTimeout` (default 250ms) bounds it, and `WithRateLimiterKeyPrefix` namespaces the keys. Requires melody/v2 with `RuntimeRateLimiter` (unreleased at the time of this entry).

### Fixed

- `v2/rate_limit.go` — `WithRateLimiterCallTimeout` falls back to the default call timeout for a non-positive duration instead of building an already-cancelled context. That context forced every `Allow`/`Reset` onto the store-failure path — deny-all under `FailureModeClosed`, allow-all under `FailureModeOpen` — no matter whether Redis was actually reachable.

## [v2.0.2] - 2026-06-25 - Keep the Caller-Owned Client Open and Floor Sub-Millisecond TTL

### Fixed

- `v2/cache/backend.go` — `Backend.Close` called `client.Close()` on the rueidis client it does not own (the client lifecycle is owned by the application and shared with the provider, locker, and token store), so closing the cache backend tore down the shared connection for every other consumer. `Close` is now a no-op, matching the `v3` behavior.
- `v2/cache/backend.go` — `Set`/`SetMultiple` passed a positive sub-millisecond TTL straight to `PX`, which truncates to `PX 0` (an invalid expiry); a positive TTL below 1ms is now floored to 1ms via `floorPositiveExpiry`. Ported from the `v3` fix.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.1.0...HEAD

[v2.1.0]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.2...integrations/rueidis/v2.1.0

[v2.0.2]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.1...integrations/rueidis/v2.0.2

[v2.0.1]: https://github.com/precision-soft/melody/compare/integrations/rueidis/v2.0.0...integrations/rueidis/v2.0.1

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/rueidis/v2.0.0
