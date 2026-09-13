# EVENT

The [`event`](../../event) package provides Melody’s deterministic event system: event objects, listener/subscriber registration, and event dispatch with stable ordering and propagation control.

## Scope

The event dispatcher is used by framework components (for example, the HTTP kernel emits lifecycle events). Dispatch requires a runtime instance to provide execution context.

## Subpackages

- [`event/contract`](../../event/contract)  
  Public contracts for events, listeners, subscribers, dispatcher, and inspector types.

## Responsibilities

- Provide event objects:
    - [`Event`](../../event/event.go)
    - [`NewEvent`](../../event/event.go)
    - [`NewEventWithTimestamp`](../../event/event.go)
    - [`NewEventFromEvent`](../../event/event.go)
- Provide a deterministic dispatcher with listener/subscriber management and an inspector:
    - [`EventDispatcher`](../../event/event_dispatcher.go)
    - [`NewEventDispatcher`](../../event/event_dispatcher.go)
    - [`AddListener` / `RemoveListener`](../../event/event_dispatcher.go)
    - [`AddSubscriber` / `RemoveSubscriber`](../../event/event_dispatcher.go)
    - [`RegisteredEvents`](../../event/event_dispatcher.go) (implements [`EventDispatcherInspector`](../../event/contract/event_dispatcher_inspector.go))
- Provide a dispatcher adapter that wraps an [`eventcontract.EventDispatcher`](../../event/contract/event_dispatcher.go):
    - [`EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
    - [`NewEventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
- Provide container resolver helpers:
    - [`ServiceEventDispatcher`](../../event/service_resolver.go)
    - [`EventDispatcherMustFromContainer`](../../event/service_resolver.go)
    - [`EventDispatcherMustFromResolver`](../../event/service_resolver.go)

## Container integration

The package defines the service name:

- [`ServiceEventDispatcher`](../../event/service_resolver.go) (`"service.event.dispatcher"`)

In the default application wiring, this service is registered by the application container setup to resolve to the kernel’s dispatcher (see [`application/application_container.go`](../../application/application_container.go)).

## Usage

The example below demonstrates dispatching a named event with an arbitrary payload from code that already has access to the runtime and container.

```go
package main

import (
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/event"
	"github.com/precision-soft/melody/exception"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type ProductCreatedPayload struct {
	ProductId string
}

func dispatchProductCreated(
	runtimeInstance runtimecontract.Runtime,
	serviceContainer containercontract.Container,
	productId string,
) {
	dispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

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

## Required listeners and failing closed

A listener may stop propagation, and the listeners behind it then do not run. That is the point of the mechanism, and it is also a way to skip a listener that was never optional — the security firewall's access-control listener, for instance, which sits on `kernel.request` at priority 20. `RequiredListenerRegistrar` closes that hole:

- Marking a listener required, with `MarkListenerRequired(registration)`, declares it one that must not be skipped. If another listener stops propagation — or fails — while a required listener behind it has not run, the dispatch returns an [`event.RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go) instead of completing quietly; a failing listener's own error travels as that refusal's cause.
- The explicit opt-out, `MarkListenerMaySkipRequiredListeners(registration)`, is for a listener that knowingly short-circuits past required listeners.

Both marks default off, so a dispatcher whose listeners are unmarked behaves exactly as it would without the mechanism. The security firewall marks its own access-control listener automatically, so an application using it is covered with no code change.

The http kernel is the consumer that acts on the error. On `kernel.request` and `kernel.controller` it answers `500` and does **not** serve a response a listener produced when the dispatch skipped a required listener — a listener stopping propagation is entitled to answer the request, but not to answer it with access control never consulted. It type-asserts the returned error itself rather than reaching through the cause chain, so an application event dispatched *by* a listener, skipping a required listener of its own, stays an ordinary listener failure.

A listener that means to serve the request without authorization running should be registered below the access-control listener's priority. One that truly must run above it says so with `MarkListenerMaySkipRequiredListeners`.

`debug:events --verbose` renders a `required` column, so whether the guarantee is armed can be read off the running application rather than inferred from the wiring code.

## Footguns & caveats

- Event names are validated for the empty string only. Whitespace-only names are not normalized by design.
- Dispatching requires a runtime instance (`Dispatch` / `DispatchName`), because listeners execute in a runtime context.
- Listener ordering is deterministic: listeners are sorted by priority, and dispatch uses a snapshot of listeners for the duration of the dispatch. A listener registered during a dispatch therefore does not run in that dispatch.
- **The dispatch stops at the first failing listener.** The listeners behind it do not run, and the error is returned alongside the partially dispatched event; when one of the listeners that never ran was marked required, the returned error is the required-listener refusal with the failure as its cause, so the skip is what the caller sees first. The caller decides the policy for a partial dispatch; the http kernel fails the request closed for it.
- **A listener panic becomes an error.** It is recovered, logged with the panic value, its type and the stack, and returned as the dispatch error, so one misbehaving listener does not take the process down. An `*exception.ExitError` is the exception: it is re-raised unchanged, because the exit code belongs to whoever owns the process boundary.
- **An event is not reusable.** `Dispatch` returns the event object it was given, and propagation, once stopped, stays stopped: dispatching that same object again runs no listener at all. Build a fresh event per dispatch — `DispatchName` does.
- **`Event` is not safe for concurrent use.** The listeners of one dispatch run in sequence, so each sees the writes of the ones before it; dispatching one event value from two goroutines, or writing to it from a goroutine a listener started, races on the propagation flag.
- **A subscriber is registered once.** A second registration of the same subscriber is refused, and so is a second instance of a field-less subscriber type: every zero-size allocation in Go answers one address, so two such instances cannot be told apart and removing either would remove both. A type that needs two live instances must carry a field.
- A subscriber that declares no subscribed events, or an event name mapped to an empty list, is refused rather than registered as nothing.
- The dispatcher adapter hands each listener a **copy** of the event and mirrors a stopped propagation back onto the original. `RegisteredEvents` on the adapter reports only the listeners registered through the adapter.

## Userland API

### Contracts (`event/contract`)

- [`type Event`](../../event/contract/event.go)
- [`type EventListener`](../../event/contract/event_listener.go)
- [`type EventSubscriber`](../../event/contract/event_subscriber.go)
- [`type SubscribedEvent`](../../event/contract/event_subscriber.go)
- [`type ListenerRegistration`](../../event/contract/event_dispatcher.go)
- [`type EventDispatcher`](../../event/contract/event_dispatcher.go)
- [`type RequiredListenerRegistrar`](../../event/contract/event_dispatcher.go)
- [`type EventDispatcherInspector`](../../event/contract/event_dispatcher_inspector.go)
- [`type RegisteredEvent`](../../event/contract/event_dispatcher_inspector.go)
- [`type RegisteredListener`](../../event/contract/event_dispatcher_inspector.go)
- [`const RegisteredListenerSourceListener`](../../event/contract/event_dispatcher_inspector.go) (`"listener"`)
- [`const RegisteredListenerSourceSubscriber`](../../event/contract/event_dispatcher_inspector.go) (`"subscriber"`)

### Implementations (`event`)

- [`type Event`](../../event/event.go)
    - [`NewEvent(name string, payload any, clockInstance clockcontract.Clock) *Event`](../../event/event.go)
    - [`NewEventWithTimestamp(name string, payload any, timestamp time.Time) *Event`](../../event/event.go)
    - [`NewEventFromEvent(eventcontract.Event) *Event`](../../event/event.go)
- [`type EventDispatcher`](../../event/event_dispatcher.go)
    - [`NewEventDispatcher(clockcontract.Clock) *EventDispatcher`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).AddListener(eventName string, listener eventcontract.EventListener, priority int) eventcontract.ListenerRegistration`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RemoveListener(registration eventcontract.ListenerRegistration) bool`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).AddSubscriber(subscriber eventcontract.EventSubscriber)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RemoveSubscriber(subscriber eventcontract.EventSubscriber) int`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).Dispatch(runtimeInstance runtimecontract.Runtime, event eventcontract.Event) (eventcontract.Event, error)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (eventcontract.Event, error)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).RegisteredEvents() []eventcontract.RegisteredEvent`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).MarkListenerRequired(registration eventcontract.ListenerRegistration)`](../../event/event_dispatcher.go)
    - [`(*EventDispatcher).MarkListenerMaySkipRequiredListeners(registration eventcontract.ListenerRegistration)`](../../event/event_dispatcher.go)
- [`type EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
    - [`NewEventDispatcherAdapter(eventcontract.EventDispatcher) *EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
- [`type RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go)
    - [`NewRequiredListenerSkippedError(eventName string, stoppedByListenerName string) *RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go)
    - [`NewRequiredListenerSkippedErrorWithStoppedListenerFailure(eventName string, stoppedByListenerName string, cause error) *RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go)
    - [`NewRequiredListenerSkippedErrorWithCause(eventName string, failedListenerName string, cause error) *RequiredListenerSkippedError`](../../event/required_listener_skipped_error.go)

### Container helpers (`event`)

- [`const ServiceEventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromContainer(containercontract.Container) eventcontract.EventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromResolver(containercontract.Resolver) eventcontract.EventDispatcher`](../../event/service_resolver.go)
