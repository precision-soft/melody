package event

import (
    "errors"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
)

func TestEventDispatcher_ATaggedListenersRecordNamesTheCallersEventTypeAndItsWritesReachTheCallersEvent(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()

    payload := &tagsTestPayload{}

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{TagsToRunAfterFailure: []string{"cleanup"}})
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

func TestNewEventFromEvent_TheCopyCarriesNoPriorFailure(t *testing.T) {
    view := newAfterFailureEvent(NewEvent("e", "payload", time.Now()), errors.New("failure"))

    copied := NewEventFromEvent(view)

    if nil != copied.PriorFailure() || "payload" != copied.Payload() {
        t.Fatalf("expected the copy to keep the payload and carry no prior failure")
    }
}
