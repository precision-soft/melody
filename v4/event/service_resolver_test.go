package event

import (
    "testing"

    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

func TestEventServiceResolver_MustFromContainerAndResolver(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    err := serviceContainer.Register(
        ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    resolvedFromContainer := EventDispatcherMustFromContainer(t.Context(), serviceContainer)
    if nil == resolvedFromContainer {
        t.Fatalf("expected dispatcher")
    }

    resolvedFromResolver := EventDispatcherMustFromResolver(t.Context(), serviceContainer)
    if nil == resolvedFromResolver {
        t.Fatalf("expected dispatcher")
    }
}
