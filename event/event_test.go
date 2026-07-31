package event

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/clock"
    clockcontract "github.com/precision-soft/melody/clock/contract"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func testNewEventDispatcher() (*EventDispatcher, clockcontract.Clock) {
    clockInstance := clock.NewSystemClock()
    dispatcher := NewEventDispatcher(clockInstance)

    return dispatcher, clockInstance
}

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

func TestEvent_StopPropagation(t *testing.T) {
    eventInstance := NewEvent("e", nil, clock.NewSystemClock())

    if true == eventInstance.IsPropagationStopped() {
        t.Fatalf("expected propagation not stopped initially")
    }

    eventInstance.StopPropagation()

    if false == eventInstance.IsPropagationStopped() {
        t.Fatalf("expected propagation stopped")
    }
}

func TestEvent_Constructors(t *testing.T) {
    timestamp := time.Unix(123, 0)

    original := NewEventWithTimestamp("e", "p", timestamp)
    copied := NewEventFromEvent(original)

    if "e" != copied.Name() {
        t.Fatalf("unexpected name")
    }
    if "p" != copied.Payload().(string) {
        t.Fatalf("unexpected payload")
    }
    if timestamp != copied.Timestamp() {
        t.Fatalf("unexpected timestamp")
    }
}

func TestEvent_Constructors_PanicOnEmptyName(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewEvent("", nil, clock.NewSystemClock())
    }, "event name may not be empty")

    testhelper.AssertPanicsWithError(t, func() {
        NewEventWithTimestamp("", nil, time.Now())
    }, "event name may not be empty")

    /* @info the source event carries the empty name rather than being built with one: NewEventWithTimestamp("", ...) panics while the argument is being evaluated, so writing it inline never enters NewEventFromEvent at all and only repeats the assertion above. An event with no name cannot be built through the constructors, which is the point — it can still arrive as a foreign implementation of the contract, and this is the path that rejects it. */
    testhelper.AssertPanicsWithError(t, func() {
        NewEventFromEvent(&Event{})
    }, "event name may not be empty")
}

/* @info a nil event reaches its own guard before anything is read off it, and a typed nil has to reach it too: the parameter is an interface, so a (*Event)(nil) is not equal to nil and would otherwise dereference inside Name(). */
func TestEvent_Constructors_PanicOnNilEvent(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewEventFromEvent(nil)
    }, "event value may not be nil")

    testhelper.AssertPanicsWithError(t, func() {
        NewEventFromEvent((*Event)(nil))
    }, "event value may not be nil")
}

/* @info the stop is part of what the event says about itself, and a copy that dropped it told a listener the propagation was live when it had already been stopped */
func TestNewEventFromEvent_CarriesTheStoppedPropagation(t *testing.T) {
    original := NewEventWithTimestamp("e", nil, time.Unix(123, 0))
    original.StopPropagation()

    copied := NewEventFromEvent(original)

    if false == copied.IsPropagationStopped() {
        t.Fatalf("expected the copy to carry the stopped propagation")
    }
}

/* @info an event that was never stopped must not be copied into one that claims it was */
func TestNewEventFromEvent_KeepsALivePropagationLive(t *testing.T) {
    original := NewEventWithTimestamp("e", nil, time.Unix(123, 0))

    copied := NewEventFromEvent(original)

    if true == copied.IsPropagationStopped() {
        t.Fatalf("expected the copy to keep the propagation live")
    }
}
