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

func TestDispatch_WarnsWhenNothingSentOrHandledTheMessage(t *testing.T) {
    routing := NewRouting()
    bus := NewManager("send-only", NewSendMessageMiddlewareFromRouting(routing))

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* a send-only bus with no route for the type used to be a total silent no-op reported as success: the caller's success path ran, the outbox row was marked relayed, and the message ceased to exist — this record is the one channel that can see the difference between "delivered somewhere" and "did absolutely nothing" */
    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if false == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected the untouched dispatch to be logged")
    }
}

func TestDispatch_DoesNotWarnWhenTheMessageWasSent(t *testing.T) {
    transport := NewInMemoryTransport(4)

    routing := NewRouting()
    RouteType[taskCreated](routing, "async", transport)

    bus := NewManager("send-only", NewSendMessageMiddlewareFromRouting(routing))

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if true == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected a routed dispatch not to be flagged as untouched")
    }
}

func TestDispatch_DoesNotWarnWhenTheMessageWasHandled(t *testing.T) {
    locator := NewHandlerLocator()
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
        return nil
    })

    bus := NewManager("handling", NewHandleMessageMiddleware(locator))

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if true == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected a handled dispatch not to be flagged as untouched")
    }
}

func TestDispatch_DoesNotWarnForAReceivedEnvelope(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("consume", NewHandleMessageMiddlewareWithOptions(locator, HandleOptions{AllowMissingHandler: true}))

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* the consume path dispatches received envelopes, and the handle middleware owns the missing-handler verdict there — the untouched-dispatch record must not fire beside it */
    received := NewEnvelope(taskCreated{TaskId: 1}).WithStamp(ReceivedStamp{TransportName: "async"})
    if _, dispatchErr := bus.Dispatch(runtimeInstance, received); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if true == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected a received envelope not to be flagged as untouched")
    }
}
