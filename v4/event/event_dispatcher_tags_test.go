package event

import (
    "errors"
    "strings"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
)

/* tagsTestRecorder records the listeners that ran and the prior failure each read */
type tagsTestRecorder struct {
    ran           []string
    priorFailures map[string]error
}

func newTagsTestRecorder() *tagsTestRecorder {
    return &tagsTestRecorder{
        ran:           make([]string, 0),
        priorFailures: make(map[string]error),
    }
}

func (instance *tagsTestRecorder) listener(name string, answer error) eventcontract.EventListener {
    return func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        instance.ran = append(instance.ran, name)
        instance.priorFailures[name] = eventValue.PriorFailure()

        return answer
    }
}

func (instance *tagsTestRecorder) ranList() string {
    return strings.Join(instance.ran, ",")
}

func requireTagsTestPanic(t *testing.T, call func()) *exception.Error {
    t.Helper()

    var recovered any
    func() {
        defer func() { recovered = recover() }()

        call()
    }()

    var melodyError *exception.Error
    recoveredErr, isError := recovered.(error)
    if false == isError || false == errors.As(recoveredErr, &melodyError) {
        t.Fatalf("expected a melody error, got %v", recovered)
    }

    return melodyError
}

func TestEventDispatcher_ATaggedListenerBehindTheFailureRunsWhenTheEventDeclaresItsTag(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    failure := errors.New("the first failure")

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cache"}})
    dispatcher.AddListener("e", recorder.listener("failing", failure), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("untagged", nil), eventcontract.ListenerConfig{Priority: 20})
    dispatcher.AddListener("e", recorder.listener("otherTag", nil), eventcontract.ListenerConfig{Priority: 15, Tags: []string{"audit"}})
    dispatcher.AddListener("e", recorder.listener("cache", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"audit", "cache"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "failing,cache" != recorder.ranList() {
        t.Fatalf("expected only the listener carrying a declared tag to run behind the failure, ran %s", recorder.ranList())
    }

    if false == errors.Is(err, failure) {
        t.Fatalf("expected the first failure as the answer, got %v", err)
    }
}

func TestEventDispatcher_AnUndeclaredEventRunsNoTaggedListenerAfterAFailure(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.AddListener("e", recorder.listener("failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("cache", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cache"}})

    _, _ = dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "failing" != recorder.ranList() {
        t.Fatalf("expected no listener behind the failure on an undeclared event, ran %s", recorder.ranList())
    }
}

func TestEventDispatcher_ATaggedListenerAheadOfTheFailureRunsOnce(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cache"}})
    dispatcher.AddListener("e", recorder.listener("aheadCache", nil), eventcontract.ListenerConfig{Priority: 40, Tags: []string{"cache"}})
    dispatcher.AddListener("e", recorder.listener("failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("behindCache", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cache"}})

    _, _ = dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "aheadCache,failing,behindCache" != recorder.ranList() {
        t.Fatalf("expected the tagged listener ahead of the failure to run once, ran %s", recorder.ranList())
    }
}

func TestEventDispatcher_TheAnswerJoinsTheTaggedFailuresInRunOrderAndThePassGoesOn(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    firstFailure := errors.New("first failure")
    secondFailure := errors.New("second failure")
    thirdFailure := errors.New("third failure")

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("first", firstFailure), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("second", secondFailure), eventcontract.ListenerConfig{Priority: 20, Tags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("third", thirdFailure), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("fourth", nil), eventcontract.ListenerConfig{Priority: 5, Tags: []string{"cleanup"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "first,second,third,fourth" != recorder.ranList() {
        t.Fatalf("expected a failing tagged listener not to end the pass, ran %s", recorder.ranList())
    }

    joined, isJoined := err.(interface{ Unwrap() []error })
    if false == isJoined || 3 != len(joined.Unwrap()) {
        t.Fatalf("expected the three failures joined, got %T (%v)", err, err)
    }

    for index, expected := range []error{firstFailure, secondFailure, thirdFailure} {
        if false == errors.Is(joined.Unwrap()[index], expected) {
            t.Fatalf("expected %v at index %d, got %v", expected, index, joined.Unwrap())
        }
    }
}

func TestEventDispatcher_PriorFailureIsWhatFailedAheadOfTheListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    firstFailure := errors.New("first failure")
    secondFailure := errors.New("second failure")

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("clean", nil), eventcontract.ListenerConfig{Priority: 40, Tags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("first", firstFailure), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("second", secondFailure), eventcontract.ListenerConfig{Priority: 20, Tags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("third", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    eventValue := NewEvent("e", nil, clockInstance.Now())

    dispatched, _ := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), eventValue)

    if eventcontract.Event(eventValue) != dispatched {
        t.Fatalf("expected Dispatch to answer the caller's own event, got %T", dispatched)
    }

    if nil != recorder.priorFailures["clean"] || nil != recorder.priorFailures["first"] {
        t.Fatalf("expected no prior failure ahead of the failure, got %v", recorder.priorFailures)
    }

    if false == errors.Is(recorder.priorFailures["second"], firstFailure) {
        t.Fatalf("expected the first tagged listener to read the first failure, got %v", recorder.priorFailures["second"])
    }

    thirdPrior := recorder.priorFailures["third"]
    if false == errors.Is(thirdPrior, firstFailure) || false == errors.Is(thirdPrior, secondFailure) {
        t.Fatalf("expected the second tagged listener to read the join, got %v", thirdPrior)
    }

    if nil != eventValue.PriorFailure() {
        t.Fatalf("expected the caller's event to carry no prior failure")
    }
}

func TestEventDispatcher_PriorFailureIsNilOnACleanDispatch(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("first", nil), eventcontract.ListenerConfig{Priority: 20})
    dispatcher.AddListener("e", recorder.listener("second", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))
    if nil != err {
        t.Fatalf("expected a clean dispatch, got %v", err)
    }

    if "first,second" != recorder.ranList() || nil != recorder.priorFailures["first"] || nil != recorder.priorFailures["second"] {
        t.Fatalf("expected every listener to run with no prior failure, ran %s, read %v", recorder.ranList(), recorder.priorFailures)
    }
}

func TestEventDispatcher_ADeliberateStopRunsNoTaggedListener(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener(
        "e",
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 30},
    )
    dispatcher.AddListener("e", recorder.listener("cleanup", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if nil != err || "" != recorder.ranList() {
        t.Fatalf("expected a stop to run no tagged listener, ran %s, got %v", recorder.ranList(), err)
    }
}

func TestEventDispatcher_AListenerThatFailsAndStopsStillRunsTheTaggedListeners(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener(
        "e",
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.StopPropagation()

            return errors.New("failed and stopped")
        },
        eventcontract.ListenerConfig{Priority: 30},
    )
    dispatcher.AddListener(
        "e",
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            recorder.ran = append(recorder.ran, "stoppingCleanup")
            eventValue.StopPropagation()

            return nil
        },
        eventcontract.ListenerConfig{Priority: 20, Tags: []string{"cleanup"}},
    )
    dispatcher.AddListener("e", recorder.listener("lastCleanup", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    _, _ = dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "stoppingCleanup,lastCleanup" != recorder.ranList() {
        t.Fatalf("expected neither the failed listener's stop nor a tagged listener's stop to skip the cleanup, ran %s", recorder.ranList())
    }
}

func TestEventDispatcher_ARequiredListenerThatRanAfterTheFailureIsNotRefused(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    failure := errors.New("failure")

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("failing", failure), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("required", nil), eventcontract.ListenerConfig{Priority: 10, Required: true, Tags: []string{"cleanup"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if _, isRefusal := err.(*RequiredListenerSkippedError); true == isRefusal {
        t.Fatalf("expected a required listener that ran after the failure to be satisfied, got %v", err)
    }

    if false == errors.Is(err, failure) || "failing,required" != recorder.ranList() {
        t.Fatalf("expected the failure answered with the required listener run, ran %s, got %v", recorder.ranList(), err)
    }
}

func TestEventDispatcher_AnUntaggedRequiredListenerBehindTheFailureIsRefusedWithTheJoin(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    firstFailure := errors.New("first failure")
    taggedFailure := errors.New("tagged failure")

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("failing", firstFailure), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("tagged", taggedFailure), eventcontract.ListenerConfig{Priority: 20, Tags: []string{"cleanup"}})
    dispatcher.AddListener("e", recorder.listener("required", nil), eventcontract.ListenerConfig{Priority: 10, Required: true})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if _, isRefusal := err.(*RequiredListenerSkippedError); false == isRefusal {
        t.Fatalf("expected the untagged required listener's refusal, got %T (%v)", err, err)
    }

    if false == errors.Is(err, firstFailure) || false == errors.Is(err, taggedFailure) {
        t.Fatalf("expected the join as the refusal's cause, got %v", err)
    }

    if "failing,tagged" != recorder.ranList() {
        t.Fatalf("expected the required listener not to run, ran %s", recorder.ranList())
    }
}

func TestEventDispatcher_ATaggedListenersRecordNamesTheCallersEventTypeAndItsWritesReachTheCallersEvent(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    payload := &tagsTestPayload{}

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error { return errors.New("failure") }, eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener(
        "e",
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*tagsTestPayload).written = true
            eventValue.StopPropagation()

            return errors.New("tagged failure")
        },
        eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}},
    )

    eventValue := NewEvent("e", payload, clockInstance.Now())

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), eventValue)

    if false == payload.written || false == eventValue.IsPropagationStopped() {
        t.Fatalf("expected the view's writes to reach the caller's event")
    }

    joined, isJoined := err.(interface{ Unwrap() []error })
    if false == isJoined || 2 != len(joined.Unwrap()) {
        t.Fatalf("expected two failures joined, got %v", err)
    }

    var taggedErr *exception.Error
    if false == errors.As(joined.Unwrap()[1], &taggedErr) || "*event.Event" != taggedErr.Context()["eventType"] {
        t.Fatalf("expected the tagged listener's record to name the caller's event type, got %v", joined.Unwrap()[1])
    }
}

type tagsTestPayload struct {
    written bool
}

func TestEventDispatcher_TheLoggedMarkOfTheAnswerNeedsEveryFailureLogged(t *testing.T) {
    for _, testCase := range []struct {
        name           string
        taggedLogged   bool
        expectedLogged bool
    }{
        {name: "an unlogged tagged failure", taggedLogged: false, expectedLogged: false},
        {name: "every failure logged", taggedLogged: true, expectedLogged: true},
    } {
        dispatcher, clockInstance := testNewEventDispatcher()

        taggedFailure := exception.NewError("tagged failure", nil, nil)
        if true == testCase.taggedLogged {
            _ = exception.MarkLogged(taggedFailure)
        }

        dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
        dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error { panic("the listener broke") }, eventcontract.ListenerConfig{Priority: 30})
        dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error { return taggedFailure }, eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

        _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

        if testCase.expectedLogged != exception.IsAlreadyLogged(err) {
            t.Fatalf("%s: expected the answer to read logged %t, got %v", testCase.name, testCase.expectedLogged, err)
        }
    }
}

func TestEventDispatcher_DeclareEventIsRefusedASecondTimeAndForAnEmptyName(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})

    for _, config := range []eventcontract.EventConfig{{RunAfterFailureTags: []string{"cleanup"}}, {}} {
        refusal := requireTagsTestPanic(t, func() { dispatcher.DeclareEvent("e", config) })
        if "the event is already declared; an event's config has one owner" != refusal.Message() || "e" != refusal.Context()["eventName"] {
            t.Fatalf("expected the second declaration refused by name, got %s %v", refusal.Message(), refusal.Context())
        }
    }

    refusal := requireTagsTestPanic(t, func() { dispatcher.DeclareEvent("", eventcontract.EventConfig{}) })
    if "event name is required to declare an event" != refusal.Message() {
        t.Fatalf("expected the empty name refused, got %s", refusal.Message())
    }
}

func TestEventDispatcher_ADeclarationMadeAfterTheListenersAppliesToTheNextDispatch(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    dispatcher.AddListener("e", recorder.listener("failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("cleanup", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    runtimeInstance := newEventDispatcherAdapterTestRuntime(t)

    _, _ = dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance.Now()))

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})

    _, _ = dispatcher.Dispatch(runtimeInstance, NewEvent("e", nil, clockInstance.Now()))

    if "failing,failing,cleanup" != recorder.ranList() {
        t.Fatalf("expected the declaration to apply from the next dispatch, ran %s", recorder.ranList())
    }
}

func TestEventDispatcher_TagsAreRefusedByTheirField(t *testing.T) {
    for _, testCase := range []struct {
        name            string
        tags            []string
        expectedMessage string
    }{
        {name: "empty", tags: []string{"cache", ""}, expectedMessage: "may not carry an empty tag"},
        {name: "leading space", tags: []string{" cache"}, expectedMessage: "carries a tag with whitespace"},
        {name: "trailing space", tags: []string{"cache "}, expectedMessage: "carries a tag with whitespace"},
        {name: "inner space", tags: []string{"ca che"}, expectedMessage: "carries a tag with whitespace"},
        {name: "tab", tags: []string{"ca\tche"}, expectedMessage: "carries a tag with whitespace"},
        {name: "control", tags: []string{"ca\x00che"}, expectedMessage: "carries a tag with whitespace"},
        {name: "duplicate", tags: []string{"cache", "audit", "cache"}, expectedMessage: "carries a tag twice"},
    } {
        dispatcher, _ := testNewEventDispatcher()

        for _, door := range []struct {
            field string
            call  func()
        }{
            {field: "ListenerConfig.Tags", call: func() { dispatcher.AddListener("e", testNoopEventListener, eventcontract.ListenerConfig{Tags: testCase.tags}) }},
            {field: "ListenerConfig.Tags", call: func() { dispatcher.InstallFloorListener("f", testNoopEventListener, eventcontract.ListenerConfig{Tags: testCase.tags}) }},
            {field: "EventConfig.RunAfterFailureTags", call: func() { dispatcher.DeclareEvent("d", eventcontract.EventConfig{RunAfterFailureTags: testCase.tags}) }},
        } {
            refusal := requireTagsTestPanic(t, door.call)
            if door.field+" "+testCase.expectedMessage != refusal.Message() {
                t.Fatalf("%s: expected %q, got %q", testCase.name, door.field+" "+testCase.expectedMessage, refusal.Message())
            }
        }

        if 0 != len(dispatcher.RegisteredEvents()) {
            t.Fatalf("%s: expected nothing registered or declared, got %+v", testCase.name, dispatcher.RegisteredEvents())
        }
    }
}

func TestEventDispatcher_ASubscriberWithABadTagInstallsNoneOfItsListeners(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()

    subscriber := &testSubscriber{
        events: map[string][]eventcontract.SubscribedEvent{
            "a.first": {NewSubscribedEvent(testNoopEventListener, eventcontract.ListenerConfig{Tags: []string{"cache"}})},
            "b.second": {NewSubscribedEvent(testNoopEventListener, eventcontract.ListenerConfig{Tags: []string{"bad tag"}})},
        },
    }

    refusal := requireTagsTestPanic(t, func() { dispatcher.AddSubscriber(subscriber) })
    if "ListenerConfig.Tags carries a tag with whitespace" != refusal.Message() || "b.second" != refusal.Context()["eventName"] {
        t.Fatalf("expected the subscriber's bad tag refused by name, got %s %v", refusal.Message(), refusal.Context())
    }

    if 0 != len(dispatcher.RegisteredEvents()) {
        t.Fatalf("expected none of the subscriber's listeners installed, got %+v", dispatcher.RegisteredEvents())
    }
}

func TestEventDispatcher_TheCallersTagSlicesAreCopied(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    recorder := newTagsTestRecorder()

    declaredTags := []string{"cleanup"}
    listenerTags := []string{"cleanup"}

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: declaredTags})
    dispatcher.AddListener("e", recorder.listener("failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 30})
    dispatcher.AddListener("e", recorder.listener("cleanup", nil), eventcontract.ListenerConfig{Priority: 10, Tags: listenerTags})

    declaredTags[0] = "changed"
    listenerTags[0] = "changed"

    _, _ = dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "failing,cleanup" != recorder.ranList() {
        t.Fatalf("expected a write to the caller's slices to change nothing, ran %s", recorder.ranList())
    }

    registeredEvents := dispatcher.RegisteredEvents()
    registeredEvents[0].RunAfterFailureTags[0] = "changed"
    registeredEvents[0].Listeners[1].Tags[0] = "changed"

    again := dispatcher.RegisteredEvents()
    if "cleanup" != again[0].RunAfterFailureTags[0] || "cleanup" != again[0].Listeners[1].Tags[0] {
        t.Fatalf("expected inspection to answer copies, got %+v", again)
    }
}

func TestEventDispatcher_RegisteredEventsReportsTheDeclarationAndTheTags(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)

    adapter.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    adapter.DeclareEvent("empty.declared", eventcontract.EventConfig{RunAfterFailureTags: []string{"audit"}})
    adapter.AddListener("e", testNoopEventListener, eventcontract.ListenerConfig{Priority: 20, Name: "tagged", Tags: []string{"cleanup"}})
    adapter.AddListener("e", testNoopEventListener, eventcontract.ListenerConfig{Priority: 10, Name: "otherTag", Tags: []string{"audit"}})
    adapter.AddListener("u", testNoopEventListener, eventcontract.ListenerConfig{Name: "untagged"})

    for _, registeredEvents := range [][]eventcontract.RegisteredEvent{dispatcher.RegisteredEvents(), adapter.RegisteredEvents()} {
        if 3 != len(registeredEvents) {
            t.Fatalf("expected three events, the declared one with no listener among them, got %+v", registeredEvents)
        }

        declared, emptyDeclared, undeclared := registeredEvents[0], registeredEvents[1], registeredEvents[2]

        if "e" != declared.EventName || false == declared.Declared || 1 != len(declared.RunAfterFailureTags) || "cleanup" != declared.RunAfterFailureTags[0] {
            t.Fatalf("expected the declaration reported, got %+v", declared)
        }

        if false == declared.Listeners[0].RunsAfterFailure || "cleanup" != declared.Listeners[0].Tags[0] || true == declared.Listeners[1].RunsAfterFailure {
            t.Fatalf("expected only the listener carrying a declared tag to run after a failure, got %+v", declared.Listeners)
        }

        if "empty.declared" != emptyDeclared.EventName || false == emptyDeclared.Declared || 0 != len(emptyDeclared.Listeners) || "audit" != emptyDeclared.RunAfterFailureTags[0] {
            t.Fatalf("expected the declared event with no listener listed, got %+v", emptyDeclared)
        }

        if "u" != undeclared.EventName || true == undeclared.Declared || nil == undeclared.RunAfterFailureTags || 0 != len(undeclared.RunAfterFailureTags) || nil == undeclared.Listeners[0].Tags || 0 != len(undeclared.Listeners[0].Tags) {
            t.Fatalf("expected an undeclared event with empty, non-nil tag lists, got %+v", undeclared)
        }
    }
}

func TestEventDispatcherAdapter_DeclareEventReachesTheWrappedDispatcher(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    adapter := NewEventDispatcherAdapter(dispatcher)
    recorder := newTagsTestRecorder()

    adapter.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"cleanup"}})
    adapter.AddListener("e", recorder.listener("failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 30})
    adapter.AddListener("e", recorder.listener("cleanup", nil), eventcontract.ListenerConfig{Priority: 10, Tags: []string{"cleanup"}})

    _, _ = adapter.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", nil, clockInstance.Now()))

    if "failing,cleanup" != recorder.ranList() {
        t.Fatalf("expected the adapter's declaration to reach the dispatch, ran %s", recorder.ranList())
    }

    refusal := requireTagsTestPanic(t, func() { dispatcher.DeclareEvent("e", eventcontract.EventConfig{}) })
    if "e" != refusal.Context()["eventName"] {
        t.Fatalf("expected the wrapped dispatcher to hold the declaration, got %v", refusal.Context())
    }
}

func TestNewEventFromEvent_TheCopyCarriesNoPriorFailure(t *testing.T) {
    view := newAfterFailureEvent(NewEvent("e", "payload", time.Now()), errors.New("failure"))

    copied := NewEventFromEvent(view)

    if nil != copied.PriorFailure() || "payload" != copied.Payload() {
        t.Fatalf("expected the copy to keep the payload and carry no prior failure")
    }
}
