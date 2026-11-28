package event

import (
    "encoding/json"
    "strings"
    "testing"

    eventcontract "github.com/precision-soft/melody/v4/event/contract"
)

func fullListenerConfig(priority int, name string) eventcontract.ListenerConfig {
    return eventcontract.ListenerConfig{
        Priority: priority,
        Name:     name,
        Tags:     []string{"cache"},
        Failure: eventcontract.ListenerFailureConfig{
            Required:                     true,
            MaySkipRequiredListeners:     true,
            ReplacesResponseAfterFailure: true,
        },
    }
}

func registeredListenersOf(registeredEvents []eventcontract.RegisteredEvent, eventName string) []eventcontract.RegisteredListener {
    for _, registeredEvent := range registeredEvents {
        if eventName == registeredEvent.EventName {
            return registeredEvent.Listeners
        }
    }

    return nil
}

func TestRegisteredEvents_CarryTheConfigEachRegistrationDeclared(t *testing.T) {
    for _, adapted := range []bool{false, true} {
        dispatcher, _ := testNewEventDispatcher()
        var target eventcontract.EventDispatcher = dispatcher
        if true == adapted {
            target = NewEventDispatcherAdapter(dispatcher)
        }

        target.AddListener("added", testNoopEventListener, fullListenerConfig(7, "named"))
        target.InstallFloorListener("floored", testNoopEventListener, fullListenerConfig(-100, ""))
        target.AddSubscriber(&testSubscriber{events: map[string][]eventcontract.SubscribedEvent{
            "subscribed": {NewSubscribedEvent(testNoopEventListener, fullListenerConfig(3, ""))},
        }})

        registeredEvents := target.RegisteredEvents()
        for eventName, expected := range map[string]eventcontract.ListenerConfig{
            "added":      fullListenerConfig(7, "named"),
            "floored":    fullListenerConfig(-100, ""),
            "subscribed": fullListenerConfig(3, ""),
        } {
            listeners := registeredListenersOf(registeredEvents, eventName)
            if 1 != len(listeners) {
                t.Fatalf("adapted %v %s: expected one listener, got %+v", adapted, eventName, listeners)
            }

            reported := listeners[0].Config
            if expected.Priority != reported.Priority || expected.Name != reported.Name || "cache" != strings.Join(reported.Tags, ",") || expected.Failure != reported.Failure {
                t.Fatalf("adapted %v %s: expected %+v, got %+v", adapted, eventName, expected, reported)
            }

            if "" == listeners[0].ListenerName || ("" == expected.Name && "named" == listeners[0].ListenerName) {
                t.Fatalf("adapted %v %s: expected the resolved name beside the declared one, got %q", adapted, eventName, listeners[0].ListenerName)
            }
        }
    }
}

func TestRegisteredEvents_TheReportedConfigIsACopy(t *testing.T) {
    for _, adapted := range []bool{false, true} {
        dispatcher, _ := testNewEventDispatcher()
        var target eventcontract.EventDispatcher = dispatcher
        if true == adapted {
            target = NewEventDispatcherAdapter(dispatcher)
        }

        target.AddListener("e", testNoopEventListener, eventcontract.ListenerConfig{Tags: []string{"cache"}})
        target.DeclareEvent("e", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cache"}})

        first := target.RegisteredEvents()
        first[0].Listeners[0].Config.Tags[0] = "changed"
        first[0].Config.TagsToRunAfterFailure[0] = "changed"

        second := target.RegisteredEvents()
        if "cache" != second[0].Listeners[0].Config.Tags[0] || "cache" != second[0].Config.TagsToRunAfterFailure[0] || false == second[0].Listeners[0].RunsAfterFailure {
            t.Fatalf("adapted %v: expected a write to a report to reach no later report, got %+v", adapted, second)
        }
    }
}

func TestRegisteredEvents_AnUndeclaredEventReportsEmptyTagLists(t *testing.T) {
    dispatcher, _ := testNewEventDispatcher()
    dispatcher.AddListener("e", testNoopEventListener, eventcontract.ListenerConfig{})

    encoded, encodeErr := json.Marshal(dispatcher.RegisteredEvents())
    if nil != encodeErr {
        t.Fatalf("unexpected encode error: %v", encodeErr)
    }

    if false == strings.Contains(string(encoded), `"config":{"tagsToRunAfterFailure":[]}`) || false == strings.Contains(string(encoded), `"tags":[]`) {
        t.Fatalf("expected empty lists, not null, got %s", encoded)
    }
}
