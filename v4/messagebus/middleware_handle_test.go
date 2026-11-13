package messagebus

import (
    "testing"

    "github.com/precision-soft/melody/v4/internal/testhelper"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

func TestHandle_NoHandlerIsRefusedByDefault(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, nil); nil == dispatchErr {
        t.Fatalf("expected the default middleware to refuse an unhandled message: on the consume path a pass-through is Acked and the message is drained")
    }
}

func TestHandle_AllowMissingHandlerPassesUnhandledMessageThrough(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        []messagebuscontract.Middleware{
            NewHandleMessageMiddleware(locator, HandleOptions{AllowMissingHandler: true}),
        },
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, nil); nil != dispatchErr {
        t.Fatalf("expected the opted-in middleware to pass an unhandled message through, got: %v", dispatchErr)
    }
}

func TestHandle_ZeroOptionsRefuse(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        []messagebuscontract.Middleware{
            NewHandleMessageMiddleware(locator, HandleOptions{}),
        },
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}, nil); nil == dispatchErr {
        t.Fatalf("expected the zero options to refuse an unhandled message")
    }
}

func TestNewHandleMessageMiddleware_RefusesANilLocator(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewHandleMessageMiddleware(nil, HandleOptions{})
    }, "message handler locator is nil")
}
