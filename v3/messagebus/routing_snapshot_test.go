package messagebus_test

import (
    "testing"

    "github.com/precision-soft/melody/v3/messagebus"
)

func TestRouting_BuildSnapshotIgnoresRoutesAddedAfterBuild(t *testing.T) {
    lateTransport := messagebus.NewInMemoryTransport(4)

    routing := messagebus.NewRouting()
    middleware := messagebus.NewSendMessageMiddlewareFromRouting(routing)

    messagebus.RouteType[taskCreated](routing, "async", lateTransport)

    dispatchBus := messagebus.NewManager("default", middleware)

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(newTestRuntime(), taskCreated{TaskId: 7})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if _, hasSentStamp := messagebus.LastStampOfType[messagebus.SentStamp](sentEnvelope); true == hasSentStamp {
        t.Fatalf("middleware routed a message via a route registered after the middleware was built; build() must take a snapshot")
    }
}
