package messagebus

import (
    "testing"
)

func TestHandle_NoHandlerIsRefusedByDefault(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("default", NewHandleMessageMiddleware(locator))

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}); nil == dispatchErr {
        t.Fatalf("expected the default middleware to refuse an unhandled message: on the consume path a pass-through is Acked and the message is drained")
    }
}

func TestHandle_AllowMissingHandlerPassesUnhandledMessageThrough(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        NewHandleMessageMiddlewareWithOptions(locator, HandleOptions{AllowMissingHandler: true}),
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}); nil != dispatchErr {
        t.Fatalf("expected the opted-in middleware to pass an unhandled message through, got: %v", dispatchErr)
    }
}
