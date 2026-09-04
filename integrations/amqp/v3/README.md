# Melody AMQP integration (v3)

A durable, transport-agnostic [`messagebus`](https://github.com/precision-soft/melody) transport backed by RabbitMQ (AMQP 0-9-1), built on [`rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go).

It implements `messagebus/contract.Transport`, so a Melody message bus can route messages to RabbitMQ for asynchronous, cross-process handling, and the `melody:messagebus:consume` worker can drain them.

## Version lines

This integration is v3-only (`github.com/precision-soft/melody/integrations/amqp/v3`); no v1 or v2 bindings are currently planned.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/amqp/v3
```

```go
import amqp "github.com/precision-soft/melody/integrations/amqp/v3"
```

## Parameters

`RegisterDefaultParameters` registers sensible defaults that userland may override:

| Parameter              | Constant            | Default                              |
|------------------------|---------------------|--------------------------------------|
| `melody.amqp.dsn`      | `ParameterDsn`      | `amqp://guest:guest@localhost:5672/` |
| `melody.amqp.prefetch` | `ParameterPrefetch` | `10`                                 |
| `melody.amqp.exchange` | `ParameterExchange` | _(unset; direct-to-queue)_           |

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

Or bundle all of it as a self-registering application module — one `RegisterModule` call wires the connection service, the named transport services, and the default parameters:

```go
app.RegisterModule(amqp.NewModule(amqp.ModuleConfig{
    Connection:            connection,
    Transports:            map[string]*amqp.Transport{"async": transport},
    WithDefaultParameters: true,
}))
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

When the consume channel is lost, the delivery loop re-dials and re-subscribes with bounded exponential backoff and resumes on the **same** output channel, so the `melody:messagebus:consume` worker keeps running across a broker restart. The publish path drops a dead channel and retries once. `Close` stops the reconnect loop and closes only connections the transport itself dialed — never the one you passed in.

The backoff window is tunable through a [`ReconnectConfig`](./reconnect_config.go) (`InitialBackoff`, `MaxBackoff`, `BackoffFactor`), defaulting to 1s → 30s doubling per attempt as [`DefaultReconnectConfig`](./reconnect_config.go) declares. Set it once for every transport and backplane the provider builds with [`WithReconnectConfig`](./connection.go) — the provider-level value only reaches the ones built through `provider.NewTransport(...)` / `provider.NewServerSentEventBackplane(...)`, not the standalone `amqp.NewTransport(...)` constructor — and override it for a single transport through `TransportConfig.Reconnect`:

```go
provider := amqp.NewProvider(amqp.WithReconnectConfig(amqp.NewReconnectConfig(2*time.Second, time.Minute, 1.5)))

transport := provider.NewTransport(amqp.TransportConfig{
	Connection: connection,
	Dialer:     provider.Dialer(dsn),
	Queue:      "welcome_email",
	Registry:   registry,
	Reconnect:  amqp.NewReconnectConfig(500*time.Millisecond, 10*time.Second, 2.0),
})
```

Unset fields keep their default, a `BackoffFactor` below 1 is refused (it would decay the wait toward zero and turn an outage into a reconnect storm), and the backoff only resets once a subscription has lived at least the initial backoff.

The **write** of a `Send` is bounded by `TransportConfig.PublishTimeout` (default 30s, sized like the close join to a full amqp handshake; a non-positive value takes the default). The amqp client discards the context it is handed for the write and holds its send locks across the blocking socket write, so a broker that stops reading its socket — a resource alarm, a half-dead peer — would otherwise hold that send, every later send and `Close` for good; the confirmation wait that follows the write keeps observing the caller's context as before. A write that outlives the timeout fails as a channel fault: on a connection the transport dialed itself (a `Dialer`) the connection is cut with a socket deadline — the one door the client leaves open once its locks are held — and the send's one retry redials; on a connection you passed in, which the transport must not cut, sends are refused at once until that write returns, by your hand or never, and `Close` reports the write it could not end. The same applies to a shared connection you close yourself: close it through `provider.Close(connection)`, which arms a deadline on the socket, because the client's plain `Close` is an RPC over the same locks and never returns over a wedged socket.

The publish channel runs in **publisher-confirm mode**: `Send` (and the retry re-publish) reports success only after the broker acknowledged the message, and a mandatory publish that comes back as unroutable (for example the queue was deleted) or is nacked by the broker (for example a `max-length` policy with `reject-publish`) surfaces as an error instead of being silently discarded. This also protects the retry path — a re-published message is confirmed before the original delivery is acked, so a broker-side discard can never drop a message between requeues.

## Dead-lettering and retries

When `DeadLetter` is `true`, the transport declares a dead-letter exchange (`<queue>.dlx`) and queue (`<queue>.dlq`), and points the main queue at it via `x-dead-letter-exchange`.

- A handler success acknowledges the delivery (`Ack`).
- A handler failure is retried under the consumer's `RetryPolicy` (max retries, base delay, optional dead-letter transport). The transport re-publishes the message carrying an incremented `x-redelivery-count` header, so the retry count survives across deliveries instead of relying on the broker's one-shot `redelivered` flag; a `DelayStamp` parks the message in a delay queue that dead-letters back to the main queue, so retries are spaced out by the configured backoff (see [Delay buckets](#delay-buckets) below). Once the retries are exhausted the message is `Nack`ed without requeue so the broker routes it to the dead-letter exchange.

### Delay buckets

RabbitMQ expires only the **head** of a queue, so a single delay queue holding heterogeneous per-message TTLs stalls short delays behind long ones. The transport therefore declares one queue per delay tier — `<queue>.delay.<milliseconds>ms` — each with a uniform **queue-level** `x-message-ttl` and `x-dead-letter-routing-key` back to the main queue. A delayed message is parked in the largest bucket that does not exceed its requested delay, so every message in a bucket shares one TTL and no head-of-queue expiry can block another.

The consequence to plan for: **an effective delay quantizes DOWN to its bucket.** With the default tiers a requested 45s delay fires at 5s, and 55m fires at 10m. [`TransportConfig.DelayBuckets`](./transport.go) overrides the tiers (ascending, between 1ms and the 32-bit millisecond wire limit, at most 8; a violation panics at construction — an over-limit bucket's queue name would promise a delay its clamped TTL cannot honour, and a sub-millisecond one collapses tiers onto a `0ms` queue) — add tiers wherever your retry policy needs the delay honoured more closely.

| Requested delay | Default bucket | Queue                     |
|-----------------|----------------|---------------------------|
| `< 5s`          | none           | `<queue>.delay`           |
| `5s` – `< 1m`   | `5s`           | `<queue>.delay.5000ms`    |
| `1m` – `< 10m`  | `1m`           | `<queue>.delay.60000ms`   |
| `10m` – `< 1h`  | `10m`          | `<queue>.delay.600000ms`  |
| `>= 1h`         | `1h`           | `<queue>.delay.3600000ms` |

A delay **below the smallest bucket** keeps the legacy `<queue>.delay` queue with a precise per-message `expiration`, where head-of-line waiting is bounded by that smallest bucket. That queue is always declared, so messages parked by an older deployment still drain. A per-message expiration is clamped to ~49.7 days, the range RabbitMQ's 32-bit millisecond parse allows.

- A delivery that cannot be decoded (missing or unknown `x-message-type`, bad body) is `Nack`ed without requeue. It is dead-lettered only when `DeadLetter` is enabled; otherwise the broker discards it (enable `DeadLetter` in production so undecodable deliveries are retained).

## Server-Sent Events backplane

`NewServerSentEventBackplane(ServerSentEventBackplaneConfig{...})` makes the core `http.ServerSentEventHub` fan its broadcasts out across every application instance behind a load balancer over a fanout exchange — without it, a `Broadcast` reaches only the clients connected to the instance that emitted it. Each instance binds its own exclusive, auto-deleted queue to the exchange (default `melody.sse`), so a published broadcast reaches every instance; the events of other instances are forwarded into the hub via `DeliverLocal`, and a tagged per-instance origin makes each instance skip the echo of its own broadcasts. Replication is best-effort in BOTH directions by design: the consumer runs auto-ack over a transient queue, and `Publish` runs no confirm mode — an event the broker discards after accepting the frame is gone with no error, so the hub's backplane-failure counter counts local publish refusals, not broker-side outcomes (the redis backplane behaves identically over pub/sub). A server-sent event is ephemeral fan-out state; the next event or a client refresh corrects a missed one. With a `Dialer` the subscription and publisher re-establish after a broker restart; without one, a lost connection stops the receive half permanently, and that terminal stop is reported through the configured `Logger` — or on the emergency stderr channel when none was configured. A publish write is bounded by `ServerSentEventBackplaneConfig.CallTimeout` (default 1s, the same budget the redis backplane gives one round trip; a non-positive value takes the default), because the amqp client discards the context a publish is handed and a broker that stops reading its socket would otherwise hold the write — and with it every later broadcast and the hub's shutdown, which waits for the publishes in flight — for good. A write that outlives the timeout fails and is not retried; on a connection the backplane dialed itself the connection is cut and redialed on the next publish, on a connection you passed in publishes are refused until that write returns. The backplane carries `Close() error` and no module registration door, and needs none: the hub owns it — `ServerSentEventHub.Shutdown`, which the container reaches through the hub's `Close`, closes the backplane it carries — so register the hub, or call `hub.Shutdown()` at shutdown; closing the backplane directly is allowed and idempotent.

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
- Most stamps are process-local and are not serialized over the wire; beyond the message body and its type name, exactly three pieces of stamp data cross the broker. A producer-assigned `messagebus.MessageIdStamp` is carried as the AMQP `MessageId` property and round-tripped back into a `MessageIdStamp` on receive, so a consumer can deduplicate redeliveries from an at-least-once producer (for example the outbox row id) and an application-driven requeue re-publishes under the same id. The redelivery count crosses in the `x-redelivery-count` header (surfaced as a `messagebus.RedeliveryStamp`) and the dead-letter attempt count in the `x-dead-letter-attempt-count` header (surfaced as a `messagebus.DeadLetterAttemptStamp`). All other stamps stay process-local. The transport also adds a `DeliveryStamp` and a `messagebus.ReceivedStamp` on receive.
- The integration test (`transport_test.go`) is skipped unless `AMQP_DSN` is set. A RabbitMQ service is available in `.dev/docker/docker-compose.yml`.
