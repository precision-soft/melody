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

### Configure a publisher

Wire the transport into a dispatch bus by routing your message types to it, then dispatch:

```go
locator := messagebus.NewHandlerLocator()
messagebus.RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, welcome WelcomeEmail) error {
	return nil
})

routing := map[reflect.Type]messagebus.TransportRouting{
	reflect.TypeOf(WelcomeEmail{}): {Name: "async", Transport: transport},
}

dispatchBus := messagebus.NewManager(
	"default",
	messagebus.NewSendMessageMiddleware(routing),
	messagebus.NewHandleMessageMiddleware(locator),
)

dispatchBus.Dispatch(runtimeInstance, WelcomeEmail{UserId: 1, Address: "user@example.com"})
```

A routed type is published to RabbitMQ and the dispatch returns immediately; an unrouted type falls through to the handle middleware and runs inline.

### Configure a consumer

Build a handle-only bus and the consume command, then run it as a long-lived worker:

```go
consumeBus := messagebus.NewManager(
	"default.consume",
	messagebus.NewHandleMessageMiddleware(locator),
)

consumeCommand := messagebus.NewConsumeCommandWithRetry(
	consumeBus,
	map[string]messagebuscontract.Transport{"async": transport},
	messagebus.RetryPolicy{MaxRetries: 3},
)
```

Register `consumeCommand` in your application's command list (so it shows up as `melody:messagebus:consume`), then run it:

```sh
app melody:messagebus:consume --transport=async
app melody:messagebus:consume --transport=async --concurrency=8
```

`--transport` selects which entry of the transports map to drain; `--concurrency` runs that many worker goroutines reading the same queue.

### Routing keys

The transport binds one queue and uses a single, static routing key — there is no per-message routing key. Message types are routed to transports in the bus layer (the `reflect.Type` routing map above), not by an AMQP routing key.

- **No `Exchange` (default):** the transport publishes to the default exchange using the queue name as the routing key — a direct delivery to the queue. The `RoutingKey` field is ignored.
- **With `Exchange` set:** the transport declares it as a `direct` exchange, binds its queue to the exchange with `RoutingKey`, and publishes to `(Exchange, RoutingKey)`. To deliver different message types to different queues, build one transport per destination (each with its own `Queue`/`RoutingKey`) and map the types to them.

### Plug-and-play registration

Configure the connection once and resolve it (or a named transport) from many services:

```go
amqp.RegisterConnectionService(registrar, connection)
amqp.RegisterTransportService(registrar, "async", transport)
```

```go
connection := amqp.ConnectionMustFromResolver(resolver)
transport := amqp.TransportMustFromResolver(resolver, "async")
```

`amqp.RegisterDefaultParameters(registrar)` registers the default `melody.amqp.*` parameters.

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
- A handler failure is retried under the consumer's `RetryPolicy` (max retries, base delay, optional dead-letter transport). The transport re-publishes the message carrying an incremented `x-redelivery-count` header, so the retry count survives across deliveries instead of relying on the broker's one-shot `redelivered` flag; a `DelayStamp` re-publishes through the `<queue>.delay` queue (per-message TTL, dead-lettered back to the main queue) so retries are spaced out by the configured backoff. Once the retries are exhausted the message is `Nack`ed without requeue so the broker routes it to the dead-letter exchange.
- A delivery that cannot be decoded (missing or unknown `x-message-type`, bad body) is `Nack`ed without requeue. It is dead-lettered only when `DeadLetter` is enabled; otherwise the broker discards it (enable `DeadLetter` in production so undecodable deliveries are retained).

## Server-Sent Events backplane

`NewServerSentEventBackplane(ServerSentEventBackplaneConfig{...})` makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a fanout exchange — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each instance binds its own exclusive, auto-deleted queue to the exchange (default `melody.sse`), so a published broadcast reaches every instance; the events of other instances are forwarded into the hub via `DeliverLocal`, and a tagged per-instance origin makes each instance skip the echo of its own broadcasts. Replication is best-effort (auto-ack, transient). With a `Dialer` the subscription and publisher re-establish after a broker restart.

```go
hub := melodyhttp.NewServerSentEventHub()
backplane := amqp.NewServerSentEventBackplane(amqp.ServerSentEventBackplaneConfig{
    Connection: connection,
    Dialer:     provider.Dialer(dsn),
    Hub:        hub,
})
defer backplane.Close()
```

`NewServerSentEventBackplane` calls `hub.SetBackplane` itself, so after construction `hub.Broadcast(...)` replicates automatically. `Close` tears the subscription down and closes only a connection the backplane itself dialed (never the one you passed in). The same hub backs the WebSocket integration, so both transports fan out cluster-wide.

## Footguns & caveats

- The transport uses one channel for publishing and one for consuming, created lazily. `Ack`/`Nack` operate on the consume channel, so they must be called from the process that received the message.
- With auto-reconnect, a message received just before a reconnect carries a delivery tag from the old channel. Each delivery is stamped with the consume-channel generation it arrived on; once the channel rotates, an `Ack`/`Nack` for an older generation is skipped as a no-op rather than acking the stale tag against the new channel — which would otherwise ack an unrelated delivery (silent loss) or trip a 406 channel close. The broker redelivers the still-unacked message. Combined with the at-least-once requeue, this means **handlers must be idempotent**.
- Behind a load balancer the consumer runs on several instances as competing consumers on the same queue; this is the normal AMQP fan-out and is safe. Because redelivery can land a message on a different instance than first processed it, idempotency must be keyed on the message, not on local state.
- Queue/exchange topology is declared on first use and assumed stable. Redeclaring with conflicting arguments will fail at the broker.
- Stamps are process-local and are not serialized over the wire; only the message body and its type name cross the broker. The transport adds a `DeliveryStamp` and a `messagebus.ReceivedStamp` on receive.
- The integration test (`transport_test.go`) is skipped unless `AMQP_DSN` is set. A RabbitMQ service is available in `.dev/docker/docker-compose.yml`.
