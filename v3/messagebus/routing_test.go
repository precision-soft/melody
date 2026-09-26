package messagebus

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestRouting_RouteTypeDispatchesToTransport(t *testing.T) {
    transport := NewInMemoryTransport(4)

    routing := NewRouting()
    RouteType[taskCreated](routing, "async", transport)

    dispatchBus := NewManager("default", NewSendMessageMiddlewareFromRouting(routing))

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(newTestRuntime(), taskCreated{TaskId: 9})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    _, hasSentStamp := LastStampOfType[SentStamp](sentEnvelope)
    if false == hasSentStamp {
        t.Fatalf("expected a sent stamp on the envelope routed via RouteType")
    }
}

func TestRouting_BuildSnapshotIgnoresRoutesAddedAfterBuild(t *testing.T) {
    lateTransport := NewInMemoryTransport(4)

    routing := NewRouting()
    middleware := NewSendMessageMiddlewareFromRouting(routing)

    RouteType[taskCreated](routing, "async", lateTransport)

    dispatchBus := NewManager("default", middleware)

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(newTestRuntime(), taskCreated{TaskId: 7})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if _, hasSentStamp := LastStampOfType[SentStamp](sentEnvelope); true == hasSentStamp {
        t.Fatalf("middleware routed a message via a route registered after the middleware was built; build() must take a snapshot")
    }
}

func TestRouteType_RefusesANilTransportAtRegistration(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        RouteType[taskCreated](NewRouting(), "async", nil)
    }, "messagebus route transport is nil")
}

func TestRouteType_RefusesATypedNilTransportAtRegistration(t *testing.T) {
    var typedNil *InMemoryTransport

    testhelper.AssertPanicsWithError(t, func() {
        RouteType[taskCreated](NewRouting(), "async", typedNil)
    }, "messagebus route transport is nil")
}
