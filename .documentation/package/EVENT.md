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

## Footguns & caveats

- Event names are validated for the empty string only. Whitespace-only names are not normalized by design.
- Dispatching requires a runtime instance (`Dispatch` / `DispatchName`), because listeners execute in a runtime context.
- Listener ordering is deterministic: listeners are sorted by priority, and dispatch uses a snapshot of listeners for the duration of the dispatch.

## Userland API

### Contracts (`event/contract`)

- [`type Event`](../../event/contract/event.go)
- [`type EventListener`](../../event/contract/event_listener.go)
- [`type EventSubscriber`](../../event/contract/event_subscriber.go)
- [`type SubscribedEvent`](../../event/contract/event_subscriber.go)
- [`type ListenerRegistration`](../../event/contract/event_dispatcher.go)
- [`type EventDispatcher`](../../event/contract/event_dispatcher.go)
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
- [`type EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)
    - [`NewEventDispatcherAdapter(eventcontract.EventDispatcher) *EventDispatcherAdapter`](../../event/event_dispatcher_adapter.go)

### Container helpers (`event`)

- [`const ServiceEventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromContainer(containercontract.Container) eventcontract.EventDispatcher`](../../event/service_resolver.go)
- [`EventDispatcherMustFromResolver(containercontract.Resolver) eventcontract.EventDispatcher`](../../event/service_resolver.go)
