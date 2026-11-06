# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/mysql/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `provider_config.go`: `ProviderConfig`, the one struct a provider is built from, `Pool`, `Timeout`, `Retry`, `PostBuildHook`, `Insecure` and `TlsConfig`, its zero value the default.
- `lock.go`: `LockerConfig`, the one struct a locker is built from, `ReleaseTimeout` 5s when left out, at least 1ms, unlimited refused; `ModuleConfig.Locker` hands it from the module.

### Changed

- `module.go`: **Behavioural change**: `RegisterServices` refuses `AsLocker` with no database, and `Locker` set with `AsLocker` off, naming the field, where it registered nothing.
- **Breaking** — `provider.go`: `NewProvider` takes one `ProviderConfig`, resolved once, a limit outside its rule panicking naming the field before any database is opened; `ProviderOption`, `WithPoolConfig`, `WithTimeoutConfig`, `WithRetryConfig`, `WithPostBuildHook`, `WithInsecure` and `WithTlsConfig` are removed.
- **Breaking** — `pool_config.go`, `timeout_config.go`, `retry_config.go`: the fields are `bound` limits, absent for the default: pool 25 / 5, lifetime 5m and idle time 1m at least 1s; connect 10s, read and write 30s, at least 1ms; retry 3 attempts at least 1, 500ms and 5s at least 1ms. Unlimited is `bound.UnlimitedCount()` / `bound.UnlimitedDuration()`, admitted on the pool and on the read and write deadlines, refused on the connect timeout and every retry field; `MaxIdleConnections: bound.Of(0)` keeps no idle connection; `BackoffMultiplier` 0 the default 2, below 1 or NaN refused where it fell back to 2; a delay under 1ms refused where it was raised to 1ms. `DefaultPoolConfig`, `NewPoolConfig`, `DefaultTimeoutConfig`, `NewTimeoutConfig`, `DefaultRetryConfig`, `NewRetryConfig` and `Unlimited` are removed.
- **Behavioural change** — `provider.go`: a left-out `RetryConfig` is the default retry, three attempts, where an absent one made a single attempt; `RetryConfig{MaxAttempts: bound.Of(1)}` is the single attempt, and it journals its terminal failure and marks it logged as the last attempt of a retry does. An empty `ConnectionParameters.Port` dials 3306.
- **Breaking** — `lock.go`, `service_resolver.go`: `NewLocker` takes the database and a `LockerConfig`, a `ReleaseTimeout` outside its rule panicking naming the field where a non-positive one took 5s, and `RegisterLockerService` takes it after the database, resolved at the call; `LockerOption` and `WithLockReleaseTimeout` are removed.
- **Breaking** — `connection_config.go`: `ConnectionConfig`, `NewConnectionConfig` and the five getters are removed; the provider reads the `bunorm.ConnectionParameters` it is handed, and the failed-open errors carry its `SafeContext`, the pool and the deadlines named as `unlimited` or the value.
- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
- **Breaking** — `lock.go`: `Locker.CreateLock` and the lock's `Refresh` take the ttl as a `bound.Duration`, the fourth major's lock contract form; the session lock still ignores it, every state admitted, `bound.UnlimitedDuration()` its honest spelling.
- `provider.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.

### Fixed

- `lock.go`: `Release` ends at the earlier of the locker's `ReleaseTimeout` and the deadline of the runtime it is handed while that one is still ahead, on that runtime's context without its cancellation, where it released on a detached budget of its own, so a `RunExclusive` or `LeaderGate` release budget shorter than the locker's was a stall of the locker's; a release cut by either deadline ends the session, which frees the lock server-side, and names `releaseTimeout` and `deadline`.
