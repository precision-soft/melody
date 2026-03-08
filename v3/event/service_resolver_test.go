package event

import (
    "testing"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
)

func TestEventServiceResolver_MustFromContainerAndResolver(t *testing.T) {
    serviceContainer := container.NewContainer()

    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    err := serviceContainer.Register(
        ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    resolvedFromContainer := EventDispatcherMustFromContainer(serviceContainer)
    if nil == resolvedFromContainer {
        t.Fatalf("expected dispatcher")
    }

    resolvedFromResolver := EventDispatcherMustFromResolver(serviceContainer)
    if nil == resolvedFromResolver {
        t.Fatalf("expected dispatcher")
    }
}
