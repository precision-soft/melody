package messagebus

import (
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func TestHandlerLocator_ConcurrentRegisterAndLookup(t *testing.T) {
    locator := NewHandlerLocator()

    var waitGroup sync.WaitGroup

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated, envelope messagebuscontract.Envelope) error {
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

type locatorNilPinMessage struct{}

/* a nil message is refused by the function handler as an unexpected type, the actual type spelled "<nil>", where reflect.TypeOf(nil).String() panicked. */
func TestFunctionHandler_RefusesANilMessageWithoutPanicking(t *testing.T) {
    handler := &functionHandler[locatorNilPinMessage]{
        handle: func(runtimeInstance runtimecontract.Runtime, message locatorNilPinMessage, envelope messagebuscontract.Envelope) error { return nil },
    }

    handleErr := handler.Handle(nil, nil)
    melodyErr, isMelodyErr := handleErr.(*exception.Error)
    if false == isMelodyErr || "message handler received unexpected message type" != melodyErr.Message() {
        t.Fatalf("expected the nil message to be refused, got %v", handleErr)
    }

    if "<nil>" != melodyErr.Context()["actualType"] {
        t.Fatalf("expected the actual type spelled <nil>, got %v", melodyErr.Context())
    }
}

func TestRegisterHandler_RefusesANilHandlerFunction(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "message handler function is nil" != recoveredErr.Error() {
            t.Fatalf("expected the nil handler refused at registration, got %v", recoveredErr)
        }
    }()

    RegisterHandler[consumeTestMessage](NewHandlerLocator(), nil)
}

func TestRegisterHandler_HandsTheEnvelopeOfTheDeliveryToTheHandler(t *testing.T) {
    locator := NewHandlerLocator()

    var seenId string
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated, envelope messagebuscontract.Envelope) error {
        seenId, _ = MessageId(envelope)

        return nil
    })

    envelope := NewEnvelope(taskCreated{}, []messagebuscontract.Stamp{MessageIdStamp{MessageId: "message-1"}})
    handlers := locator.HandlersFor(taskCreated{})
    if 1 != len(handlers) {
        t.Fatalf("expected one handler, got %d", len(handlers))
    }

    if handleErr := handlers[0].Handle(nil, envelope); nil != handleErr || "message-1" != seenId {
        t.Fatalf("expected the handler to read the delivery's message id, got %q (%v)", seenId, handleErr)
    }
}

func TestHandleMessageMiddleware_HandsTheDispatchedEnvelopeToTheHandler(t *testing.T) {
    locator := NewHandlerLocator()

    var seenId string
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated, envelope messagebuscontract.Envelope) error {
        seenId, _ = MessageId(envelope)

        return nil
    })

    bus := NewManager("default", []messagebuscontract.Middleware{NewHandleMessageMiddleware(locator, HandleOptions{})})
    if _, dispatchErr := bus.Dispatch(newTestRuntime(), taskCreated{}, []messagebuscontract.Stamp{MessageIdStamp{MessageId: "dispatched-1"}}); nil != dispatchErr {
        t.Fatalf("dispatch: %v", dispatchErr)
    }

    if "dispatched-1" != seenId {
        t.Fatalf("expected the handler to read the dispatched envelope's message id, got %q", seenId)
    }
}
