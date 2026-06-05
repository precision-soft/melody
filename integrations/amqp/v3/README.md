# Melody AMQP integration (v3)

A durable, transport-agnostic [`messagebus`](https://github.com/precision-soft/melody) transport backed by RabbitMQ (AMQP 0-9-1), built on [`rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go).

It implements `messagebus/contract.Transport`, so a Melody message bus can route messages to RabbitMQ for asynchronous, cross-process handling, and the `melody:messagebus:consume` worker can drain them.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/amqp/v3
```

```go
import amqp "github.com/precision-soft/melody/integrations/amqp/v3"
```

## Parameters

`RegisterDefaultParameters` registers sensible defaults that userland may override:

| Parameter | Constant | Default |
| --- | --- | --- |
| `melody.amqp.dsn` | `ParameterDsn` | `amqp://guest:guest@localhost:5672/` |
| `melody.amqp.prefetch` | `ParameterPrefetch` | `10` |
| `melody.amqp.exchange` | `ParameterExchange` | _(unset; direct-to-queue)_ |

## Usage

### Open a connection

```go
provider := amqp.NewProvider(amqp.WithHeartbeat(10 * time.Second))

connection, openErr := provider.Open(configuration.Get(amqp.ParameterDsn).String())
if nil != openErr {
	return openErr
}
```

### Register message types

The transport serializes the message body as JSON and stores the message type name in the `x-message-type` header so the consumer can reconstruct the concrete Go type.

```go
registry := amqp.NewMessageRegistry()
amqp.RegisterMessage[WelcomeEmail](registry, "welcome_email")
```

Register the same value type your handlers are registered for (the message bus locates handlers by exact Go type).

### Build a transport

```go
transport := amqp.NewTransport(amqp.TransportConfig{
	Connection: connection,
	Queue:      "welcome_email",
	Prefetch:   10,
	Registry:   registry,
	DeadLetter: true,
})
```

Wire it into a dispatch bus with `messagebus.NewSendMessageMiddleware`, and into the `messagebus.NewConsumeCommand` transports map under a name (for example `"async"`). Consume with:

```sh
app melody:messagebus:consume --transport=async
```

### Auto-reconnect

By default a dropped broker connection stops the consumer (run it under a process supervisor). To let the transport recover on its own, also set a `Dialer` — `Provider.Dialer(dsn)` builds one from the same DSN:

```go
provider := amqp.NewProvider()
connection, _ := provider.Open(dsn)

transport := amqp.NewTransport(amqp.TransportConfig{
	Connection: connection,
	Dialer:     provider.Dialer(dsn),
	Queue:      "welcome_email",
	Prefetch:   10,
	Registry:   registry,
})
```

When the consume channel is lost, the delivery loop re-dials and re-subscribes with bounded exponential backoff (1s → 30s) and resumes on the **same** output channel, so the `melody:messagebus:consume` worker keeps running across a broker restart. The publish path drops a dead channel and retries once. `Close` stops the reconnect loop and closes only connections the transport itself dialed — never the one you passed in.

## Dead-lettering and retries

When `DeadLetter` is `true`, the transport declares a dead-letter exchange (`<queue>.dlx`) and queue (`<queue>.dlq`), and points the main queue at it via `x-dead-letter-exchange`.

- A handler success acknowledges the delivery (`Ack`).
- A handler failure negatively acknowledges it. To avoid poison-message loops, a message that was already redelivered is dead-lettered instead of being requeued again. This is a bounded single-retry policy; delayed/backoff retry is not yet implemented.
- A delivery that cannot be decoded (missing or unknown `x-message-type`, bad body) is dead-lettered immediately.

## Footguns & caveats

- The transport uses one channel for publishing and one for consuming, created lazily. `Ack`/`Nack` operate on the consume channel, so they must be called from the process that received the message.
- With auto-reconnect, a message received just before a reconnect carries a delivery tag from the old channel; acking it after the channel rotated fails benignly and the broker redelivers it. Combined with the at-least-once requeue, this means **handlers must be idempotent**.
- Queue/exchange topology is declared on first use and assumed stable. Redeclaring with conflicting arguments will fail at the broker.
- Stamps are process-local and are not serialized over the wire; only the message body and its type name cross the broker. The transport adds a `DeliveryStamp` and a `messagebus.ReceivedStamp` on receive.
- The integration test (`transport_test.go`) is skipped unless `AMQP_DSN` is set. A RabbitMQ service is available in `.dev/docker/docker-compose.yml`.
