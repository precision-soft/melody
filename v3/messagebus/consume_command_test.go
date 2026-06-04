package messagebus

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type consumeTestMessage struct {
    Value int
}

func TestConsumeFrom_StopsAtLimitAndHandlesMessages(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    transport := NewInMemoryTransport(8)

    firstSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 1}))
    secondSendErr := transport.Send(runtimeInstance, NewEnvelope(consumeTestMessage{Value: 2}))
    if nil != firstSendErr || nil != secondSendErr {
        t.Fatalf("unexpected send error: %v %v", firstSendErr, secondSendErr)
    }

    locator := NewHandlerLocator()
    var sum int
    RegisterHandler(locator, func(runtimeInstance runtimecontract.Runtime, message consumeTestMessage) error {
        sum += message.Value
        return nil
    })

    bus := NewManager("default", NewHandleMessageMiddleware(locator))
    command := NewConsumeCommand(bus, nil)

    consumeErr := command.consumeFrom(runtimeInstance, transport, 2)
    if nil != consumeErr {
        t.Fatalf("unexpected consume error: %v", consumeErr)
    }

    if 3 != sum {
        t.Fatalf("expected handlers to sum to 3, got %d", sum)
    }
}
