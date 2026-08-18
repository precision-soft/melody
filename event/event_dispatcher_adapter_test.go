package event

import (
    "fmt"
    "sync"
    "testing"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type testAdapterSubscriber struct {
    events map[string][]eventcontract.SubscribedEvent
}

func (instance *testAdapterSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return instance.events
}

func TestEventDispatcherAdapter_StopPropagationIsMirroredToOriginalEvent(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)
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
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(nil)
    }, "event subscriber may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(&testAdapterSubscriber{events: nil})
    }, "subscribed events may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "": {
                        NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0),
                    },
                },
            },
        )
    }, "event name may not be empty")

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": nil,
                },
            },
        )
    }, "subscribed event list may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {nil},
                },
            },
        )
    }, "subscribed event may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        adapter.AddSubscriber(
            &testAdapterSubscriber{
                events: map[string][]eventcontract.SubscribedEvent{
                    "e": {NewSubscribedEvent(nil, 0)},
                },
            },
        )
    }, "subscribed event listener is required")
}

func TestEventDispatcherAdapter_Dispatch_ReturnsErrorOnNilEvent(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = adapter.Dispatch(nil, nil)
        },
        "event may not be nil",
    )
}

func TestEventDispatcherAdapter_DispatchName_ReturnsErrorOnEmptyName(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _, _ = adapter.DispatchName(nil, "", nil)
        },
        "event name may not be empty",
    )
}

func TestEventDispatcherAdapter_RemoveListener_RemovesListener(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

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
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

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

func TestEventDispatcherAdapter_RegisteredEventsIsSafeForConcurrentReaders(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    listener := func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
        return nil
    }

    for index := 0; index < 200; index++ {
        _ = adapter.AddListener("e", listener, index)
    }

    var waitGroup sync.WaitGroup
    start := make(chan struct{})

    for worker := 0; worker < 16; worker++ {
        waitGroup.Add(1)

        go func() {
            defer waitGroup.Done()

            <-start

            for iteration := 0; iteration < 200; iteration++ {
                _ = adapter.RegisteredEvents()
            }
        }()
    }

    close(start)
    waitGroup.Wait()
}

type firstZeroSizeAdapterSubscriber struct{}

func (instance *firstZeroSizeAdapterSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "zero.size.adapter.first": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

type secondZeroSizeAdapterSubscriber struct{}

func (instance *secondZeroSizeAdapterSubscriber) SubscribedEvents() map[string][]eventcontract.SubscribedEvent {
    return map[string][]eventcontract.SubscribedEvent{
        "zero.size.adapter.second": {
            NewSubscribedEvent(
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            ),
        },
    }
}

func TestEventDispatcherAdapter_RemoveSubscriber_DistinctZeroSizeSubscribersKeepTheirOwnListeners(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    first := &firstZeroSizeAdapterSubscriber{}
    second := &secondZeroSizeAdapterSubscriber{}

    adapter.AddSubscriber(first)
    adapter.AddSubscriber(second)

    removedCount := adapter.RemoveSubscriber(first)
    if 1 != removedCount {
        t.Fatalf("expected 1 removed listener, got: %d", removedCount)
    }

    registeredEvents := adapter.RegisteredEvents()

    if true == testHasRegisteredEventWithListeners(registeredEvents, "zero.size.adapter.first") {
        t.Fatalf("expected the removed subscriber to lose its listener")
    }

    if false == testHasRegisteredEventWithListeners(registeredEvents, "zero.size.adapter.second") {
        t.Fatalf("expected the other zero size subscriber to keep its listener")
    }
}

/* the bookkeeping is scrubbed whether or not the wrapped dispatcher still held the listener: returning early on false left the adapter's own record of a listener that no longer exists, reported by RegisteredEvents forever and removable by nothing, since every retry took the same early return */
func TestEventDispatcherAdapter_RemoveListener_ScrubsItsRecordForAListenerTheWrappedDispatcherNoLongerHolds(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    registration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    if false == dispatcher.RemoveListener(registration) {
        t.Fatalf("expected the wrapped dispatcher to remove the listener")
    }

    if true == adapter.RemoveListener(registration) {
        t.Fatalf("expected the adapter to report that the wrapped dispatcher held nothing")
    }

    if 0 != len(adapter.RegisteredEvents()) {
        t.Fatalf("the adapter must not keep reporting a listener that no longer exists")
    }
}

/* callers probe for RequiredListenerRegistrar to learn whether the fail-closed guarantee is available, and the adapter satisfies that probe on its own behalf: swallowing the mark answered the probe yes and left the guarantee unarmed */
func TestEventDispatcherAdapter_MarkListenerRequired_RefusesADispatcherThatCannotMarkRequiredListeners(t *testing.T) {
    adapter := NewEventDispatcherAdapter(&testPlainEventDispatcher{})

    testhelper.AssertPanicsWithError(
        t,
        func() {
            adapter.MarkListenerRequired(
                eventcontract.ListenerRegistration{
                    EventName:  "e",
                    ListenerId: 1,
                },
            )
        },
        "the wrapped event dispatcher cannot mark required listeners",
    )
}

/* the same refusal for the opt-out, which is just as silently absorbed */
func TestEventDispatcherAdapter_MarkListenerMaySkipRequiredListeners_RefusesADispatcherThatCannotMarkRequiredListeners(t *testing.T) {
    adapter := NewEventDispatcherAdapter(&testPlainEventDispatcher{})

    testhelper.AssertPanicsWithError(
        t,
        func() {
            adapter.MarkListenerMaySkipRequiredListeners(
                eventcontract.ListenerRegistration{
                    EventName:  "e",
                    ListenerId: 1,
                },
            )
        },
        "the wrapped event dispatcher cannot mark required listeners",
    )
}

/* the marks reach the adapter's own inspection too, so wrapping a dispatcher does not hide whether the guarantee is armed */
func TestEventDispatcherAdapter_RegisteredEvents_ReportsTheRequiredListenerMarks(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    registration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )
    adapter.MarkListenerRequired(registration)

    registeredEvents := adapter.RegisteredEvents()
    if 1 != len(registeredEvents) || 1 != len(registeredEvents[0].Listeners) {
        t.Fatalf("expected one registered listener")
    }

    if false == registeredEvents[0].Listeners[0].Required {
        t.Fatalf("expected the required mark to be reported")
    }
}

/* a typed nil dispatcher passed the plain guard and dereferenced on the first use, blaming the dispatch instead of the wiring */
func TestNewEventDispatcherAdapter_RefusesATypedNilDispatcher(t *testing.T) {
    var dispatcher *EventDispatcher

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewEventDispatcherAdapter(dispatcher)
        },
        "event dispatcher may not be nil",
    )
}

type testPlainEventDispatcher struct {
}

func (instance *testPlainEventDispatcher) AddListener(eventName string, listener eventcontract.EventListener, priority int) eventcontract.ListenerRegistration {
    return eventcontract.ListenerRegistration{
        EventName:  eventName,
        ListenerId: 1,
    }
}

func (instance *testPlainEventDispatcher) RemoveListener(registration eventcontract.ListenerRegistration) bool {
    return false
}

func (instance *testPlainEventDispatcher) AddSubscriber(subscriber eventcontract.EventSubscriber) {
}

func (instance *testPlainEventDispatcher) RemoveSubscriber(subscriber eventcontract.EventSubscriber) int {
    return 0
}

func (instance *testPlainEventDispatcher) Dispatch(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) (eventcontract.Event, error) {
    return eventValue, nil
}

func (instance *testPlainEventDispatcher) DispatchName(runtimeInstance runtimecontract.Runtime, eventName string, payload any) (eventcontract.Event, error) {
    return nil, nil
}

/* the adapter's own door carries the same two refusals as the dispatcher behind it: it does not forward before validating, so a listener registered under an empty name or a nil listener would be recorded by the adapter and refused by the dispatcher — an inspection reporting a listener that was never installed */
func TestEventDispatcherAdapter_AddListener_RefusesAnEmptyNameAndANilListener(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = adapter.AddListener(
                "",
                func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                    return nil
                },
                0,
            )
        },
        "event name is required to add a listener",
    )

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = adapter.AddListener("e", nil, 0)
        },
        "event listener is required to add a listener",
    )

    if 0 != len(adapter.RegisteredEvents()) {
        t.Fatalf("expected a refused registration to leave no record, got %d events", len(adapter.RegisteredEvents()))
    }
}

/* a subscriber already registered is refused rather than registered twice: every zero-size type answers one address, so a second registration would give the two instances one record and removing either would unregister both */
func TestEventDispatcherAdapter_AddSubscriber_RefusesASecondRegistrationOfTheSameSubscriber(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    subscriber := &testAdapterSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "e": {
                NewSubscribedEvent(
                    func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                        return nil
                    },
                    0,
                ),
            },
        },
    }

    adapter.AddSubscriber(subscriber)

    testhelper.AssertPanicsWithError(
        t,
        func() {
            adapter.AddSubscriber(subscriber)
        },
        "event subscriber is already registered",
    )

    registeredEvents := adapter.RegisteredEvents()
    if 1 != len(registeredEvents) || 1 != len(registeredEvents[0].Listeners) {
        t.Fatalf("expected the refused second registration to add no listener, got %#v", registeredEvents)
    }
}

/* removing the last listener of a subscriber drops the subscriber key rather than leaving an empty list: kept, the subscriber is reported as registered forever and can never be registered again */
func TestEventDispatcherAdapter_RemoveListener_DropsTheSubscriberKeyWithItsLastRegistration(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    subscriber := &testAdapterSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "e": {
                NewSubscribedEvent(
                    func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
                        return nil
                    },
                    0,
                ),
            },
        },
    }

    adapter.AddSubscriber(subscriber)

    identityValue := eventSubscriberIdentity(subscriber)

    adapter.mutex.RLock()
    registrations := append([]eventcontract.ListenerRegistration(nil), adapter.subscriberRegistrations[identityValue]...)
    adapter.mutex.RUnlock()

    if 1 != len(registrations) {
        t.Fatalf("expected one registration for the subscriber, got %d", len(registrations))
    }

    if false == adapter.RemoveListener(registrations[0]) {
        t.Fatalf("expected the listener to be removed")
    }

    adapter.mutex.RLock()
    _, keyExists := adapter.subscriberRegistrations[identityValue]
    adapter.mutex.RUnlock()

    if true == keyExists {
        t.Fatalf("expected the subscriber key to be dropped with its last registration")
    }

    /* the record is gone, so the same subscriber may be registered again — the proof that nothing stale was left behind */
    adapter.AddSubscriber(subscriber)
}

/* the opt-out mark is recorded on the adapter's own entry, not only forwarded: the adapter is what an inspection reads, so a mark that reached the dispatcher alone left `debug:events --verbose` reporting a guarantee still armed for a listener that had opted out of it */
func TestEventDispatcherAdapter_MarkListenerMaySkipRequiredListeners_RecordsTheMarkForInspection(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    registration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        0,
    )

    adapter.MarkListenerMaySkipRequiredListeners(registration)

    registeredEvents := adapter.RegisteredEvents()
    if 1 != len(registeredEvents) || 1 != len(registeredEvents[0].Listeners) {
        t.Fatalf("expected one registered listener, got %#v", registeredEvents)
    }

    if false == registeredEvents[0].Listeners[0].MaySkipRequiredListeners {
        t.Fatalf("expected the opt-out mark to be reported by the inspection")
    }
    if true == registeredEvents[0].Listeners[0].Required {
        t.Fatalf("expected the opt-out mark not to arm the required one")
    }
}

/* listeners of equal priority are reported in registration order: dispatch breaks such a tie by listener id, so an inspection ordering them any other way would advertise an execution order the dispatch does not use */
func TestEventDispatcherAdapter_RegisteredEvents_BreaksEqualPrioritiesByRegistrationOrder(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    firstRegistration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        5,
    )
    secondRegistration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        5,
    )
    higherRegistration := adapter.AddListener(
        "e",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        },
        10,
    )

    registeredEvents := adapter.RegisteredEvents()
    if 1 != len(registeredEvents) || 3 != len(registeredEvents[0].Listeners) {
        t.Fatalf("expected three registered listeners, got %#v", registeredEvents)
    }

    reported := registeredEvents[0].Listeners

    expectedOrder := []uint64{
        higherRegistration.ListenerId,
        firstRegistration.ListenerId,
        secondRegistration.ListenerId,
    }

    for index, expectedListenerId := range expectedOrder {
        if fmt.Sprintf("%d", expectedListenerId) != reported[index].ListenerId {
            t.Fatalf("expected listener %d at position %d, got %q", expectedListenerId, index, reported[index].ListenerId)
        }
    }
}

func TestEventDispatcherAdapter_ConcurrentAddSubscriberRefusesAllButOne(t *testing.T) {
    for iteration := 0; iteration < 2000; iteration++ {
        dispatcher, _ := testNewEventDispatcher()
        adapter := NewEventDispatcherAdapter(dispatcher)

        subscriber := &testAdapterSubscriber{
            events: map[string][]eventcontract.SubscribedEvent{
                "adapter.concurrent": {
                    NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 0),
                    NewSubscribedEvent(func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil }, 1),
                },
            },
        }

        startBarrier := make(chan struct{})
        var waitGroup sync.WaitGroup
        var panicMutex sync.Mutex
        panicCount := 0

        for worker := 0; worker < 4; worker++ {
            waitGroup.Add(1)
            go func() {
                defer waitGroup.Done()
                defer func() {
                    if nil != recover() {
                        panicMutex.Lock()
                        panicCount++
                        panicMutex.Unlock()
                    }
                }()

                <-startBarrier
                adapter.AddSubscriber(subscriber)
            }()
        }

        close(startBarrier)
        waitGroup.Wait()

        installedCount := 0
        for _, registered := range adapter.RegisteredEvents() {
            installedCount += len(registered.Listeners)
        }

        if 3 != panicCount || 2 != installedCount {
            t.Fatalf("iteration %d: expected three refusals and one installation, got %d refusals and %d installed listeners", iteration, panicCount, installedCount)
        }
    }
}

func TestEventDispatcherAdapter_InspectorTiebreakFollowsTheDispatchOrder(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    firstRegistration := dispatcher.AddListener(
        "adapter.tiebreak",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil },
        0,
    )
    secondRegistration := dispatcher.AddListener(
        "adapter.tiebreak",
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error { return nil },
        0,
    )

    if firstRegistration.ListenerId >= secondRegistration.ListenerId {
        t.Fatalf("expected the wrapped dispatcher to issue increasing listener ids")
    }

    /* the interleaving under construction: the goroutine holding the lower listener id was preempted before recording its adapter entry, so the entries arrived in the opposite order */
    adapter.mutex.Lock()
    adapter.listenerRegistrations["adapter.tiebreak"] = []adapterListenerRegistration{
        {
            registration: secondRegistration,
            priority:     0,
            source:       eventcontract.RegisteredListenerSourceListener,
        },
        {
            registration: firstRegistration,
            priority:     0,
            source:       eventcontract.RegisteredListenerSourceListener,
        },
    }
    adapter.mutex.Unlock()

    registered := adapter.RegisteredEvents()
    if 1 != len(registered) || 2 != len(registered[0].Listeners) {
        t.Fatalf("expected one event with two listeners")
    }

    expectedFirst := fmt.Sprintf("%d", firstRegistration.ListenerId)
    if expectedFirst != registered[0].Listeners[0].ListenerId {
        t.Fatalf("expected the inspector to rank listener id %s first, got %s", expectedFirst, registered[0].Listeners[0].ListenerId)
    }
}
