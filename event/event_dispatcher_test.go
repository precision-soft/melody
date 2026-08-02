package event

import (
    "context"
    "errors"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/clock"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

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

    testhelper.AssertPanicsWithError(t, func() {
        _ = dispatcher.AddListener("", func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0)
    }, "event name is required to add a listener")

    testhelper.AssertPanicsWithError(t, func() {
        _ = dispatcher.AddListener("e", nil, 0)
    }, "event listener is required to add a listener")
}

func TestEventDispatcher_Dispatch_PanicsOnNilEvent(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = dispatcher.Dispatch(runtimeInstance, nil)
        },
        "event may not be nil",
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

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = dispatcher.Dispatch(runtimeInstance, &emptyNameEvent{})
        },
        "event name may not be empty",
    )
}

func TestEventDispatcher_DispatchName_PanicsOnEmptyName(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = dispatcher.DispatchName(runtimeInstance, "", nil)
        },
        "event name may not be empty",
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

func TestEventDispatcher_StopBeforeRequiredListener_FailsClosed(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    requiredRan := false

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()
            return nil
        },
        100,
    )
    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requiredRan = true
            return nil
        },
        0,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected a fail-closed error when propagation stopped before a required listener")
    }
    if true == requiredRan {
        t.Fatalf("the required listener must not have run")
    }
}

func TestEventDispatcher_MaySkipRequiredListeners_RestoresProceed(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    stopperRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()
            return nil
        },
        100,
    )
    dispatcher.MarkListenerMaySkipRequiredListeners(stopperRegistration)

    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("a stopping listener marked may-skip must not fail closed, got %v", err)
    }
}

func TestEventDispatcher_StopAfterRequiredListener_DoesNotFail(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        100,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()
            return nil
        },
        50,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("stopping propagation after a required listener already ran must not fail, got %v", err)
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

/* @info an error-shaped panic value travels as the cause of the dispatcher's wrapper: kept only in the context slot it collapsed to its bare message at the render boundary, so the context map and the cause chain of the very error the listener panicked with reached no record and no caller */
func TestEventDispatcher_ListenerPanicWithError_CarriesItAsCause(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    rootCause := exception.NewError("redis dial failed", nil, nil)
    panicErr := exception.NewError(
        "cache write failed",
        map[string]any{"backend": "redis:6379"},
        rootCause,
    )

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exception.Panic(panicErr)
            return nil
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected error")
    }

    if false == errors.Is(err, panicErr) {
        t.Fatalf("expected the panic error to travel as the cause of the returned wrapper")
    }

    if false == errors.Is(err, rootCause) {
        t.Fatalf("expected the panic error's own cause chain to stay reachable")
    }
}

/* @info a typed-nil error panic value reads as the no-cause it means instead of feeding a nil receiver into the cause chain */
func TestEventDispatcher_ListenerPanicWithTypedNilError_YieldsNoCause(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            var typedNil *exception.Error
            panic(typedNil)
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

    if nil != exceptionValue.Unwrap() {
        t.Fatalf("expected no cause for a typed-nil panic value, got %v", exceptionValue.Unwrap())
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

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(nil)
    }, "event subscriber may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(&testSubscriber{events: nil})
    }, "subscribed events may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "": {
                        NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0),
                    },
                },
            },
        )
    }, "event name may not be empty")

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": nil,
                },
            },
        )
    }, "subscribed event list may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {nil},
                },
            },
        )
    }, "subscribed event may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        dispatcher.AddSubscriber(
            &testSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {NewSubscribedEvent(nil, 0)},
                },
            },
        )
    }, "subscribed event listener is required")
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

type firstZeroSizeSubscriber struct{}

func (instance *firstZeroSizeSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "zero.size.first": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

type secondZeroSizeSubscriber struct{}

func (instance *secondZeroSizeSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "zero.size.second": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

func testHasRegisteredEventWithListeners(registeredEvents []eventcontract.RegisteredEvent, eventName string) bool {
    for _, registeredEvent := range registeredEvents {
        if eventName == registeredEvent.EventName && 0 < len(registeredEvent.Listeners) {
            return true
        }
    }

    return false
}

func TestEventDispatcher_RemoveSubscriber_DistinctZeroSizeSubscribersKeepTheirOwnListeners(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    first := &firstZeroSizeSubscriber{}
    second := &secondZeroSizeSubscriber{}

    dispatcher.AddSubscriber(first)
    dispatcher.AddSubscriber(second)

    removedCount := dispatcher.RemoveSubscriber(first)
    if 1 != removedCount {
        t.Fatalf("expected 1 removed listener, got: %d", removedCount)
    }

    registeredEvents := dispatcher.RegisteredEvents()

    if true == testHasRegisteredEventWithListeners(registeredEvents, "zero.size.first") {
        t.Fatalf("expected the removed subscriber to lose its listener")
    }

    if false == testHasRegisteredEventWithListeners(registeredEvents, "zero.size.second") {
        t.Fatalf("expected the other zero size subscriber to keep its listener")
    }
}

/* @info a listener that stops propagation and fails in the same call skipped the required listeners behind it just as decisively as one that only stopped, but its own failure was returned first and the required scan never ran: the caller learned that a listener had failed and never that access control had been skipped */
func TestEventDispatcher_StoppingListenerThatAlsoFails_StillReportsTheSkippedRequiredListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    requiredRan := false

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return errors.New("the listener's own failure")
        },
        100,
    )
    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requiredRan = true

            return nil
        },
        0,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))

    if true == requiredRan {
        t.Fatalf("the required listener must not have run")
    }

    if _, ok := err.(*RequiredListenerSkippedError); false == ok {
        t.Fatalf("expected a RequiredListenerSkippedError, got: %T (%v)", err, err)
    }
}

/* @info the stopping listener's own failure is what a dispatch reports when nothing required was skipped: the required scan must not turn every stopping failure into a skipped-listener report */
func TestEventDispatcher_StoppingListenerThatAlsoFails_ReportsItsOwnFailureWhenNothingRequiredWasSkipped(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return errors.New("the listener's own failure")
        },
        100,
    )
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected the listener's own failure")
    }

    if _, ok := err.(*RequiredListenerSkippedError); true == ok {
        t.Fatalf("expected the listener's own failure, not a skipped-required-listener report")
    }
}

/* @info the propagation test opens the iteration rather than closing it: an event that arrived already stopped — the object a previous dispatch returned — ran the first listener anyway and then had that listener named as the one that stopped it */
func TestEventDispatcher_EventThatArrivedStopped_RunsNoListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    firstRan := false

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            firstRan = true

            return nil
        },
        100,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    eventValue := NewEvent("e", nil, clockInstance)
    eventValue.StopPropagation()

    _, err := dispatcher.Dispatch(runtimeInstance, eventValue)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if true == firstRan {
        t.Fatalf("an event that arrived stopped must run no listener")
    }
}

/* @info the required listeners of an event that arrived stopped are skipped by nobody in particular, and the dispatch owes the same refusal it owes a stopping listener */
func TestEventDispatcher_EventThatArrivedStopped_ReportsTheSkippedRequiredListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    eventValue := NewEvent("e", nil, clockInstance)
    eventValue.StopPropagation()

    _, err := dispatcher.Dispatch(runtimeInstance, eventValue)

    if _, ok := err.(*RequiredListenerSkippedError); false == ok {
        t.Fatalf("expected a RequiredListenerSkippedError, got: %T (%v)", err, err)
    }
}

/* @info a typed nil handed back by a listener is a non-nil interface around a nil value: read as a failure it aborted the dispatch, skipped every listener behind it and failed the request closed for a listener that reported success */
func TestEventDispatcher_TypedNilListenerError_ReadsAsTheSuccessItMeans(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    secondRan := false

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            var typedNilErr *testTypedNilError

            return typedNilErr
        },
        100,
    )
    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            secondRan = true

            return nil
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if false == secondRan {
        t.Fatalf("expected the dispatch to continue past a listener that reported success")
    }
}

type testTypedNilError struct {
}

func (instance *testTypedNilError) Error() string {
    return "typed nil"
}

/* @info the failure travels unlogged and unmarked: the record written by the dispatcher carried the listener's identity but never the error, and marking the wrapper as logged suppressed the caller's own record — the one that renders the cause chain — so the reason a request failed closed existed in no log at all */
func TestEventDispatcher_ListenerError_TravelsUnmarkedWithItsCause(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    listenerErr := errors.New("the database is down")

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerErr
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected the listener failure")
    }

    exceptionErr, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected an exception error, got: %T", err)
    }

    if true == exceptionErr.AlreadyLogged() {
        t.Fatalf("the listener failure must reach the caller's logging unmarked")
    }

    if false == errors.Is(err, listenerErr) {
        t.Fatalf("the listener's own error must survive as the cause")
    }
}

/* @info a mark that lands nowhere left the fail-closed guarantee unarmed while reporting that it had been applied, and the caller had no way to tell */
func TestEventDispatcher_MarkListenerRequired_RefusesAnUnknownRegistration(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    registration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.MarkListenerRequired(
                eventcontract.ListenerRegistration{
                    EventName:  registration.EventName,
                    ListenerId: registration.ListenerId + 1,
                },
            )
        },
        "event listener registration is not registered",
    )
}

/* @info the marks a listener carries reach inspection: an unarmed fail-closed guarantee used to look exactly like an armed one */
func TestEventDispatcher_RegisteredEvents_ReportsTheRequiredListenerMarks(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    registeredEvents := dispatcher.RegisteredEvents()
    if 1 != len(registeredEvents) {
        t.Fatalf("expected 1 registered event, got: %d", len(registeredEvents))
    }

    if 1 != len(registeredEvents[0].Listeners) {
        t.Fatalf("expected 1 registered listener, got: %d", len(registeredEvents[0].Listeners))
    }

    if false == registeredEvents[0].Listeners[0].Required {
        t.Fatalf("expected the required mark to be reported")
    }

    if true == registeredEvents[0].Listeners[0].MaySkipRequiredListeners {
        t.Fatalf("expected the may-skip mark to be absent")
    }
}

/* @info a subscriber that carries no fields occupies no memory and every zero-size allocation answers one address, so two instances of one such type were one identity here: a later RemoveSubscriber for either instance took both instances' listeners down and reported a plausible count for it */
func TestEventDispatcher_AddSubscriber_RefusesASecondRegistrationOfTheSameIdentity(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    dispatcher.AddSubscriber(&firstZeroSizeSubscriber{})

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(&firstZeroSizeSubscriber{})
        },
        "event subscriber is already registered",
    )
}

/* @info every subscribed event is validated before a single listener is registered: validating while registering left a subscriber whose second event was malformed half-installed, with the listeners of the first one live and firing under a subscriber the caller was told had been refused */
func TestEventDispatcher_AddSubscriber_RegistersNothingWhenALaterEventIsMalformed(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "a.event": {
                NewSubscribedEvent(
                    func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                        return nil
                    },
                    0,
                ),
            },
            "b.event": nil,
        },
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(subscriber)
        },
        "subscribed event list may not be nil",
    )

    if 0 != len(dispatcher.RegisteredEvents()) {
        t.Fatalf("a refused subscriber must leave no listener registered")
    }
}

/* @info an event name mapped to no subscribed events registers nothing while reporting success, which is how a subscriber assembled from configuration ends up silently inert */
func TestEventDispatcher_AddSubscriber_RefusesAnEmptySubscribedEventList(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "a.event": {},
        },
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(subscriber)
        },
        "subscribed event list may not be empty",
    )
}

/* @info a subscriber declaring no events at all is the same silence one step further out */
func TestEventDispatcher_AddSubscriber_RefusesASubscriberWithNoSubscribedEvents(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{},
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(subscriber)
        },
        "event subscriber declares no subscribed events",
    )
}

/* @info the typed nil passes a plain comparison and the identity guard that names the mistake sat behind the SubscribedEvents call, so the caller received a bare nil dereference raised inside its own subscriber instead of the framework error */
func TestEventDispatcher_AddSubscriber_RefusesATypedNilSubscriber(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    var subscriber *testSubscriber

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(subscriber)
        },
        "event subscriber may not be nil",
    )
}

/* @info the same hole on the removal side, where the precise message was already reachable */
func TestEventDispatcher_RemoveSubscriber_RefusesATypedNilSubscriber(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    var subscriber *testSubscriber

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.RemoveSubscriber(subscriber)
        },
        "event subscriber may not be nil",
    )
}

/* @info a typed nil event passed the plain guard and dereferenced on Name(); the recovery that would have described it then dereferenced the same value while building its context, so a second panic replaced the diagnostic and the caller was left with a bare memory address */
func TestEventDispatcher_Dispatch_RefusesATypedNilEvent(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    var eventValue *Event

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = dispatcher.Dispatch(runtimeInstance, eventValue)
        },
        "event may not be nil",
    )
}

/* @info the typed nil clock passed the constructor guard and the failure surfaced only at the first dispatch, from inside the event constructor, without the wiring site that caused it */
func TestNewEventDispatcher_RefusesATypedNilClock(t *testing.T) {
    var clockInstance *testTypedNilClock

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewEventDispatcher(clockInstance)
        },
        "clock may not be nil",
    )
}

type testTypedNilClock struct {
}

func (instance *testTypedNilClock) Now() time.Time {
    return time.Time{}
}

func (instance *testTypedNilClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return nil
}

/* @info an exit carries its code on the wrapper, and folding it into an ordinary listener error left a deliberate exit as a request failure with the code gone */
func TestEventDispatcher_ListenerExit_TravelsToTheProcessBoundary(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            exception.Exit(
                exception.NewExitError(
                    7,
                    exception.NewError("the command is done", nil, nil),
                ),
            )

            return nil
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    recoveredValue := func() (recoveredValue any) {
        defer func() {
            recoveredValue = recover()
        }()

        _, _ = dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))

        return nil
    }()

    exitErr, ok := recoveredValue.(*exception.ExitError)
    if false == ok {
        t.Fatalf("expected the exit to reach the process boundary, got: %T (%v)", recoveredValue, recoveredValue)
    }

    if 7 != exitErr.ExitCode() {
        t.Fatalf("expected exit code 7, got: %d", exitErr.ExitCode())
    }
}

/* @info a panic value that reports itself already logged was written by whoever raised it, and logging it again here reported one failure as two */
func TestEventDispatcher_ListenerPanic_IsNotLoggedTwiceWhenTheValueReportsItselfLogged(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panicErr := exception.NewError("already reported by its author", nil, nil)
            _ = exception.MarkLogged(panicErr)

            exception.Panic(panicErr)

            return nil
        },
        0,
    )

    logger := &testRecordingLogger{}
    runtimeInstance := testRuntimeWithLogger(t, logger)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected the panic to travel as an error")
    }

    if 0 != len(logger.errorRecords) {
        t.Fatalf("expected no second record, got: %v", logger.errorRecords)
    }
}

/* @info a panic nobody reported is still the dispatcher's to log, with the panic value, its type and the stack */
func TestEventDispatcher_ListenerPanic_IsLoggedWhenNobodyReportedIt(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            panic("nobody reported this")
        },
        0,
    )

    logger := &testRecordingLogger{}
    runtimeInstance := testRuntimeWithLogger(t, logger)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected the panic to travel as an error")
    }

    if 1 != len(logger.errorRecords) {
        t.Fatalf("expected exactly one record, got: %d", len(logger.errorRecords))
    }
}

func testRuntimeWithLogger(t *testing.T, logger loggingcontract.Logger) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    err := serviceContainer.Register(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    return runtime.New(context.Background(), scope, serviceContainer)
}

type testRecordingLogger struct {
    errorRecords []string
}

func (instance *testRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Alert(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Critical(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Error(message string, context loggingcontract.Context) {
    instance.errorRecords = append(instance.errorRecords, message)
}

func (instance *testRecordingLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Notice(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Info(message string, context loggingcontract.Context) {
}

func (instance *testRecordingLogger) Debug(message string, context loggingcontract.Context) {
}

/* the dispatcher's panic record travels through logging.LogError, which writes a melody error via Log at the error's own level — an error-level record landing here counts the same as one through Error */
func (instance *testRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    if loggingcontract.LevelError == level {
        instance.errorRecords = append(instance.errorRecords, message)
    }
}

func (instance *testRecordingLogger) Close() error {
    return nil
}

func (instance *testRecordingLogger) Closed() bool {
    return false
}

/* the propagation probe is what dispatch calls outside the listener recovery, so a panic raised there escapes into dispatchSafely — the only door to the generic diagnostic, since a melody error and an exit are re-raised untouched above it */
type testPanickingPropagationEvent struct {
    panicValue any
}

func (instance *testPanickingPropagationEvent) Name() string {
    return "e"
}

func (instance *testPanickingPropagationEvent) Payload() any {
    return nil
}

func (instance *testPanickingPropagationEvent) Timestamp() time.Time {
    return time.Time{}
}

func (instance *testPanickingPropagationEvent) StopPropagation() {
}

func (instance *testPanickingPropagationEvent) IsPropagationStopped() bool {
    panic(instance.panicValue)
}

/* @info a panic that is neither a melody error nor an exit is described rather than passed on bare: the caller used to receive the raw value with no event name, no event type and no stack, which for a panic raised inside the dispatch machinery names nothing an operator can act on */
func TestEventDispatcher_DispatchPanic_IsDescribedWithTheEventAndTheStack(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    eventValue := &testPanickingPropagationEvent{panicValue: "propagation probe exploded"}

    recoveredValue := func() (recovered any) {
        defer func() {
            recovered = recover()
        }()

        _, _ = dispatcher.Dispatch(runtimeInstance, eventValue)

        return nil
    }()

    if nil == recoveredValue {
        t.Fatalf("expected the dispatch panic to travel to the caller")
    }

    exceptionValue, isException := recoveredValue.(*exception.Error)
    if false == isException {
        t.Fatalf("expected the panic to be described by a melody error, got %T (%v)", recoveredValue, recoveredValue)
    }

    if "event dispatch panicked" != exceptionValue.Message() {
        t.Fatalf("unexpected message: %q", exceptionValue.Message())
    }

    context := exceptionValue.Context()

    if "e" != context["eventName"] {
        t.Fatalf("expected the event name in the context, got %v", context["eventName"])
    }
    if "*event.testPanickingPropagationEvent" != context["eventType"] {
        t.Fatalf("expected the event type in the context, got %v", context["eventType"])
    }
    if "propagation probe exploded" != context["recoveredValue"] {
        t.Fatalf("expected the recovered value in the context, got %v", context["recoveredValue"])
    }

    panicStack, isString := context["panicStack"].(string)
    if false == isString || "" == panicStack {
        t.Fatalf("expected a panic stack in the context, got %v", context["panicStack"])
    }
}

/* @info the two refusals of the mark, which is the door a fail-closed guarantee is armed through: an empty event name files the mark under a bucket no dispatch reads, and a zero listener id matches no registration — both used to be discovered as a guarantee that silently never armed */
func TestEventDispatcher_MarkListenerRequired_RefusesAnUnusableRegistration(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    registration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.MarkListenerRequired(
                eventcontract.ListenerRegistration{EventName: "", ListenerId: registration.ListenerId},
            )
        },
        "event name is required to mark a listener",
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.MarkListenerMaySkipRequiredListeners(
                eventcontract.ListenerRegistration{EventName: "e", ListenerId: 0},
            )
        },
        "event listener id is required to mark a listener",
    )
}

/* @info the same two refusals on the removal door: without them an empty name removes nothing while reporting a search, and a zero id would match the zero value of any registration a caller assembled by hand */
func TestEventDispatcher_RemoveListener_RefusesAnUnusableRegistration(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    registration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = dispatcher.RemoveListener(
                eventcontract.ListenerRegistration{EventName: "", ListenerId: registration.ListenerId},
            )
        },
        "event name is required to remove a listener",
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = dispatcher.RemoveListener(
                eventcontract.ListenerRegistration{EventName: "e", ListenerId: 0},
            )
        },
        "event listener id is required to remove a listener",
    )
}

type testTwoEventSubscriber struct {
}

func (instance *testTwoEventSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "first": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
        "second": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

/* @info removing one listener of a subscriber scrubs exactly that entry from the subscriber's record and leaves the rest: the record is what RemoveSubscriber later walks, so an entry left behind names a listener that no longer exists — and a subscriber whose last entry went must leave no key behind, or a second registration of the same subscriber is refused as already registered forever */
func TestEventDispatcher_RemoveListener_ScrubsOnlyTheRemovedEntryFromTheSubscriberRecord(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &testTwoEventSubscriber{}

    dispatcher.AddSubscriber(subscriber)

    identityValue := eventSubscriberIdentity(subscriber)

    dispatcher.mutex.RLock()
    registrations := append([]subscriberRegistration(nil), dispatcher.subscriberRegistrations[identityValue]...)
    dispatcher.mutex.RUnlock()

    if 2 != len(registrations) {
        t.Fatalf("expected the subscriber to hold two registrations, got %d", len(registrations))
    }

    removed := dispatcher.RemoveListener(
        eventcontract.ListenerRegistration{
            EventName:  registrations[0].eventName,
            ListenerId: registrations[0].listenerId,
        },
    )
    if false == removed {
        t.Fatalf("expected the listener to be removed")
    }

    dispatcher.mutex.RLock()
    remaining := append([]subscriberRegistration(nil), dispatcher.subscriberRegistrations[identityValue]...)
    dispatcher.mutex.RUnlock()

    if 1 != len(remaining) {
        t.Fatalf("expected exactly the removed entry to be scrubbed, got %d remaining", len(remaining))
    }
    if registrations[1].listenerId != remaining[0].listenerId {
        t.Fatalf("expected the surviving entry to be the one not removed, got %d", remaining[0].listenerId)
    }

    removed = dispatcher.RemoveListener(
        eventcontract.ListenerRegistration{
            EventName:  remaining[0].eventName,
            ListenerId: remaining[0].listenerId,
        },
    )
    if false == removed {
        t.Fatalf("expected the last listener to be removed")
    }

    dispatcher.mutex.RLock()
    _, keyExists := dispatcher.subscriberRegistrations[identityValue]
    dispatcher.mutex.RUnlock()

    if true == keyExists {
        t.Fatalf("expected the subscriber key to be dropped once its last registration went")
    }

    /* the record is gone, so the same subscriber may be registered again — the proof that nothing stale was left behind */
    dispatcher.AddSubscriber(subscriber)
}

type testValueSubscriber struct {
}

func (instance testValueSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "e": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

/* @info a subscriber that is not a pointer has no address to be filed under, so two instances carrying the same fields would be one registration and removing either would unregister both; it is refused at the door instead, naming the type the caller passed */
func TestEventDispatcher_AddSubscriber_RefusesASubscriberThatIsNotAPointer(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    identityValue := eventSubscriberIdentity(testValueSubscriber{})
    if 0 != identityValue.pointer {
        t.Fatalf("expected a value subscriber to have no identity, got %d", identityValue.pointer)
    }

    testhelper.AssertPanicsWithError(
        t,
        func() {
            dispatcher.AddSubscriber(testValueSubscriber{})
        },
        "event subscriber pointer is required to add a subscriber",
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = dispatcher.RemoveSubscriber(testValueSubscriber{})
        },
        "event subscriber pointer is required to remove a subscriber",
    )
}

/* @info the identity of a nil subscriber is the WHOLE zero identity, type included: the pointer alone would also read zero for a typed nil that walked past this guard into the reflect path below, so the type is what tells the two apart — and the identity is a map key, where a zero pointer paired with a type is a different key from the zero value */
func TestEventSubscriberIdentity_AnswersTheZeroIdentityForANilSubscriber(t *testing.T) {
    untypedIdentity := eventSubscriberIdentity(nil)
    if 0 != untypedIdentity.pointer || nil != untypedIdentity.subscriberType {
        t.Fatalf("expected an untyped nil subscriber to have no identity, got %#v", untypedIdentity)
    }

    var typedNil *testTwoEventSubscriber

    typedIdentity := eventSubscriberIdentity(typedNil)
    if 0 != typedIdentity.pointer || nil != typedIdentity.subscriberType {
        t.Fatalf("expected a typed nil subscriber to have no identity, got %#v", typedIdentity)
    }
}
