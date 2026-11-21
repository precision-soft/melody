# EVENT

The [`event`](../../event) package provides Melody’s deterministic event system: event objects, listener/subscriber registration, and event dispatch with stable ordering and propagation control.

## Scope

The event dispatcher is used by framework components (for example, the HTTP kernel emits lifecycle events). Dispatch requires a runtime instance to provide execution context.

## Subpackages

- [`event/contract`](../../event/contract)\
  Public contracts for events, listeners, subscribers, dispatcher, and inspector types.

## Responsibilities

- Provide event objects:
    - [`Event`](../../event/event.go)
    - [`NewEvent`](../../event/event.go)
    - [`NewEventFromEvent`](../../event/event.go)
- Provide a deterministic dispatcher with listener/subscriber management and an inspector:
    - [`EventDispatcher`](../../event/event_dispatcher.go)
    - [`NewEventDispatcher`](../../event/event_dispatcher.go)
    - [`AddListener` / `RemoveListener`](../../event/event_dispatcher.go)
    - [`AddSubscriber` / `RemoveSubscriber`](../../event/event_dispatcher.go), by a registration
    - [`RegisteredEvents`](../../event/event_dispatcher.go), a method of [`eventcontract.EventDispatcher`](../../event/contract/event_dispatcher.go)
- Provide a dispatcher adapter that wraps an [`eventcontract.EventDispatcher`](../../event/contract/event_dispatcher.go):
    - [`EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
    - [`NewEventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
- Provide container resolver helpers:
    - [`ServiceEventDispatcher`](../../event/service_resolver.go)
    - [`EventDispatcherMustFromResolver`](../../event/service_resolver.go)

## Container integration

The package defines the service name:

- [`ServiceEventDispatcher`](../../event/service_resolver.go) (`"service.event.dispatcher"`)

In the default application wiring, this service is registered by the application container setup to resolve to the kernel’s dispatcher (see [`application/application_container.go`](../../application/application_container.go)).

## Usage

The example below demonstrates dispatching a named event with an arbitrary payload from code that holds the runtime.

```go
package main

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    "github.com/precision-soft/melody/v4/exception"
)

type ProductCreatedPayload struct {
    ProductId string
}

func dispatchProductCreated(
    runtimeInstance containercontract.Runtime,
    productId string,
) {
    dispatcher := event.EventDispatcherMustFromRuntime(runtimeInstance)

    _, dispatchErr := dispatcher.DispatchName(
        runtimeInstance,
        "product.created",
        ProductCreatedPayload{
            ProductId: productId,
        },
    )
    if nil != dispatchErr {
        exception.Panic(
            exception.NewError("failed to dispatch product.created event", nil, dispatchErr),
        )
    }
}
```

## Every listener runs, and the failures are joined

A dispatch runs every listener of the event in priority order. A listener that fails — a returned error or a recovered panic — does not end the dispatch: its failure is collected and the next listener runs. The answer is that failure, or every failure joined through `errors.Join` in priority order when several failed, each reachable through `errors.Is` and `errors.As`, the joined answer reading already logged (`exception.IsAlreadyLogged`) only when every failure does, so the caller's record names a failure no one logged beside one that was; a single failure is answered as itself, so its type reaches the caller unchanged. Two things still end a dispatch early: a listener that stops propagation, and the failure of a listener registered with `ListenerConfig{FailureEndsDispatch: true}`, after which no listener behind it runs. The framework declares its own gates so — the rate limit listener, the security resolution listener and the access control listener on `kernel.request` — because there the order is the protection: a listener below a gate that failed must not act on a request the gate never judged.

## Registering a listener

[`eventcontract.ListenerConfig`](../../event/contract/event_dispatcher.go) is what a registration declares, handed to `AddListener`, `InstallFloorListener` and a subscriber's `SubscribedEvent.Config()`: `Priority` (the highest first), `Name` (what inspection and the journal call the listener, the function's qualified name when empty), `Required`, `MaySkipRequiredListeners` and `FailureEndsDispatch`. The zero value is priority 0 with nothing declared. `RegisteredEvents` reports what each listener declared.

## Required listeners and failing closed

A listener may stop propagation, and the listeners behind it then do not run. That is the point of the mechanism, and it is also a way to skip a listener that was never optional — the security firewall's access-control listener, for instance, which sits on `kernel.request` at priority 20. `ListenerConfig.Required` closes that hole:

- A listener registered with `Required: true` must not be skipped. If another listener stops propagation — or a listener declared `FailureEndsDispatch` fails — while a required listener behind it has not run, the dispatch returns an [`event.RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go) instead of completing quietly; the dispatch's failures travel as that refusal's cause.
- The explicit opt-out, `MaySkipRequiredListeners: true`, is for a listener that knowingly short-circuits past required listeners. It licenses a stop, never a failure: a listener that fails and stops is refused as any other.

Both default off, so a dispatcher whose listeners declare nothing behaves exactly as it would without the mechanism. The security firewall registers its own access-control listener required, so an application using it is covered with no code change.

The http kernel is the consumer that acts on the error. On `kernel.request` and `kernel.controller` it answers `500` and does **not** serve a response a listener produced when the dispatch failed or skipped a required listener — a listener stopping propagation is entitled to answer the request, but not to answer it with access control never consulted, and a response set beside a failure is half made. A refusal a `kernel.request` listener made through `KernelRequestEvent.Refuse` before a later listener failed keeps its rendered status, unless a required listener was skipped or a second decision was attempted, which answer `500`. It type-asserts the returned error itself rather than reaching through the cause chain, so an application event dispatched *by* a listener, skipping a required listener of its own, stays an ordinary listener failure.

A listener that means to serve the request without authorization running should be registered below the access-control listener's priority. One that truly must run above it says so with `MaySkipRequiredListeners: true`.

`debug:events --verbose` renders a `required` column, so whether the guarantee is armed can be read off the running application rather than inferred from the wiring code.

## A payload that ends the dispatch

A listener receives the event and writes to its payload, and the payload has no reference back to the event wrapping it. A payload that implements [`PropagationStopper`](../../event/contract/event.go) reports its own stop: the dispatcher reads it before each listener and after the last, beside the event's own stop, so a required listener behind it is refused the same way. `http.KernelExceptionEvent` is one: its `SetResponse` ends the dispatch.

## The floor of an event

`InstallFloorListener(eventName, listener, config)` gives the bottom of one event's order to the listener that answers last: it registers it and reserves `config.Priority` in one step, and from then on `AddListener` refuses by name, on that event, a listener at or below that priority, which could never run; a subscriber's listener goes through the same door. A listener registered before the floor stands and keeps its place, and a second installation adds nothing and answers false. The http kernel installs its built-in `kernel.exception` listener this way; it answers after every other listener, so its answer stands when an application listener before it failed.

## Footguns & caveats

- Event names are validated for the empty string only. Whitespace-only names are not normalized by design.
- Dispatching requires a runtime instance (`Dispatch` / `DispatchName`), because listeners execute in a runtime context.
- Listener ordering is deterministic: listeners are sorted by priority, and dispatch uses a snapshot of listeners for the duration of the dispatch. A listener registered during a dispatch therefore does not run in that dispatch.
- **A failing listener does not stop the dispatch.** The listeners behind it run, and the caller receives the failures joined beside the dispatched event. A release that must happen still belongs in the store the entity lives in (a cascade, a transaction): a later listener runs, but it runs after a failure it may need to know of. A listener whose failure must end the dispatch declares `FailureEndsDispatch`.
- **A listener panic becomes an error.** It is recovered, logged with the panic value, its type and the stack — unless the panic value already reports itself logged —, and joined into the dispatch's answer as any failure, the listeners behind it running, so one misbehaving listener does not take the process down. An `*exception.ExitError` is the exception: it is re-raised unchanged, because the exit code belongs to whoever owns the process boundary.
- **An event is not reusable.** `Dispatch` returns the event object it was given, and propagation, once stopped, stays stopped: dispatching that same object again runs no listener at all. Build a fresh event per dispatch — `DispatchName` does.
- **`Event` is not safe for concurrent use.** The listeners of one dispatch run in sequence, so each sees the writes of the ones before it; dispatching one event value from two goroutines, or writing to it from a goroutine a listener started, races on the propagation flag.
- **A subscriber is installed by a registration.** `AddSubscriber` files an installation under a [`SubscriberRegistration`](../../event/contract/event_dispatcher.go) the dispatcher issues and answers it, and `RemoveSubscriber` takes it and removes that one installation. A subscriber's pointer is not an identity: a subscriber that carries no fields occupies no memory, and every zero-size allocation in Go answers one address, so two instances of such a type are one pointer. Filed under a registration, one subscriber installs any number of times, a value subscriber installs like any other, and the dispatcher keeps no reference to the subscriber of its own, only the listeners it installed, which keep it reachable when they are its methods.
- A subscriber that declares no subscribed events, or an event name mapped to an empty list, is refused rather than registered as nothing.
- The dispatcher adapter hands each listener **the dispatched event itself**, exactly as the plain dispatcher does, so a custom event type stays type-assertable through the adapter and a field a listener writes is seen by the caller. `RegisteredEvents` on the adapter reports only the listeners registered through the adapter.

## Userland API

### Contracts (`event/contract`)

- [`type Event`](../../event/contract/event.go)
- [`type EventListener`](../../event/contract/event_listener.go)
- [`type EventSubscriber`](../../event/contract/event_subscriber.go)
- [`type SubscribedEvent`](../../event/contract/event_subscriber.go)
- [`type ListenerRegistration`](../../event/contract/event_dispatcher.go)
- [`type SubscriberRegistration`](../../event/contract/event_dispatcher.go)
- [`type EventDispatcher`](../../event/contract/event_dispatcher.go)
- [`type ListenerConfig`](../../event/contract/event_dispatcher.go)
- [`type PropagationStopper`](../../event/contract/event.go)
- [`type RegisteredEvent`](../../event/contract/event_dispatcher_inspector.go)
- [`type RegisteredListener`](../../event/contract/event_dispatcher_inspector.go)
- [`const RegisteredListenerSourceListener`](../../event/contract/event_dispatcher_inspector.go) (`"listener"`)
- [`const RegisteredListenerSourceSubscriber`](../../event/contract/event_dispatcher_inspector.go) (`"subscriber"`)

### Implementations (`event`)

- [`type Event`](../../event/event.go)
    - [`NewEvent(name string, payload any, timestamp time.Time) *Event`](../../event/event.go)
    - [`NewEventFromEvent(eventcontract.Event) *Event`](../../event/event.go)
- [`type EventDispatcher`](../../event/event_dispatcher.go)
    - [`NewEventDispatcher(EventDispatcherConfig) *EventDispatcher`](../../event/event_dispatcher.go) — `EventDispatcherConfig{Clock}`, the system clock when nil, a typed nil refused
    - [`(*EventDispatcher).AddListener(eventName string, listener eventcontract.EventListener, config eventcontract.ListenerConfig) eventcontract.ListenerRegistration`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RemoveListener(registration eventcontract.ListenerRegistration) bool`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).AddSubscriber(subscriber eventcontract.EventSubscriber) eventcontract.SubscriberRegistration`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RemoveSubscriber(registration eventcontract.SubscriberRegistration) int`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).Dispatch(runtimeInstance containercontract.Runtime, event eventcontract.Event) (eventcontract.Event, error)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).DispatchName(runtimeInstance containercontract.Runtime, eventName string, payload any) (eventcontract.Event, error)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RegisteredEvents() []eventcontract.RegisteredEvent`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).InstallFloorListener(eventName string, listener eventcontract.EventListener, config eventcontract.ListenerConfig) (eventcontract.ListenerRegistration, bool)`](../../event/event_dispatcher.go)
- [`type EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
    - [`NewEventDispatcherAdapter(eventcontract.EventDispatcher) *EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go) — a nil dispatcher is refused
- [`type RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go)
    - [`NewRequiredListenerSkippedError(eventName string, stoppedByListenerName string, cause error) *RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go) — the cause joins the dispatch's failures, nil when none failed

### Container helpers (`event`)

- [`const ServiceEventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromRuntime(containercontract.Runtime) eventcontract.EventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromResolver(context.Context, containercontract.Resolver) eventcontract.EventDispatcher`](../../event/service_resolver.go)
