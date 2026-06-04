# Changelog

All notable changes to `precision-soft/melody/integrations/amqp` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial Melody v3 binding of the AMQP integration — a durable `messagebus/contract.Transport` backed by RabbitMQ (AMQP 0-9-1) on `rabbitmq/amqp091-go`. Developed v3-first; v1 and v2 bindings to follow.
- `connection.go` — `Provider` with `NewProvider(...ProviderOption)` builder (`WithHeartbeat`), `Open(dsn) (*amqp091.Connection, error)`, and `Close`. DSN credentials are redacted in error context.
- `registry.go` — `MessageRegistry` with generic `RegisterMessage[T](registry, name)`; maps message Go type ↔ wire type name so the consumer can reconstruct the concrete type from the `x-message-type` header.
- `transport.go` — `Transport` / `TransportConfig` implementing `Send`, `Receive`, `Ack`, `Nack`, `Close`. Lazily opens separate publish and consume channels, declares queue/exchange topology on first use, JSON-serializes the message body (reusing core `serializer.JsonSerializer`), and sets prefetch via QoS. Decode failures are dead-lettered.
- `stamp.go` — `DeliveryStamp` (`Tag`, `Redelivered`) carried on received envelopes so `Ack`/`Nack` can map back to the AMQP delivery.
- Dead-letter support: when `DeadLetter` is enabled, declares `<queue>.dlx` / `<queue>.dlq` and points the main queue at it. Bounded single-retry policy — an already-redelivered message that fails again is dead-lettered instead of requeued (poison-message guard).
- `parameter.go` — `ParameterDsn`, `ParameterExchange`, `ParameterPrefetch`, and `RegisterDefaultParameters`.
- `transport_test.go` — send/receive/ack integration test, skipped unless `AMQP_DSN` is set; verified end-to-end against RabbitMQ 3.13.
- A `rabbitmq` service added to `.dev/docker/docker-compose.yml` for local/integration runs.
