# MESSAGEBUS

The [`messagebus`](../../messagebus) package provides Melody's transport-agnostic message bus: messages wrapped in envelopes with stamps, a configurable middleware stack, typed handler registration, pluggable transports, and a long-running consumer command. It is the asynchronous, durable counterpart to the in-process [`event`](EVENT.md) dispatcher.

## Scope

The message bus is opt-in. Unlike core services, it is not wired by the application container; userland builds the bus, registers handlers, and exposes it through its own module (see the example application). Dispatch and consumption both require a runtime instance for execution context and logging.

## Subpackages

- [`messagebus/contract`](../../messagebus/contract)  
  Public contracts for envelopes, stamps, handlers, the bus, middleware, the handler locator, and transports.

## Responsibilities

- Wrap messages and carry metadata:
    - [`Envelope`](../../messagebus/contract/envelope.go)
    - [`Stamp`](../../messagebus/contract/envelope.go)
    - [`NewEnvelope`](../../messagebus/envelope.go)
    - [`EnsureEnvelope`](../../messagebus/envelope.go)
    - built-in stamps [`BusNameStamp`](../../messagebus/stamp.go), [`SentStamp`](../../messagebus/stamp.go), [`ReceivedStamp`](../../messagebus/stamp.go), [`HandledStamp`](../../messagebus/stamp.go)
    - [`LastStampOfType`](../../messagebus/stamp.go)
- Dispatch through a middleware stack:
    - [`Bus`](../../messagebus/contract/bus.go)
    - [`Manager`](../../messagebus/manager.go)
    - [`NewManager`](../../messagebus/manager.go)
    - [`Middleware`](../../messagebus/contract/middleware.go) / [`StackNext`](../../messagebus/contract/middleware.go)
- Locate and run handlers:
    - [`MessageHandler`](../../messagebus/contract/handler.go)
    - [`HandlerLocator`](../../messagebus/contract/locator.go)
    - [`NewHandlerLocator`](../../messagebus/locator.go)
    - [`RegisterHandler`](../../messagebus/locator.go)
    - [`NewHandleMessageMiddleware`](../../messagebus/middleware_handle.go)
- Route messages to transports:
    - [`Transport`](../../messagebus/contract/transport.go)
    - [`TransportRouting`](../../messagebus/middleware_send.go)
    - [`NewSendMessageMiddleware`](../../messagebus/middleware_send.go)
    - [`InMemoryTransport`](../../messagebus/transport_in_memory.go)
- Consume asynchronously:
    - [`ConsumeCommand`](../../messagebus/consume_command.go)
    - [`NewConsumeCommand`](../../messagebus/consume_command.go)
- Provide container resolver helpers:
    - [`ServiceBus`](../../messagebus/service_resolver.go)
    - [`ServiceHandlerLocator`](../../messagebus/service_resolver.go)
    - [`BusMustFromContainer`](../../messagebus/service_resolver.go)
    - [`BusMustFromResolver`](../../messagebus/service_resolver.go)

## How dispatch flows

A bus is a [`Manager`](../../messagebus/manager.go) holding an ordered middleware stack. `Dispatch` wraps the message in an envelope, stamps it with the bus name, and runs the stack. Each [`Middleware`](../../messagebus/contract/middleware.go) may inspect or replace the envelope and decides whether to call `next`.

The two built-in middlewares compose the common pattern:

- [`NewSendMessageMiddleware`](../../messagebus/middleware_send.go) routes a message to a transport by its Go type. If a route matches and the envelope was not already received from a transport, it sends and stops the stack (asynchronous handling). Otherwise it calls `next`.
- [`NewHandleMessageMiddleware`](../../messagebus/middleware_handle.go) runs every handler registered for the message type and stamps the envelope as handled.

A dispatch bus typically uses `send` then `handle`: routed messages go to the transport, unrouted messages are handled inline (synchronous). A consume bus uses `handle` only, because the consumer feeds it envelopes already received from a transport.

## Container integration

The package defines the service names:

- [`ServiceBus`](../../messagebus/service_resolver.go) (`"service.messagebus.bus"`)
- [`ServiceHandlerLocator`](../../messagebus/service_resolver.go) (`"service.messagebus.handler_locator"`)

These are not registered by the framework. Userland registers `ServiceBus` to its dispatch bus once, so that HTTP handlers, services, or commands can resolve and dispatch:

```go
registrar.RegisterService(
	messagebus.ServiceBus,
	func(resolver containercontract.Resolver) (messagebuscontract.Bus, error) {
		return dispatchBus, nil
	},
)
```

Any service, handler, or command then resolves the same bus and dispatches:

```go
bus := messagebus.BusMustFromResolver(resolver)
bus.Dispatch(runtimeInstance, WelcomeEmail{UserId: 1})
```

`BusMustFromContainer` is the equivalent from a `Container` (for example `runtimeInstance.Container()` inside a handler). The factory result is cached, so every resolver shares the one configured bus — configure it once, dispatch from many places. The example application wires this end-to-end: the bus is registered in [`.example/config/service.go`](../../.example/config/service.go) and resolved in the [`/messagebus/demo`](../../.example/handler/messagebus_demo_handler.go) HTTP handler. Set `AMQP_DSN` before launching the example to route the bus over RabbitMQ via the [`amqp`](../../../integrations/amqp/v3) integration instead of the in-process transport.

## Usage

Building a bus, registering a typed handler, and dispatching:

```go
package main

import (
	"reflect"

	melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
	runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type WelcomeEmail struct {
	UserId  int
	Address string
}

func buildBus() (*melodymessagebus.Manager, *melodymessagebus.InMemoryTransport) {
	transport := melodymessagebus.NewInMemoryTransport(64)

	locator := melodymessagebus.NewHandlerLocator()
	melodymessagebus.RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message WelcomeEmail) error {
		return nil
	})

	routing := map[reflect.Type]melodymessagebus.TransportRouting{
		reflect.TypeOf(WelcomeEmail{}): {Name: "async", Transport: transport},
	}

	bus := melodymessagebus.NewManager(
		"default",
		melodymessagebus.NewSendMessageMiddleware(routing),
		melodymessagebus.NewHandleMessageMiddleware(locator),
	)

	return bus, transport
}
```

The routing map can also be built type-safely with [`NewRouting`](../../messagebus/routing.go) + [`RouteType[T]`](../../messagebus/routing.go), avoiding the `reflect.TypeOf` keys, and passed via [`NewSendMessageMiddlewareFromRouting`](../../messagebus/routing.go):

```go
routing := melodymessagebus.NewRouting()
melodymessagebus.RouteType[WelcomeEmail](routing, "async", transport)

bus := melodymessagebus.NewManager("default", melodymessagebus.NewSendMessageMiddlewareFromRouting(routing))
```

Consuming asynchronously is done with the [`ConsumeCommand`](../../messagebus/consume_command.go), registered as a CLI command:

```sh
app melody:messagebus:consume --transport=async
app melody:messagebus:consume --transport=async --limit=100
app melody:messagebus:consume --transport=async --concurrency=8
```

The consumer loops over the transport, dispatches each received envelope to a handle-only bus, and acknowledges on success or negatively acknowledges (with requeue) on failure. It shuts down cooperatively on `SIGINT`/`SIGTERM` or when the runtime context is cancelled. The transport's lifecycle is owned by the application, not the consumer — the consumer does **not** call `Close` on exit, so a transport shared between the dispatcher and the consumer in the same process keeps working after the consume command returns (the process exit releases a durable transport's connections). If the delivery channel closes without a cancelled context (for example a lost broker connection), the consumer returns an error rather than reporting a clean exit, so a supervisor can tell a crash from a graceful stop.

By default the consumer handles one message at a time (so the broker's prefetch only buffers). Pass `--concurrency=N` to run N worker goroutines reading the same transport; per-transport `Ack`/`Nack` stay serialized, so this is safe. On shutdown the consumer waits a bounded grace period (default 30s, set with `ConsumeCommand.WithShutdownGrace`) for in-flight handlers to drain, then returns even if a handler is still running — handlers are not context-aware, so this stops a wedged handler from blocking shutdown forever. Messages already received but not yet acked are redelivered on the next run (at-least-once), so handlers must be idempotent.

Poison messages (a delivery that can never decode) are nacked without requeue. With a durable transport this lands them in the configured dead-letter queue (enable `DeadLetter` on the AMQP transport); **without** a DLQ the broker discards them. Enable a DLQ in production so an undecodable message is retained for inspection rather than dropped.

A runnable end-to-end demonstration lives in the example application: [`messagebus:demo`](../../.example/cli/messagebus_demo_command.go), wired in [`.example/config/messagebus.go`](../../.example/config/messagebus.go).

## Footguns & caveats

- The bus is opt-in and userland-wired. The framework does not register a default bus, transport, or handler locator.
- [`InMemoryTransport`](../../messagebus/transport_in_memory.go) is process-local: a message dispatched in one process is not visible to a consumer in another process. Use it for tests and single-process demos; use a durable transport (for example AMQP) across processes. Behind a load balancer this is mandatory — every instance must publish to and consume from the same broker. Multiple consumer instances on one queue are competing consumers (the broker delivers each message to one of them), which is the intended scale-out pattern; combined with at-least-once redelivery it means a message may be processed on a different instance than first received it, so handler idempotency must not rely on instance-local state.
- [`RegisterHandler`](../../messagebus/locator.go) keys handlers by the exact Go type of the message, including pointer vs value. Dispatch the same type you registered.
- [`NewSendMessageMiddleware`](../../messagebus/middleware_send.go) stops the stack after a successful send, so handle middleware placed after it does not run for routed messages. This is the intended synchronous/asynchronous split.
- The consumer dispatches one message at a time per invocation; run multiple consumers for parallelism.
- Retries are **at-least-once**: a durable transport that carries the redelivery count by re-publishing (the AMQP binding) can, on a crash between the re-publish and the original's ack, redeliver the original alongside the re-published copy. Handlers must be idempotent. The redelivery count stamped on an exhausted/dead-lettered message is the number of *redeliveries*, which is one less than the number of handler *attempts*.
- The retry backoff is capped and overflow-safe. [`InMemoryTransport`](../../messagebus/transport_in_memory.go) honors a `DelayStamp` by re-pushing after the delay (it no longer hot-retries), and drops a requeue if its buffer is full or the transport is closed — acceptable for a dev transport, but another reason to use a durable transport in production.

## Userland API

### Contracts (`messagebus/contract`)

- [`type Stamp`](../../messagebus/contract/envelope.go)
- [`type Envelope`](../../messagebus/contract/envelope.go)
- [`type MessageHandler`](../../messagebus/contract/handler.go)
- [`type HandlerLocator`](../../messagebus/contract/locator.go)
- [`type StackNext`](../../messagebus/contract/middleware.go)
- [`type Middleware`](../../messagebus/contract/middleware.go)
- [`type Bus`](../../messagebus/contract/bus.go)
- [`type Transport`](../../messagebus/contract/transport.go)

### Implementations (`messagebus`)

- [`NewEnvelope(message any, stamps ...messagebuscontract.Stamp) messagebuscontract.Envelope`](../../messagebus/envelope.go)
- [`EnsureEnvelope(message any) messagebuscontract.Envelope`](../../messagebus/envelope.go)
- [`type BusNameStamp`](../../messagebus/stamp.go), [`type SentStamp`](../../messagebus/stamp.go), [`type ReceivedStamp`](../../messagebus/stamp.go), [`type HandledStamp`](../../messagebus/stamp.go)
- [`type RedeliveryStamp`](../../messagebus/stamp.go), [`type DelayStamp`](../../messagebus/stamp.go) — retry metadata carried on a requeued envelope
- [`LastStampOfType[T messagebuscontract.Stamp](envelope) (T, bool)`](../../messagebus/stamp.go)
- [`RedeliveryCount(envelope messagebuscontract.Envelope) int`](../../messagebus/stamp.go) — the number of redeliveries so far, for a handler inspecting retry attempts
- [`type HandlerLocator`](../../messagebus/locator.go)
    - [`NewHandlerLocator() *HandlerLocator`](../../messagebus/locator.go)
    - [`RegisterHandler[T any](locator *HandlerLocator, handle func(runtimecontract.Runtime, T) error)`](../../messagebus/locator.go)
- [`type Manager`](../../messagebus/manager.go)
    - [`NewManager(name string, middlewares ...messagebuscontract.Middleware) *Manager`](../../messagebus/manager.go)
- [`NewHandleMessageMiddleware(locator messagebuscontract.HandlerLocator) messagebuscontract.Middleware`](../../messagebus/middleware_handle.go)
- [`type HandleOptions`](../../messagebus/middleware_handle.go) (`RequireHandler bool`)
    - [`NewHandleMessageMiddlewareWithOptions(locator messagebuscontract.HandlerLocator, options HandleOptions) messagebuscontract.Middleware`](../../messagebus/middleware_handle.go)
- [`type TransportRouting`](../../messagebus/middleware_send.go)
    - [`NewSendMessageMiddleware(routingByType map[reflect.Type]TransportRouting) messagebuscontract.Middleware`](../../messagebus/middleware_send.go)
- [`type Routing`](../../messagebus/routing.go) — type-safe routing builder
    - [`NewRouting() *Routing`](../../messagebus/routing.go)
    - [`RouteType[T any](routing *Routing, name string, transport messagebuscontract.Transport) *Routing`](../../messagebus/routing.go)
    - [`NewSendMessageMiddlewareFromRouting(routing *Routing) messagebuscontract.Middleware`](../../messagebus/routing.go)
- [`type InMemoryTransport`](../../messagebus/transport_in_memory.go)
    - [`NewInMemoryTransport(bufferSize int) *InMemoryTransport`](../../messagebus/transport_in_memory.go)
    - [`(*InMemoryTransport).WithLogger(logger loggingcontract.Logger) *InMemoryTransport`](../../messagebus/transport_in_memory.go)
- [`type ConsumeCommand`](../../messagebus/consume_command.go)
    - [`NewConsumeCommand(bus messagebuscontract.Bus, transports map[string]messagebuscontract.Transport) *ConsumeCommand`](../../messagebus/consume_command.go)
    - [`NewConsumeCommandWithRetry(bus messagebuscontract.Bus, transports map[string]messagebuscontract.Transport, retryPolicy RetryPolicy) *ConsumeCommand`](../../messagebus/consume_command.go)
    - [`(*ConsumeCommand).WithShutdownGrace(grace time.Duration) *ConsumeCommand`](../../messagebus/consume_command.go)
- [`type RetryPolicy`](../../messagebus/consume_command.go) (`MaxRetries int`, `BaseDelay time.Duration`, `FailureTransport messagebuscontract.Transport`)

### Container helpers (`messagebus`)

- [`const ServiceBus`](../../messagebus/service_resolver.go)
- [`const ServiceHandlerLocator`](../../messagebus/service_resolver.go)
- [`BusMustFromContainer(containercontract.Container) messagebuscontract.Bus`](../../messagebus/service_resolver.go)
- [`BusMustFromResolver(containercontract.Resolver) messagebuscontract.Bus`](../../messagebus/service_resolver.go)
- [`HandlerLocatorMustFromContainer(containercontract.Container) messagebuscontract.HandlerLocator`](../../messagebus/service_resolver.go)
- [`HandlerLocatorMustFromResolver(containercontract.Resolver) messagebuscontract.HandlerLocator`](../../messagebus/service_resolver.go)
