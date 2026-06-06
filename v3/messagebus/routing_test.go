package messagebus_test

import (
    "testing"

    "github.com/precision-soft/melody/v3/messagebus"
)

func TestRouting_RouteTypeDispatchesToTransport(t *testing.T) {
    transport := messagebus.NewInMemoryTransport(4)

    routing := messagebus.NewRouting()
    messagebus.RouteType[taskCreated](routing, "async", transport)

    dispatchBus := messagebus.NewManager("default", messagebus.NewSendMessageMiddlewareFromRouting(routing))

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(newTestRuntime(), taskCreated{TaskId: 9})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    _, hasSentStamp := messagebus.LastStampOfType[messagebus.SentStamp](sentEnvelope)
    if false == hasSentStamp {
        t.Fatalf("expected a sent stamp on the envelope routed via RouteType")
    }
}
