package messagebus

import (
    "testing"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestDispatch_RunsRegisteredHandler(t *testing.T) {
    locator := NewHandlerLocator()

    var handled int
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
        handled = message.TaskId
        return nil
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))

    envelopeInstance, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 42})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 42 != handled {
        t.Fatalf("expected handler to receive 42, got %d", handled)
    }

    _, hasHandledStamp := LastStampOfType[HandledStamp](envelopeInstance)
    if false == hasHandledStamp {
        t.Fatalf("expected handled stamp on envelope")
    }
}

func TestDispatch_NilMessageReturnsErrorInsteadOfPanicking(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("default", NewHandleMessageMiddleware(locator))

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), nil); nil == dispatchErr {
        t.Fatalf("expected an error when dispatching a nil message")
    }
}
