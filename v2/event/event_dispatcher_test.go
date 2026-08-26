package event

import (
    "context"
    "errors"
    "strings"
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

func TestEventDispatcher_TheWrapperInheritsTheAlreadyLoggedMarkOfTheListenerFailure(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return exception.MarkLogged(exception.NewError("failed and already logged", nil, nil))
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected error")
    }

    if false == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the dispatch wrapper to inherit the mark of the failure it carries")
    }
}

func TestEventDispatcher_TheWrapperOfAnUnmarkedFailureStaysUnmarked(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("failed and never logged")
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
    if nil == err {
        t.Fatalf("expected error")
    }

    if true == exception.IsAlreadyLogged(err) {
        t.Fatalf("expected the wrapper of an unmarked failure to stay unmarked")
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

    /* the stop's refusal carries the listener's own failure as its cause: returned unlogged on the promise that the caller's record names it, the failure otherwise reached no log at all on exactly this path */
    chain := ""
    for current := err; nil != current; current = errors.Unwrap(current) {
        chain = chain + current.Error() + "\n"
    }

    if false == strings.Contains(chain, "the listener's own failure") {
        t.Fatalf("expected the listener's failure in the refusal's cause chain, got %q", chain)
    }
}

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

func TestEventDispatcher_FailingListenerBeforeRequiredListener_FailsClosed(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    listenerFailure := errors.New("the listener's own failure")
    requiredRan := false

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerFailure
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

    if false == errors.Is(err, listenerFailure) {
        t.Fatalf("expected the listener's failure to travel as the cause, got: %v", err)
    }
}

func TestEventDispatcher_FailingListenerWithMaySkip_ReportsItsOwnFailure(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    listenerFailure := errors.New("the listener's own failure")

    failingRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerFailure
        },
        100,
    )
    dispatcher.MarkListenerMaySkipRequiredListeners(failingRegistration)

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

    if _, ok := err.(*RequiredListenerSkippedError); true == ok {
        t.Fatalf("a failing listener marked may-skip must report its own failure, got: %v", err)
    }

    if false == errors.Is(err, listenerFailure) {
        t.Fatalf("expected the listener's own failure, got: %v", err)
    }
}

func TestEventDispatcher_FailingListenerAfterTheRequiredListenerRan_ReportsItsOwnFailure(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    listenerFailure := errors.New("the listener's own failure")
    requiredRan := false

    requiredRegistration := dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            requiredRan = true

            return nil
        },
        100,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    _ = dispatcher.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return listenerFailure
        },
        0,
    )

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))

    if false == requiredRan {
        t.Fatalf("expected the required listener to have run before the failing one")
    }

    if _, ok := err.(*RequiredListenerSkippedError); true == ok {
        t.Fatalf("nothing required was skipped, the dispatch must report the listener's own failure, got: %v", err)
    }

    if false == errors.Is(err, listenerFailure) {
        t.Fatalf("expected the listener's own failure, got: %v", err)
    }
}

/* a subscriber installation is atomic against its twin and against its removal: without the outer subscriber section, two concurrent registrations of one identity both pass the duplicate refusal and every listener fires twice. The rounds make the interleaving overwhelmingly likely to occur at least once, and the race detector reads the same window directly. */
func TestEventDispatcher_ConcurrentSubscriberRegistrationStaysAtomic(t *testing.T) {
    for round := 0; 100 > round; round++ {
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

        panicCount := 0

        var panicMutex sync.Mutex
        var registrations sync.WaitGroup

        /* the gate releases both registrations at once: started bare, the first goroutine often finishes before the second begins and the interleaving under test never occurs */
        start := make(chan struct{})

        for attempt := 0; 4 > attempt; attempt++ {
            registrations.Add(1)

            go func() {
                defer registrations.Done()
                defer func() {
                    if recovered := recover(); nil != recovered {
                        panicMutex.Lock()
                        panicCount++
                        panicMutex.Unlock()
                    }
                }()

                <-start

                dispatcher.AddSubscriber(subscriber)
            }()
        }

        close(start)
        registrations.Wait()

        if 3 != panicCount {
            t.Fatalf("round %d: expected exactly three duplicate refusals, got %d", round, panicCount)
        }

        runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

        _, err := dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance))
        if nil != err {
            t.Fatalf("round %d: unexpected error: %v", round, err)
        }

        if 1 != calls {
            t.Fatalf("round %d: expected the listener to be installed exactly once, got %d calls", round, calls)
        }
    }
}

/* debugGateLogger records what it is handed and answers the level question the way a configured journal would: against a THRESHOLD, not with one answer for every level. A double that answered the same for all five would shadow the guard it is here to prove — asking about the wrong level would get the right answer by accident, and a dispatch gating on emergency instead of debug would pass (§5.16). */
type debugGateLogger struct {
    debugMessages []string
    minLevel      loggingcontract.Level
}

func debugGateLevelPriority(level loggingcontract.Level) int {
    switch level {
    case loggingcontract.LevelDebug:
        return 0
    case loggingcontract.LevelInfo:
        return 1
    case loggingcontract.LevelWarning:
        return 2
    case loggingcontract.LevelError:
        return 3
    case loggingcontract.LevelEmergency:
        return 4
    }

    return 0
}

func (instance *debugGateLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *debugGateLogger) Debug(message string, context loggingcontract.Context) {
    instance.debugMessages = append(instance.debugMessages, message)
}

func (instance *debugGateLogger) Info(message string, context loggingcontract.Context) {}

func (instance *debugGateLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *debugGateLogger) Error(message string, context loggingcontract.Context) {}

func (instance *debugGateLogger) Emergency(message string, context loggingcontract.Context) {}

func (instance *debugGateLogger) Enabled(level loggingcontract.Level) bool {
    return debugGateLevelPriority(level) >= debugGateLevelPriority(instance.minLevel)
}

var _ loggingcontract.LevelReporter = (*debugGateLogger)(nil)

/* the dispatch asks the journal once and builds nothing it would throw away: at least three events travel per request, and every debug record below assembles a context map at the call site — plus, for one of them, a listener name resolved through reflect and runtime.FuncForPC per listener per dispatch. A logger that says the level is off must receive nothing at all; the same dispatch under a logger that says it is on must receive every record it always did, which is what tells the gate apart from a deletion. */
func TestEventDispatcher_DoesNotBuildDebugRecordsTheJournalWouldDiscard(t *testing.T) {
    for _, testCase := range []struct {
        name            string
        minLevel        loggingcontract.Level
        expectedRecords int
    }{
        {"the journal discards debug", loggingcontract.LevelError, 0},
        {"the journal keeps debug", loggingcontract.LevelDebug, 3},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            logger := &debugGateLogger{minLevel: testCase.minLevel}

            dispatcher := NewEventDispatcher(clock.NewSystemClock())

            _ = dispatcher.AddListener(
                "gate.event",
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                100,
            )

            _, dispatchErr := dispatcher.DispatchName(testRuntimeWithLogger(t, logger), "gate.event", nil)
            if nil != dispatchErr {
                t.Fatalf("unexpected error: %v", dispatchErr)
            }

            if testCase.expectedRecords != len(logger.debugMessages) {
                t.Fatalf(
                    "expected %d debug records under a %q journal, got %d: %v",
                    testCase.expectedRecords,
                    testCase.minLevel,
                    len(logger.debugMessages),
                    logger.debugMessages,
                )
            }
        })
    }
}

/* the listener name is resolved where it is USED, so the paths that need it must still carry it with the journal at a level that builds no debug record at all: the failure wrapper's context and the required-listener refusal are what an operator reads when a dispatch goes wrong, and a name resolved only inside the debug branch would leave both saying "-" exactly when they matter. */
func TestEventDispatcher_NamesTheListenerOnTheFailurePathWithDebugOff(t *testing.T) {
    logger := &debugGateLogger{minLevel: loggingcontract.LevelError}

    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    _ = dispatcher.AddListener(
        "gate.failure",
        failingGateListener,
        100,
    )

    _, dispatchErr := dispatcher.DispatchName(testRuntimeWithLogger(t, logger), "gate.failure", nil)
    if nil == dispatchErr {
        t.Fatalf("expected the listener failure to be reported")
    }

    if 0 != len(logger.debugMessages) {
        t.Fatalf("expected no debug record under a journal that discards them, got %v", logger.debugMessages)
    }

    var exceptionErr *exception.Error
    if false == errors.As(dispatchErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got %T", dispatchErr)
    }

    listenerName, ok := exceptionErr.Context()["listenerName"].(string)
    if false == ok {
        t.Fatalf("expected the failure context to name the listener, got %#v", exceptionErr.Context()["listenerName"])
    }

    if false == strings.Contains(listenerName, "failingGateListener") {
        t.Fatalf("expected the listener's own name in the failure, got %q", listenerName)
    }
}

func failingGateListener(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
    return errors.New("the listener refused")
}

/* which listener stopped propagation travels as the LISTENER and is named only when the refusal is built, so the required-listener refusal must still say who stopped it. Resolving the name per iteration paid the reflection for an answer almost no dispatch asks for; resolving it nowhere would leave this message blaming "-". */
func TestEventDispatcher_NamesTheStoppingListenerInTheRequiredRefusalWithDebugOff(t *testing.T) {
    logger := &debugGateLogger{minLevel: loggingcontract.LevelError}

    dispatcher := NewEventDispatcher(clock.NewSystemClock())

    _ = dispatcher.AddListener("gate.stop", stoppingGateListener, 200)

    requiredRegistration := dispatcher.AddListener(
        "gate.stop",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        100,
    )
    dispatcher.MarkListenerRequired(requiredRegistration)

    _, dispatchErr := dispatcher.DispatchName(testRuntimeWithLogger(t, logger), "gate.stop", nil)
    if nil == dispatchErr {
        t.Fatalf("expected the skipped required listener to be refused")
    }

    var exceptionErr *exception.Error
    if false == errors.As(dispatchErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got %T", dispatchErr)
    }

    stoppedBy, ok := exceptionErr.Context()["stoppedByListener"].(string)
    if false == ok {
        t.Fatalf("expected the refusal to name the stopping listener, got %#v", exceptionErr.Context())
    }

    if false == strings.Contains(stoppedBy, "stoppingGateListener") {
        t.Fatalf("expected the stopping listener's own name, got %q", stoppedBy)
    }
}

func stoppingGateListener(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
    eventValue.StopPropagation()

    return nil
}
