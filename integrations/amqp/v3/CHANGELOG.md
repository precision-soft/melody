# Changelog

All notable changes to `precision-soft/melody/integrations/amqp` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial Melody v3 binding of the AMQP integration — a durable `messagebus/contract.Transport` backed by RabbitMQ (AMQP 0-9-1) on `rabbitmq/amqp091-go`. Developed v3-first; v1 and v2 bindings to follow.
- `connection.go` — `Provider` with `NewProvider(...ProviderOption)` builder (`WithHeartbeat`), `Open(dsn) (*amqp091.Connection, error)`, and `Close`. DSN credentials are redacted in error context.
- `registry.go` — `MessageRegistry` with generic `RegisterMessage[T](registry, name)`; maps message Go type ↔ wire type name so the consumer can reconstruct the concrete type from the `x-message-type` header; a conflicting re-registration (the same name bound to a different type, or vice versa) panics instead of silently overwriting and corrupting later deserialization.
- `transport.go` — `Transport` / `TransportConfig` implementing `Send`, `Receive`, `Ack`, `Nack`, `Close`. Lazily opens separate publish and consume channels, declares queue/exchange topology on first use, JSON-serializes the message body (reusing core `serializer.JsonSerializer`), and sets prefetch via QoS. Decode failures are dead-lettered. On `Nack(requeue=true)` the message is re-published (not broker-requeued) carrying an incremented `x-redelivery-count` header, which `decode` reads back into a `messagebus.RedeliveryStamp` — so the consumer's `RetryPolicy` (max retries, dead-letter transport) is honored uniformly across transports instead of relying on the broker's one-shot `redelivered` flag. A `DelayStamp` re-publishes through a `<queue>.delay` queue (per-message TTL, dead-lettered back to the main queue), so the consumer's backoff actually spaces out retries. Consume-channel acknowledgements (`Ack`/`Nack`/the requeue re-publish, and the decode-failure nack emitted from the delivery-forwarding goroutine) are serialized through a dedicated consume mutex, mirroring the publish mutex, since an `amqp091` channel is not safe for concurrent use.
- `stamp.go` — `DeliveryStamp` (`Tag`, `Redelivered`) carried on received envelopes so `Ack`/`Nack` can map back to the AMQP delivery.
- Dead-letter support: when `DeadLetter` is enabled, declares `<queue>.dlx` / `<queue>.dlq` and points the main queue at it. A message whose retries are exhausted is nacked without requeue (`Nack(requeue=false)`) so it lands in the DLX rather than being dropped; the retry count itself is owned by the consumer through the persisted `x-redelivery-count` header (see `transport.go`).
- `parameter.go` — `ParameterDsn`, `ParameterExchange`, `ParameterPrefetch`, and `RegisterDefaultParameters`.
- `transport_test.go` — send/receive/ack integration test, plus tests that a re-published message persists its `x-redelivery-count` and dead-letters once retries are exhausted, and that a `DelayStamp` routes through the `<queue>.delay` queue and returns after its TTL; all skipped unless `AMQP_DSN` is set; verified end-to-end against RabbitMQ 3.13.
- A `rabbitmq` service added to `.dev/docker/docker-compose.yml` for local/integration runs.
