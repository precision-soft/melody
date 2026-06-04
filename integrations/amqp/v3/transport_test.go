package amqp_test

import (
    "context"
    "os"
    "testing"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    "github.com/precision-soft/melody/v3/container"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    "github.com/precision-soft/melody/v3/runtime"
)

type testMessage struct {
    Id   int
    Name string
}

func TestTransport_SendReceiveAck(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := amqp.NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[testMessage](registry, "amqp.test.message")

    transport := amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.test",
        Prefetch:   10,
        Registry:   registry,
        DeadLetter: true,
    })
    defer transport.Close()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    sent := []testMessage{
        {Id: 1, Name: "one"},
        {Id: 2, Name: "two"},
    }

    for _, messageInstance := range sent {
        sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(messageInstance))
        if nil != sendErr {
            t.Fatalf("send: %v", sendErr)
        }
    }

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    received := make(map[int]string)
    timeout := time.After(10 * time.Second)

    for len(received) < len(sent) {
        select {
        case envelopeInstance := <-queue:
            messageInstance, isType := envelopeInstance.Message().(testMessage)
            if false == isType {
                t.Fatalf("unexpected message type %T", envelopeInstance.Message())
            }

            received[messageInstance.Id] = messageInstance.Name

            ackErr := transport.Ack(runtimeInstance, envelopeInstance)
            if nil != ackErr {
                t.Fatalf("ack: %v", ackErr)
            }
        case <-timeout:
            t.Fatalf("timed out waiting for messages, received=%v", received)
        }
    }

    if "one" != received[1] || "two" != received[2] {
        t.Fatalf("unexpected payloads: %v", received)
    }
}
