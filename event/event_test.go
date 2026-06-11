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

/** @info shared test helpers */

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

/** @info event */

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
    testhelper.AssertPanics(t, func() {
        NewEvent("", nil, clock.NewSystemClock())
    })

    testhelper.AssertPanics(t, func() {
        NewEventWithTimestamp("", nil, time.Now())
    })

    testhelper.AssertPanics(t, func() {
        NewEventFromEvent(NewEventWithTimestamp("", nil, time.Now()))
    })
}
