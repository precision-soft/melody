package amqp

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

type closeUnblockMessage struct {
    Id int
}

func TestForwardDeliveries_CloseUnblocksGoroutineParkedOnOutput(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[closeUnblockMessage](registry, "amqp.test.close-unblock")

    transport := NewTransport(TransportConfig{
        Dialer:   func() (*amqp091.Connection, error) { return nil, nil },
        Queue:    "melody.amqp.close-unblock",
        Registry: registry,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    body, marshalErr := json.Marshal(closeUnblockMessage{Id: 1})
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    deliveries := make(chan amqp091.Delivery, 1)
    deliveries <- amqp091.Delivery{
        Headers:     amqp091.Table{headerMessageType: "amqp.test.close-unblock"},
        Body:        body,
        DeliveryTag: 1,
    }

    out := make(chan messagebuscontract.Envelope)
    done := make(chan forwardReason, 1)

    go func() {
        done <- transport.forwardDeliveries(runtimeInstance, nil, deliveries, out)
    }()

    time.Sleep(50 * time.Millisecond)

    transport.Close(runtimeInstance)

    select {
    case reason := <-done:
        if forwardDone != reason {
            t.Fatalf("expected forwardDone after Close, got %v", reason)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("forwardDeliveries did not return after Close — the consume goroutine leaked")
    }
}
