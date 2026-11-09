# Changelog

All notable changes to `precision-soft/melody/integrations/outbox/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog, and the third-major fixes released after it are listed below as carried.
- `relay.go`, `relay_config.go`: `RelayConfig.LockReleaseTimeout` bounds the lease release and `RelayConfig.ResolutionTimeout` one resolution write, each on its detached context, 5s when left out as the constants were, at least 1ms, unlimited refused.

### Changed

- **Behavioural change** — `module.go`: `RegisterServices` refuses a `RelayCommand` configuration with neither `Relay` nor `RelayFactory`, naming `ModuleConfig.RelayCommand`, where the configuration was read by nothing and no command was registered.
- **Breaking** — `service_resolver.go`: `StoreMustFromResolver`, `StoreMustFromContainer`, `RelayMustFromResolver` and `RelayMustFromContainer` take a `context.Context` first, the fourth major's resolver form; the lazy repository resolves the store under the context of the call that reaches it, and the relay command resolves a lazy relay under its run's context.
- **Breaking** — `relay.go`, `relay_config.go`: `RelayConfig`'s `LockTtl`, `VisibilityTimeout`, `InitialBackoff` and `MaxBackoff` are `bound.Duration` and `BatchSize`, `MaxAttempts` and `MaxDeliveryAttempts` `bound.Count`, resolved once at `NewRelay`, a value outside its rule panicking naming the field, a zero or an unlimited one included where a non-positive value took the default; `LockTtl` and `VisibilityTimeout` are at least 1s, the backoffs at least 1ms, the counts at least 1. `BatchSize` above 100000 is refused where it was clamped, `MaxDeliveryAttempts` at or below `MaxAttempts` is refused naming both where it was raised to twice `MaxAttempts`, an `InitialBackoff` above `MaxBackoff` is refused naming both where every delay was held at the cap, and a `BackoffFactor` below 1 is refused where it took 2, and a NaN one where it was admitted. A nil `Repository`, `Transport` or `Codec` names the field.
- **Breaking** — `store.go`: `ClaimDueMessages` refuses a limit above 100000 where it cut it to that ceiling, the answer the relay's `BatchSize` gets.
- **Breaking** — `relay_command.go`, `module.go`: `NewRelayCommand` and `NewRelayCommandFromResolver` take a `RelayCommandConfig` after their dependency, whose `MaxErrorBackoff` caps the doubling delay between failed batches, one minute when left out as the constant was, at least 1ms, unlimited refused; `ModuleConfig.RelayCommand` carries it to the command the module registers.
- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.
- `relay.go`: the relay hands the resolved `RelayConfig.LockTtl` to the `Locker` as a `bound.Duration`, the fourth major's lock contract form.
- `relay.go`: the relay stamps the message id through `messagebus.NewEnvelope(message, []messagebuscontract.Stamp{...})`, the fourth major's envelope form.
- `relay.go` and `relay_command.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.

### Fixed

- **Behavioural change** — `relay.go` — carried from the third major (v3.2.1): a failed send ends the batch. The failed row alone is charged an attempt, and the rows the batch had not reached are handed back pending with their attempts unchanged, available again when the failed row is tried again, their `last_error` naming the row whose failed send held them. Before, the batch went on and charged every claimed row an attempt on every pass, so a broker outage of about 66 minutes at the default settings dead-lettered every good message the relay claimed; a claim takes the lowest id first, so an outage now charges the first row of each claimed batch alone, pass after pass, and dead-letters one row of that batch per ~66 minutes, every poll that finds rows enqueued during the outage claiming a batch of its own. A contained send panic ends the batch the same way. A hold write that fails, or a cancellation of the run, ends the hold, the rows not handed back re-surfacing after
  `VisibilityTimeout`.
  Once the broker is back, the held rows wait out the failed row's current backoff, at most `MaxBackoff`, and a message the broker refuses holds the rows behind it for its own backoff each time it is due. When the failed send dead-letters its row, the held rows still wait the backoff that row would have had, `MaxBackoff` at the default settings.
- `relay.go` — carried from the third major (v3.2.1): a failed send writes one `outbox message send failed` ERROR record with the row's id, its type, the attempt it was charged, `MaxAttempts`, when it is tried again or that it was dead-lettered, and the transport's cause, written once the row's reschedule or dead-letter write succeeded, where a rescheduled row left no trace in the process log, `RunOnce` answering no error for it and the cause reaching only `last_error`; the batch ends at the failed send, so a pass writes at most one such record, and a contained send panic keeps its own `outbox delivery panicked` record instead.
- relay: a batch failure whose `Unwrap` chain reaches itself no longer holds the relay command that classifies it during a shutdown.

### Security

- `go.mod`: `golang.org/x/crypto` v0.52.0 and `go.opentelemetry.io/otel` v1.41.0, past the versions the advisories name. None of them reached code this module calls.

### Documentation

- readme: carried from the third major (unreleased): the delivery semantics state that a failed `Acquire` of the optional `Locker` ends the run before it claims a row, so an outage of the lease store stops delivery, and how a wrapped `Locker` delivers through it.
