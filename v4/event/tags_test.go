package event

import (
    "errors"
    "testing"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

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
