package messagebus

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func TestDispatch_RunsRegisteredHandler(t *testing.T) {
    locator := NewHandlerLocator()

    var handled int
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
        handled = message.TaskId
        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    envelopeInstance, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 42}, nil)
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

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    if _, dispatchErr := bus.Dispatch(newTestRuntime(), nil, nil); nil == dispatchErr {
        t.Fatalf("expected an error when dispatching a nil message")
    }
}

func TestDispatch_WarnsWhenNothingSentOrHandledTheMessage(t *testing.T) {
    routing := NewRouting()
    bus := NewManager("send-only", []messagebuscontract.Middleware{NewSendMessageMiddlewareFromRouting(routing)})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* a send-only bus with no route for the type answers success, so this record is the one channel that tells "delivered somewhere" from "did nothing" */
    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}, nil); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if false == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected the untouched dispatch to be logged")
    }
}

func TestDispatch_DoesNotWarnWhenTheMessageWasSent(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    routing := NewRouting()
    RouteType[taskCreated](routing, "async", transport)

    bus := NewManager("send-only", []messagebuscontract.Middleware{NewSendMessageMiddlewareFromRouting(routing)})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}, nil); nil != dispatchErr {
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

    bus := NewManager("handling", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    if _, dispatchErr := bus.Dispatch(runtimeInstance, taskCreated{TaskId: 1}, nil); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if true == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected a handled dispatch not to be flagged as untouched")
    }
}

func TestDispatch_DoesNotWarnForAReceivedEnvelope(t *testing.T) {
    locator := NewHandlerLocator()

    bus := NewManager("consume", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{AllowMissingHandler: true})})

    runtimeInstance, logger := newTestRuntimeWithRecordingLogger()

    /* the consume path dispatches received envelopes, and the handle middleware owns the missing-handler verdict there — the untouched-dispatch record must not fire beside it */
    received := NewEnvelope(taskCreated{TaskId: 1}, nil).WithStamp(ReceivedStamp{TransportName: "async"})
    if _, dispatchErr := bus.Dispatch(runtimeInstance, received, nil); nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if true == logger.hasMessageContaining("nothing sent or handled the message") {
        t.Fatalf("expected a received envelope not to be flagged as untouched")
    }
}

func TestNewManager_RefusesANilMiddlewareByItsIndex(t *testing.T) {
    middlewares := []messagebuscontract.Middleware{NewHandleMessageMiddleware(NewHandlerLocator(), HandleOptions{}), nil}

    testhelper.AssertPanicsWithError(t, func() {
        NewManager("default", middlewares)
    }, "message bus middleware is nil")

    context := recoveredPanicContext(func() {
        NewManager("default", middlewares)
    })
    if 1 != context["index"] || "default" != context["bus"] {
        t.Fatalf("expected the refusal to name the bus and index 1, got %v", context)
    }
}

func TestManager_DispatchAddsTheStampsAfterTheEnvelopeOwn(t *testing.T) {
    bus := NewManager("default", nil)

    result, dispatchErr := bus.Dispatch(
        newTestRuntime(),
        NewEnvelope(taskCreated{TaskId: 1}, []messagebuscontract.Stamp{MessageIdStamp{MessageId: "own"}}),
        []messagebuscontract.Stamp{DelayStamp{Delay: time.Second}},
    )
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    stamps := result.Stamps()
    if 3 != len(stamps) || StampNameMessageId != stamps[0].StampName() || StampNameDelay != stamps[1].StampName() {
        t.Fatalf("expected the envelope's own stamp, then the dispatched one, then the bus name, got %v", stamps)
    }
}
