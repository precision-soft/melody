package messagebus

import (
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/exception"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

func TestHandlerLocator_ConcurrentRegisterAndLookup(t *testing.T) {
    locator := NewHandlerLocator()

    var waitGroup sync.WaitGroup

    for index := 0; index < 50; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message taskCreated) error {
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
        handle: func(runtimeInstance runtimecontract.Runtime, message locatorNilPinMessage) error { return nil },
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
