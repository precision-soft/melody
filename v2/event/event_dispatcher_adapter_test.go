package event

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v2/clock"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func newEventDispatcherAdapterTestRuntime(t *testing.T) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    err := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}

type testAdapterSubscriber struct {
    events map[string][]eventcontract.SubscribedEvent
}

func (instance *testAdapterSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return instance.events
}

func TestEventDispatcherAdapter_StopPropagationIsMirroredToOriginalEvent(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)
    _ = adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()
            return nil
        },
        0,
    )

    eventInstance := NewEvent("e", nil, clockInstance)

    if true == eventInstance.IsPropagationStopped() {
        t.Fatalf("expected not stopped")
    }

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := adapter.Dispatch(runtimeInstance, eventInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == eventInstance.IsPropagationStopped() {
        t.Fatalf("expected stopped")
    }
}

func TestEventDispatcherAdapter_AddSubscriber_PanicsOnInvalidDefinitions(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(nil)
    })

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(&testAdapterSubscriber{events: nil})
    })

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "": {
                        NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0),
                    },
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": nil,
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {nil},
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {NewSubscribedEvent(nil, 0)},
                },
            },
        )
    })
}

func TestEventDispatcherAdapter_Dispatch_ReturnsErrorOnNilEvent(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    testhelper.AssertPanics(
        t,
        func() {
            _, _ = adapter.Dispatch(nil, nil)
        },
    )
}

func TestEventDispatcherAdapter_DispatchName_ReturnsErrorOnEmptyName(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    testhelper.AssertPanics(
        t,
        func() {
            _, _ = adapter.DispatchName(nil, "", nil)
        },
    )
}

func TestEventDispatcher_DispatchName_PayloadIsPreserved(t *testing.T) {
    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    var receivedPayload any

    dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            receivedPayload = eventValue.Payload()
            return nil
        },
        0,
    )

    payload := map[string]any{"a": 1}

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.DispatchName(runtimeInstance, "e", payload)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if nil == receivedPayload {
        t.Fatalf("expected payload")
    }
}

func TestEventDispatcherAdapter_RemoveListener_RemovesListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    invoked := 0

    listener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked++
        return nil
    }
    listenerRegistration := adapter.AddListener(
        "e",
        listener,
        0,
    )

    removed := adapter.RemoveListener(listenerRegistration)
    if false == removed {
        t.Fatalf("expected listener to be removed")
    }

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := adapter.DispatchName(runtimeInstance, "test.event", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 0 != invoked {
        t.Fatalf("expected 0 invocations, got: %d", invoked)
    }
}

func TestEventDispatcherAdapter_RemoveSubscriber_RemovesAllSubscriberListeners(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher, clockInstance)

    invoked := 0

    listenerA := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked++
        return nil
    }

    listenerB := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked++
        return nil
    }

    subscriber := &testAdapterSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "e": {
                NewSubscribedEvent(listenerA, 0),
                NewSubscribedEvent(listenerB, 0),
            },
        },
    }

    adapter.AddSubscriber(subscriber)

    removedCount := adapter.RemoveSubscriber(subscriber)
    if 2 != removedCount {
        t.Fatalf("expected 2 removed listeners, got: %d", removedCount)
    }

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := adapter.DispatchName(runtimeInstance, "test.event", nil)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 0 != invoked {
        t.Fatalf("expected 0 invocations, got: %d", invoked)
    }
}
