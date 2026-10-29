# Changelog

All notable changes to `precision-soft/melody/integrations/outbox/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog, and the third-major fixes released after it are listed below as carried.

### Changed

- **Breaking** — `service_resolver.go`: `ServiceRegistrar.RegisterService` takes the framework's `RegisterOptions` struct last, the fourth major's registration form, where it took variadic `RegisterOption` functions; the application satisfies it as before.

### Fixed

- **Behavioural change** — `relay.go` — carried from the third major (v3.2.1): a failed send ends the batch. The failed row alone is charged an attempt, and the rows the batch had not reached are handed back pending with their attempts unchanged, available again when the failed row is tried again, their `last_error` naming the row whose failed send held them. Before, the batch went on and charged every claimed row an attempt on every pass, so a broker outage of about 66 minutes at the default settings dead-lettered every good message the relay claimed; a claim takes the lowest id first, so an outage now charges the first row of each claimed batch alone, pass after pass, and dead-letters one row of that batch per ~66 minutes, every poll that finds rows enqueued during the outage claiming a batch of its own. A contained send panic ends the batch the same way. A hold write that fails, or a cancellation of the run, ends the hold, the rows not handed back re-surfacing after
  `VisibilityTimeout`.
  Once the broker is back, the held rows wait out the failed row's current backoff, at most `MaxBackoff`, and a message the broker refuses holds the rows behind it for its own backoff each time it is due. When the failed send dead-letters its row, the held rows still wait the backoff that row would have had, `MaxBackoff` at the default settings.
- `relay.go` — carried from the third major (v3.2.1): a failed send writes one `outbox message send failed` ERROR record with the row's id, its type, the attempt it was charged, `MaxAttempts`, when it is tried again or that it was dead-lettered, and the transport's cause, written once the row's reschedule or dead-letter write succeeded, where a rescheduled row left no trace in the process log, `RunOnce` answering no error for it and the cause reaching only `last_error`; the batch ends at the failed send, so a pass writes at most one such record, and a contained send panic keeps its own `outbox delivery panicked` record instead.

### Documentation

- readme: carried from the third major (unreleased): the delivery semantics state that a failed `Acquire` of the optional `Locker` ends the run before it claims a row, so an outage of the lease store stops delivery, and how a wrapped `Locker` delivers through it.
