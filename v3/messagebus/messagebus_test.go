package messagebus_test

import (
    "context"
    "reflect"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/messagebus"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type taskCreated struct {
    TaskId int
}

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestDispatch_RunsRegisteredHandler(t *testing.T) {
    locator := messagebus.NewHandlerLocator()

    var handled int
    messagebus.RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
        handled = message.TaskId
        return nil
    })

    bus := messagebus.NewManager("default", messagebus.NewHandleMessageMiddleware(locator))

    envelopeInstance, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{TaskId: 42})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if 42 != handled {
        t.Fatalf("expected handler to receive 42, got %d", handled)
    }

    _, hasHandledStamp := messagebus.LastStampOfType[messagebus.HandledStamp](envelopeInstance)
    if false == hasHandledStamp {
        t.Fatalf("expected handled stamp on envelope")
    }
}

func TestHandlerLocator_ConcurrentRegisterAndLookup(t *testing.T) {
    locator := messagebus.NewHandlerLocator()

    var waitGroup sync.WaitGroup

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            messagebus.RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
                return nil
            })
        }()
    }

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for _, handler := range locator.HandlersFor(taskCreated{}) {
                _ = handler
            }
        }()
    }

    waitGroup.Wait()
}

func TestSendMiddleware_RoutesToTransportAndWorkerHandles(t *testing.T) {
    transport := messagebus.NewInMemoryTransport(4)

    routing := map[reflect.Type]messagebus.TransportRouting{
        reflect.TypeOf(taskCreated{}): {Name: "async", Transport: transport},
    }

    dispatchBus := messagebus.NewManager("default", messagebus.NewSendMessageMiddleware(routing))

    runtimeInstance := newTestRuntime()

    sentEnvelope, dispatchErr := dispatchBus.Dispatch(runtimeInstance, taskCreated{TaskId: 7})
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    _, hasSentStamp := messagebus.LastStampOfType[messagebus.SentStamp](sentEnvelope)
    if false == hasSentStamp {
        t.Fatalf("expected sent stamp on envelope")
    }

    locator := messagebus.NewHandlerLocator()
    var handled int
    messagebus.RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
        handled = message.TaskId
        return nil
    })

    consumeBus := messagebus.NewManager("default", messagebus.NewHandleMessageMiddleware(locator))

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("unexpected receive error: %v", receiveErr)
    }

    received := <-queue
    _, dispatchErr = consumeBus.Dispatch(runtimeInstance, received)
    if nil != dispatchErr {
        t.Fatalf("unexpected consume error: %v", dispatchErr)
    }

    if 7 != handled {
        t.Fatalf("expected worker handler to receive 7, got %d", handled)
    }
}
