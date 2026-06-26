# Changelog

All notable changes to `precision-soft/melody/integrations/outbox/v3` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial transactional-outbox helper. `Store` (bun-backed) writes outbox rows: `Enqueue(ctx, executor, message)` takes a `bun.IDB`, so passing the caller's `bun.Tx` commits the outbox row atomically with the business write — the point of the pattern. A `Relay` (`NewRelay(RelayConfig{...})`) drains pending rows to any core `messagebus/contract.Transport` (e.g. the AMQP transport) with exponential backoff and a terminal dead-letter state: `RunOnce` publishes one batch of due messages, marking each sent, rescheduling a failed send with an incremented attempt count and capped backoff, and dead-lettering once `MaxAttempts` is reached or when a row cannot be decoded (poison). An optional core `lock/contract.Locker` (e.g. the Redis locker) leases the relay so only one instance drains at a time. Messages are stored and rebuilt through an application-supplied `MessageCodec`, and persistence sits behind a `Repository` interface that the `Store` implements, keeping the relay's retry/dead-letter logic unit-testable without a database. The reference design is Curatorium's `payment` outbox (15s base backoff, 12 attempts, Redis lease, dead-letter). Depends on the core `melody/v3` and `github.com/uptrace/bun`.
