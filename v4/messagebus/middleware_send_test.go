package messagebus

import (
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

func TestSendMiddleware_RoutesToTransportAndWorkerHandles(t *testing.T) {
    transport := NewInMemoryTransport(InMemoryTransportConfig{BufferSize: bound.Of(4)})

    routing := NewRouting()
    RouteType[taskCreated](routing, "async", transport)

    dispatchBus := NewManager("default", []messagebuscontract.Middleware{NewSendMessageMiddleware(routing)})

    runtimeInstance := newTestRuntime()

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(runtimeInstance, taskCreated{TaskId: 7}, nil)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    _, hasSentStamp := LastStampOfType[SentStamp](sentEnvelope)
    if false == hasSentStamp {
        t.Fatalf("expected sent stamp on envelope")
    }

    locator := NewHandlerLocator()
    var handled int
    RegisterHandler(locator, func(runtimeInstance containercontract.Runtime, message taskCreated, envelope messagebuscontract.Envelope) error {
        handled = message.TaskId
        return nil
    })

    consumeBus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("unexpected receive error: %v", receiveErr)
    }

    received := <-queue
    _, dispatchErr = consumeBus.Dispatch(runtimeInstance, received, nil)
    if nil != dispatchErr {
        t.Fatalf("unexpected consume error: %v", dispatchErr)
    }

    if 7 != handled {
        t.Fatalf("expected worker handler to receive 7, got %d", handled)
    }
}
