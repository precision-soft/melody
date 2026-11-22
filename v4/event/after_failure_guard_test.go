package event

import (
    "errors"
    "strings"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/eventseam"
)

/* guardRecordingPayload records every announcement and every leave, in the order they happen */
type guardRecordingPayload struct {
    steps []string
}

func (instance *guardRecordingPayload) EnterAfterFailureListener(token eventseam.Token, listener eventseam.AfterFailureListener) func() {
    door := "closed"
    if true == listener.ReplacesResponse {
        door = "open"
    }

    instance.steps = append(instance.steps, "enter "+listener.EventName+" "+listener.ListenerName+" "+door)

    return func() {
        instance.steps = append(instance.steps, "leave "+listener.ListenerName)
    }
}

func recordingListener(payload *guardRecordingPayload, name string, err error) eventcontract.EventListener {
    return func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        payload.steps = append(payload.steps, "run "+name)

        return err
    }
}

func TestDispatch_AnnouncesEachAfterFailureListenerToAGuardingPayload(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    payload := &guardRecordingPayload{}

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"t"}})
    dispatcher.AddListener("e", recordingListener(payload, "ahead", nil), eventcontract.ListenerConfig{Priority: 50, Name: "ahead", Tags: []string{"t"}})
    dispatcher.AddListener("e", recordingListener(payload, "failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 40, Name: "failing"})
    dispatcher.AddListener("e", recordingListener(payload, "A", nil), eventcontract.ListenerConfig{Priority: 30, Name: "A", Tags: []string{"t"}, ReplacesResponseAfterFailure: true})
    dispatcher.AddListener("e", recordingListener(payload, "untagged", nil), eventcontract.ListenerConfig{Priority: 20, Name: "untagged"})
    dispatcher.AddListener("e", recordingListener(payload, "B", nil), eventcontract.ListenerConfig{Priority: 10, Name: "B", Tags: []string{"t"}})

    _, _ = dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", payload, clockInstance.Now()))

    expected := "run ahead|run failing|enter e A open|run A|leave A|enter e B closed|run B|leave B"
    if expected != strings.Join(payload.steps, "|") {
        t.Fatalf("expected %q, got %q", expected, strings.Join(payload.steps, "|"))
    }

    clean := &guardRecordingPayload{}
    cleanDispatcher, _ := testNewEventDispatcher()
    cleanDispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"t"}})
    cleanDispatcher.AddListener("e", recordingListener(clean, "A", nil), eventcontract.ListenerConfig{Name: "A", Tags: []string{"t"}, ReplacesResponseAfterFailure: true})

    _, _ = cleanDispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", clean, clockInstance.Now()))

    if "run A" != strings.Join(clean.steps, "|") {
        t.Fatalf("expected no announcement in a clean dispatch, got %q", strings.Join(clean.steps, "|"))
    }
}

func TestDispatch_LeavesTheGuardWhenAnAfterFailureListenerPanicsOrExits(t *testing.T) {
    dispatcher, clockInstance := testNewEventDispatcher()
    payload := &guardRecordingPayload{}

    dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"t"}})
    dispatcher.AddListener("e", recordingListener(payload, "failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 20, Name: "failing"})
    dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        panic("the tagged listener panicked")
    }, eventcontract.ListenerConfig{Priority: 10, Name: "panicking", Tags: []string{"t"}})

    _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", payload, clockInstance.Now()))

    if nil == err || "run failing|enter e panicking closed|leave panicking" != strings.Join(payload.steps, "|") {
        t.Fatalf("expected the panicking listener left and its failure joined, got %q and %v", strings.Join(payload.steps, "|"), err)
    }

    exiting := &guardRecordingPayload{}
    exitingDispatcher, _ := testNewEventDispatcher()
    exitingDispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"t"}})
    exitingDispatcher.AddListener("e", recordingListener(exiting, "failing", errors.New("failure")), eventcontract.ListenerConfig{Priority: 20, Name: "failing"})
    exitingDispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
        exception.Exit(exception.NewExitError(3, exception.NewError("the command is done", nil, nil)))

        return nil
    }, eventcontract.ListenerConfig{Priority: 10, Name: "exiting", Tags: []string{"t"}})

    func() {
        defer func() {
            recoveredValue := recover()
            if _, isExit := recoveredValue.(*exception.ExitError); false == isExit {
                t.Fatalf("expected the exit to pass through, got %v", recoveredValue)
            }
        }()

        _, _ = exitingDispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", exiting, clockInstance.Now()))
    }()

    if "run failing|enter e exiting closed|leave exiting" != strings.Join(exiting.steps, "|") {
        t.Fatalf("expected the exiting listener left, got %q", strings.Join(exiting.steps, "|"))
    }
}

func TestDispatch_APayloadThatGuardsNothingIsDispatchedUnchanged(t *testing.T) {
    for name, payload := range map[string]any{"plain": "payload", "nil": nil, "typed nil guard": (*guardRecordingPayload)(nil)} {
        dispatcher, clockInstance := testNewEventDispatcher()
        dispatcher.DeclareEvent("e", eventcontract.EventConfig{RunAfterFailureTags: []string{"t"}})
        dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return errors.New("failure")
        }, eventcontract.ListenerConfig{Priority: 20})

        ran := false
        dispatcher.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            ran = true

            return nil
        }, eventcontract.ListenerConfig{Priority: 10, Tags: []string{"t"}, ReplacesResponseAfterFailure: true})

        if _, err := dispatcher.Dispatch(newEventDispatcherAdapterTestRuntime(t), NewEvent("e", payload, clockInstance.Now())); nil == err || false == ran {
            t.Fatalf("%s: expected the tagged listener to run and the failure answered, got ran %v and %v", name, ran, err)
        }
    }
}

func TestRegisteredEvents_ReportsReplacesResponseAfterFailure(t *testing.T) {
    for _, door := range []bool{true, false} {
        dispatcher, _ := testNewEventDispatcher()
        adapter := NewEventDispatcherAdapter(dispatcher)

        adapter.AddListener("e", func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            return nil
        }, eventcontract.ListenerConfig{Tags: []string{"t"}, ReplacesResponseAfterFailure: door})

        for name, registeredEvents := range map[string][]eventcontract.RegisteredEvent{"dispatcher": dispatcher.RegisteredEvents(), "adapter": adapter.RegisteredEvents()} {
            if 1 != len(registeredEvents) || 1 != len(registeredEvents[0].Listeners) || door != registeredEvents[0].Listeners[0].ReplacesResponseAfterFailure {
                t.Fatalf("%s: expected the door reported as %v, got %+v", name, door, registeredEvents)
            }
        }
    }
}
