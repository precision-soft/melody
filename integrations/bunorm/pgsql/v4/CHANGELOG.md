# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm/pgsql/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.3.0; that history is in the third major's changelog.
- `provider_config.go`: `ProviderConfig`, the one struct a provider is built from, `Pool`, `Timeout`, `Retry`, `PostBuildHook`, `Insecure` and `TlsConfig`, its zero value the default.
- `lock.go`: `LockerConfig`, the one struct a locker is built from, `ReleaseTimeout` 5s when left out, at least 1ms, unlimited refused; `ModuleConfig.Locker` hands it from the module.

### Changed

- **Breaking** — `post_build_hook.go`, `provider.go`, `README.md`: `PostBuildHook` takes the runtime first, `func(runtimeInstance, connector) error`, handed the opener's runtime over a context ending at the connect timeout and done once the open returns, where it took a bare `context.Context`; with a hook set, `Open` and `OpenForMigration` refuse a runtime whose scope is nil as an error naming `runtimeInstance` and `ProviderConfig.PostBuildHook`.
- **Breaking** — `lock.go`, `README.md`: `Locker.CreateLock(name, lockcontract.CreateLockOptions)` and the lock's `Refresh(runtimeInstance, lockcontract.RefreshOptions)` take the fourth major's options structs, both ignored by a session lock as the ttl was.
- `lock.go`, `README.md`: the locker's GoDoc and the readme say a session lock is not a shared lease: the locker does not answer `lockcontract.SharedLeaseLocker`, and the cron tick dedup warns over it.
- **Breaking** — `module.go`, `service_resolver.go`: `ModuleConfig.Locker` is a `*LockerConfig`, nil registering no locker and a pointer registering it, one handed with a nil database refused at `RegisterServices` naming `ModuleConfig.Locker`; `AsLocker`, the refusal of a locker configuration with the locker off, the exported `RegisterLockerService` and `ServiceRegistrar` are removed, the module being the door.
- **Breaking** — `provider.go`: `Open` and `OpenForMigration` refuse a typed-nil logger as `pgsql provider logger is a typed nil`, naming `pgsql.Provider.Open.logger`, where it read as the emergency logger; a nil logger is still the emergency logger.
- **Breaking** — `lock.go`: `CreateLock` answers `(lockcontract.Lock, error)`, a nil error for any ttl.
- **Breaking** with the framework: the fourth major's runtime is read from `v4/container/contract` and built through `v4/container/runtime`, a runtime resolving through its scope only; `runtime.New` and `Runtime.Container()` are gone. See the framework's `UPGRADE.md`.
- **Breaking** — `provider.go`: `NewProvider` refuses `Insecure: true` beside a non-nil `TlsConfig`, naming both fields, where `TlsConfig` won in silence.
- **Breaking** — `provider.go`: `Open(runtimeInstance, parameters, logger)` and `OpenForMigration(runtimeInstance, parameters, logger)` take the runtime first and open under its context, the bodies of the removed `OpenContext` and `OpenForMigrationContext`; a nil runtime, a typed nil and one carrying no context are refused with "the runtime is nil or carries no context" before bun's diagnostics are routed, where a nil context read as `context.Background()`. The refusal before the attempt and the one cutting a retry's backoff carry the context's cause with the cancellation under it, so `errors.Is` matches a cause set through `WithCancelCause` and `context.Canceled` alike.
- **Breaking** — `provider.go`: `NewProvider` takes one `ProviderConfig`, resolved once, a limit outside its rule panicking naming the field before any database is opened; `ProviderOption`, `WithPoolConfig`, `WithTimeoutConfig`, `WithRetryConfig`, `WithPostBuildHook`, `WithInsecure` and `WithTlsConfig` are removed.
- **Breaking** — `pool_config.go`, `timeout_config.go`, `retry_config.go`: the fields are `bound` limits, absent for the default: pool 50 / 25, lifetime 5m and idle time 1m at least 1s; connect 5s, read and write 30s, at least 1ms; retry 3 attempts at least 1, 500ms and 5s at least 1ms. Unlimited is `bound.UnlimitedCount()` / `bound.UnlimitedDuration()`, admitted on the pool and on the read and write deadlines, refused on the connect timeout and every retry field; `MaxIdleConnections: bound.Of(0)` keeps no idle connection; `BackoffMultiplier` 0 the default 2, below 1 or NaN refused where it fell back to 2; a delay under 1ms refused where it was raised to 1ms. `DefaultPoolConfig`, `NewPoolConfig`, `DefaultTimeoutConfig`, `NewTimeoutConfig`, `DefaultRetryConfig`, `NewRetryConfig` and `Unlimited` are removed, and so is `NewTimeoutConfigWithDeadlines`, the name the third major gave the three-duration form.
- **Behavioural change** — `provider.go`: a left-out `RetryConfig` is the default retry, three attempts, where an absent one made a single attempt; `RetryConfig{MaxAttempts: bound.Of(1)}` is the single attempt, and it journals its terminal failure and marks it logged as the last attempt of a retry does. An empty `ConnectionParameters.Port` dials 5432.
- **Breaking** — `lock.go`, `service_resolver.go`: `NewLocker` takes the database and a `LockerConfig`, a `ReleaseTimeout` outside its rule panicking naming the field where a non-positive one took 5s; `LockerOption` and `WithLockReleaseTimeout` are removed.
- **Breaking** — `connection_config.go`: `ConnectionConfig`, `NewConnectionConfig` and the five getters are removed; the provider reads the `bunorm.ConnectionParameters` it is handed, and the failed-open errors carry its `SafeContext`, the pool and the deadlines named as `unlimited` or the value.
- **Breaking** — `lock.go`: `Locker.CreateLock` and the lock's `Refresh` take the ttl as a `bound.Duration`, the fourth major's lock contract form; the session lock still ignores it, every state admitted, `bound.UnlimitedDuration()` its honest spelling.
- `provider.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `retry_config.go`: `RetryConfig.InitialDelay`, `MaxDelay` and `BackoffMultiplier` are one `RetryConfig.Backoff bound.Backoff` (500ms, 5s, 2), resolved by `bound.ResolveBackoff` and refused naming `RetryConfig.Backoff.<field>`; a `Max` below `Initial` is refused where the first wait was capped; the open retry waits `ResolvedBackoff.Delay(attempt)`, the sequence on the defaults unchanged.

### Fixed

- `lock.go`: a `pg_advisory_unlock` the server answers `false`, a session that does not hold the lock, as behind a proxy pooling by transaction, fails the release and ends the session, where the answer was discarded, `Release` answered nil and the lock stayed held by the pooled session.
- `lock.go`: `Release` ends at the earlier of the locker's `ReleaseTimeout` and the deadline of the runtime it is handed while that one is still ahead, on that runtime's context without its cancellation, where it released on a detached budget of its own, so a `RunExclusive` or `LeaderGate` release budget shorter than the locker's was a stall of the locker's; a release cut by either deadline ends the session, which frees the lock server-side, and names `releaseTimeout` and `deadline`.
- provider: a post-build hook's error whose `Unwrap` chain reaches itself no longer holds the open that classifies it.
- pgsql: `IsDuplicateKey` returns on an error whose `Unwrap` chain reaches itself.

### Security

- `go.mod`: `golang.org/x/crypto` v0.52.0 and `go.opentelemetry.io/otel` v1.41.0, past the versions the advisories name. None of them reached code this module calls.
