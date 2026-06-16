package messagebus

import (
    "testing"
)

func TestHandle_NoHandlerPassesThroughByDefault(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("default", NewHandleMessageMiddleware(locator))

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}); nil != dispatchErr {
        t.Fatalf("expected the default middleware to pass an unhandled message through, got: %v", dispatchErr)
    }
}

func TestHandle_RequireHandlerRejectsUnhandledMessage(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager(
        "default",
        NewHandleMessageMiddlewareWithOptions(locator, HandleOptions{RequireHandler: true}),
    )

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 1}); nil == dispatchErr {
        t.Fatalf("expected a missing-handler error in strict mode")
    }
}
