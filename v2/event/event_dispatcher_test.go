package event

import (
    "context"
    "errors"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/clock"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func testNewEventDispatcher() (*EventDispatcher, clockcontract.Clock) {
    clockInstance := clock.NewSystemClock()
    dispatcher := NewEventDispatcher(clockInstance)

    return dispatcher, clockInstance
}

func TestEventDispatcherStableOrderingForEqualPriorities(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    invoked := make([]string, 0)
    _ = dispatcher.AddListener(
        "test",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            invoked = append(invoked, "a")
            return nil
        },
        100,
    )
    _ = dispatcher.AddListener(
        "test",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            invoked = append(invoked, "b")
            return nil
        },
        100,
    )
    _ = dispatcher.AddListener(
        "test",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            invoked = append(invoked, "c")
            return nil
        },
        100,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.DispatchName(runtimeInstance, "test", nil)
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }

    if 3 != len(invoked) {
        t.Fatalf("expected 3 listeners invoked, got: %d", len(invoked))
    }

    if "a" != invoked[0] {
        t.Fatalf("expected first listener to be 'a', got: %s", invoked[0])
    }

    if "b" != invoked[1] {
        t.Fatalf("expected second listener to be 'b', got: %s", invoked[1])
    }

    if "c" != invoked[2] {
        t.Fatalf("expected third listener to be 'c', got: %s", invoked[2])
    }
}

type testSubscriber struct {
    events map[string][]eventcontract.SubscribedEvent
}

func (instance *testSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return instance.events
}

func TestEventDispatcher_AddListener_SortsByPriorityDescending(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    order := make([]int, 0)
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            order = append(order, 10)
            return nil
        },
        10,
    )
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            order = append(order, 30)
            return nil
        },
        30,
    )
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            order = append(order, 20)
            return nil
        },
        20,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 3 != len(order) {
        t.Fatalf("expected 3 calls, got %d", len(order))
    }
    if 30 != order[0] || 20 != order[1] || 10 != order[2] {
        t.Fatalf("unexpected order: %+v", order)
    }
}

func TestEventDispatcher_AddListener_PanicsOnInvalidInput(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    testhelper.AssertPanics(t, func() {
        _ = dispatcher.AddListener("", func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0)
    })

    testhelper.AssertPanics(t, func() {
        _ = dispatcher.AddListener("e", nil, 0)
    })
}

func TestEventDispatcher_Dispatch_PanicsOnNilEvent(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    testhelper.AssertPanics(
        t,
        func() {
            _, _ = dispatcher.Dispatch(runtimeInstance, nil)
        },
    )
}

type emptyNameEvent struct{}

func (instance *emptyNameEvent) Name() string {
    return ""
}

func (instance *emptyNameEvent) Payload() any {
    return nil
}

func (instance *emptyNameEvent) Timestamp() time.Time {
    return time.Now()
}

func (instance *emptyNameEvent) StopPropagation() {}

func (instance *emptyNameEvent) IsPropagationStopped() bool { return false }

func TestEventDispatcher_Dispatch_PanicsOnEmptyName(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    testhelper.AssertPanics(
        t,
        func() {
            _, _ = dispatcher.Dispatch(runtimeInstance, &emptyNameEvent{})
        },
    )
}

func TestEventDispatcher_DispatchName_PanicsOnEmptyName(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    testhelper.AssertPanics(
        t,
        func() {
            _, _ = dispatcher.DispatchName(runtimeInstance, "", nil)
        },
    )
}

func TestEventDispatcher_StopPropagation_SkipsRemainingListeners(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    calls := 0
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            calls++
            eventValue.StopPropagation()
            return nil
        },
        100,
    )
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            calls++
            return nil
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != calls {
        t.Fatalf("expected 1 call, got %d", calls)
    }
}

func TestEventDispatcher_ListenerError_IsWrappedWithContext(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    expectedErr := errors.New("listener error")
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return expectedErr
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected error")
    }

    exceptionValue, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", err)
    }

    if "event listener returned error" != exceptionValue.Message() {
        t.Fatalf("unexpected message: %q", exceptionValue.Message())
    }

    if nil == exceptionValue.Context() {
        t.Fatalf("expected context")
    }

    if "e" != exceptionValue.Context()["eventName"] {
        t.Fatalf("expected eventName in context")
    }
}

func TestEventDispatcher_ListenerPanic_IsConvertedToErrorWithContext(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("boom")
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected error")
    }

    exceptionValue, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected *exception.Error, got %T", err)
    }

    if "event listener panicked" != exceptionValue.Message() {
        t.Fatalf("unexpected message: %q", exceptionValue.Message())
    }

    if nil == exceptionValue.Context() {
        t.Fatalf("expected context")
    }

    if "e" != exceptionValue.Context()["eventName"] {
        t.Fatalf("expected eventName in context")
    }

    panicStackAny := exceptionValue.Context()["panicStack"]
    panicStack, ok := panicStackAny.(string)
    if false == ok {
        t.Fatalf("expected panicStack to be string, got %T", panicStackAny)
    }

    if "" == panicStack {
        t.Fatalf("expected panicStack in context")
    }
}

func TestEventDispatcher_AddSubscriber_HappyPathRegistersListeners(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    calls := 0

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "e": {
                NewSubscribedEvent(
                    func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                        calls++
                        return nil
                    },
                    10,
                ),
            },
        },
    }

    dispatcher.AddSubscriber(subscriber)

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != calls {
        t.Fatalf("expected 1 call, got %d", calls)
    }
}

func TestEventDispatcher_AddSubscriber_PanicsOnInvalidDefinitions(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(nil)
    })

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(&testSubscriber{events: nil})
    })

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "": {
                        NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0),
                    },
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": nil,
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {nil},
                },
            },
        )
    })

    testhelper.AssertPanics(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {NewSubscribedEvent(nil, 0)},
                },
            },
        )
    })
}

func TestEventDispatcher_RemoveListener_RemovesListenerAndKeepsOthers(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    invoked := make([]string, 0)

    listenerA := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked = append(invoked, "a")
        return nil
    }

    listenerB := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked = append(invoked, "b")
        return nil
    }
    listenerARegistration := dispatcher.AddListener(
        "e",
        listenerA,
        0,
    )
    _ = dispatcher.AddListener(
        "e",
        listenerB,
        0,
    )

    removed := dispatcher.RemoveListener(listenerARegistration)
    if false == removed {
        t.Fatalf("expected listener to be removed")
    }

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(invoked) {
        t.Fatalf("expected 1 listener invoked, got: %d", len(invoked))
    }

    if "b" != invoked[0] {
        t.Fatalf("expected remaining listener to be 'b', got: %s", invoked[0])
    }
}

func TestEventDispatcher_RemoveSubscriber_RemovesAllSubscriberListeners(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    invoked := make([]string, 0)

    listenerA := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked = append(invoked, "a")
        return nil
    }

    listenerB := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        invoked = append(invoked, "b")
        return nil
    }

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "e": {
                NewSubscribedEvent(listenerA, 0),
                NewSubscribedEvent(listenerB, 0),
            },
        },
    }

    dispatcher.AddSubscriber(subscriber)

    removedCount := dispatcher.RemoveSubscriber(subscriber)
    if 2 != removedCount {
        t.Fatalf("expected 2 removed listeners, got: %d", removedCount)
    }

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 0 != len(invoked) {
        t.Fatalf("expected no listeners invoked, got: %d", len(invoked))
    }
}

func TestEventDispatcher_Dispatch_UsesListenerSnapshot(t *testing.T) {
    serviceContainer := container.NewContainer()

    registerLoggerErr := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )
    if nil != registerLoggerErr {
        t.Fatalf("unexpected register error: %v", registerLoggerErr)
    }

    scope := serviceContainer.NewScope()

    runtimeInstance := runtime.New(
        context.Background(),
        scope,
        serviceContainer,
    )

    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    started := make(chan struct{})
    startedOnce := sync.Once{}
    continueDispatch := make(chan struct{})

    firstListener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        startedOnce.Do(func() {
            close(started)
        })
        <-continueDispatch

        return nil
    }

    var secondListenerCalled int32
    secondListener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        atomic.AddInt32(&secondListenerCalled, 1)

        return nil
    }

    dispatcher.AddListener("test.event", firstListener, 0)

    dispatchDone := make(chan struct{})
    go func() {
        defer close(dispatchDone)

        _, dispatchErr := dispatcher.DispatchName(runtimeInstance, "test.event", nil)
        if nil != dispatchErr {
            t.Errorf("unexpected dispatch error: %v", dispatchErr)
        }
    }()

    <-started

    dispatcher.AddListener("test.event", secondListener, 0)

    close(continueDispatch)
    <-dispatchDone

    if 0 != atomic.LoadInt32(&secondListenerCalled) {
        t.Fatalf("expected listener added during dispatch to not be called in the same dispatch")
    }

    _, secondDispatchErr := dispatcher.DispatchName(runtimeInstance, "test.event", nil)
    if nil != secondDispatchErr {
        t.Fatalf("unexpected dispatch error: %v", secondDispatchErr)
    }

    if 1 != atomic.LoadInt32(&secondListenerCalled) {
        t.Fatalf("expected listener to be called in subsequent dispatch")
    }

    closeErr := scope.Close()
    if nil != closeErr {
        t.Fatalf("unexpected scope close error: %v", closeErr)
    }

    containerCloseErr := serviceContainer.Close()
    if nil != containerCloseErr {
        t.Fatalf("unexpected container close error: %v", containerCloseErr)
    }
}
