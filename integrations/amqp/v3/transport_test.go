package amqp_test

import (
    "context"
    "os"
    "testing"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    "github.com/precision-soft/melody/v3/container"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    amqp091 "github.com/rabbitmq/amqp091-go"
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

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

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

func TestTransport_RequeuePersistsRedeliveryCountThenDeadLetters(t *testing.T) {
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
    amqp.RegisterMessage[testMessage](registry, "amqp.test.retry")

    queueName := "melody.amqp.retry"
    transport := amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Queue:      queueName,
        Prefetch:   1,
        Registry:   registry,
        DeadLetter: true,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "retry"})); nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    first := receiveWithin(t, queue, 10*time.Second)
    if 0 != melodymessagebus.RedeliveryCount(first) {
        t.Fatalf("expected initial redelivery count 0, got %d", melodymessagebus.RedeliveryCount(first))
    }

    retried := first.WithStamp(melodymessagebus.RedeliveryStamp{Count: 1})
    if nackErr := transport.Nack(runtimeInstance, retried, true); nil != nackErr {
        t.Fatalf("nack requeue: %v", nackErr)
    }

    second := receiveWithin(t, queue, 10*time.Second)
    if 1 != melodymessagebus.RedeliveryCount(second) {
        t.Fatalf("expected persisted redelivery count 1, got %d", melodymessagebus.RedeliveryCount(second))
    }

    if nackErr := transport.Nack(runtimeInstance, second, false); nil != nackErr {
        t.Fatalf("nack dead-letter: %v", nackErr)
    }

    if false == drainedToDeadLetter(t, connection, queueName+".dlq", 5*time.Second) {
        t.Fatalf("expected the message to land in the dead-letter queue")
    }
}

func TestTransport_DelayStampRoutesThroughDelayQueue(t *testing.T) {
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
    amqp.RegisterMessage[testMessage](registry, "amqp.test.delay")

    transport := amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.delay",
        Prefetch:   1,
        Registry:   registry,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "delay"})); nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    first := receiveWithin(t, queue, 10*time.Second)

    retried := first.
        WithStamp(melodymessagebus.RedeliveryStamp{Count: 1}).
        WithStamp(melodymessagebus.DelayStamp{Delay: 1500 * time.Millisecond})

    start := time.Now()
    if nackErr := transport.Nack(runtimeInstance, retried, true); nil != nackErr {
        t.Fatalf("nack requeue: %v", nackErr)
    }

    select {
    case <-queue:
        t.Fatalf("delayed message arrived before its delay elapsed")
    case <-time.After(700 * time.Millisecond):
    }

    second := receiveWithin(t, queue, 5*time.Second)
    if elapsed := time.Since(start); elapsed < 1200*time.Millisecond {
        t.Fatalf("delayed message returned too early after %s", elapsed)
    }

    if 1 != melodymessagebus.RedeliveryCount(second) {
        t.Fatalf("expected the delayed message to keep redelivery count 1, got %d", melodymessagebus.RedeliveryCount(second))
    }

    if ackErr := transport.Ack(runtimeInstance, second); nil != ackErr {
        t.Fatalf("ack: %v", ackErr)
    }
}

func TestTransport_ReconnectsAfterConnectionDrop(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := amqp.NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[testMessage](registry, "amqp.test.reconnect")

    queueName := "melody.amqp.reconnect"
    transport := amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Dialer:     provider.Dialer(dsn),
        Queue:      queueName,
        Prefetch:   1,
        Registry:   registry,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    if dropErr := connection.Close(); nil != dropErr {
        t.Fatalf("drop connection: %v", dropErr)
    }

    publisherConnection, publisherErr := provider.Open(dsn)
    if nil != publisherErr {
        t.Fatalf("open publisher connection: %v", publisherErr)
    }
    defer provider.Close(publisherConnection)

    publisher := amqp.NewTransport(amqp.TransportConfig{
        Connection: publisherConnection,
        Queue:      queueName,
        Registry:   registry,
    })
    defer publisher.Close(runtimeInstance)

    if sendErr := publisher.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 7, Name: "after-reconnect"})); nil != sendErr {
        t.Fatalf("send after drop: %v", sendErr)
    }

    deadline := time.After(20 * time.Second)
    for {
        select {
        case envelopeInstance := <-queue:
            messageInstance, isType := envelopeInstance.Message().(testMessage)
            if true == isType && 7 == messageInstance.Id {
                if ackErr := transport.Ack(runtimeInstance, envelopeInstance); nil != ackErr {
                    t.Logf("ack after reconnect (expected to occasionally fail on a rotated channel): %v", ackErr)
                }

                return
            }
        case <-deadline:
            t.Fatalf("expected the consumer to reconnect and deliver the message")
        }
    }
}

func receiveWithin(t *testing.T, queue <-chan messagebuscontract.Envelope, timeout time.Duration) messagebuscontract.Envelope {
    t.Helper()

    select {
    case envelopeInstance := <-queue:
        return envelopeInstance
    case <-time.After(timeout):
        t.Fatalf("timed out waiting for a message")
        return nil
    }
}

func drainedToDeadLetter(t *testing.T, connection *amqp091.Connection, deadLetterQueue string, timeout time.Duration) bool {
    t.Helper()

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        t.Fatalf("open inspection channel: %v", channelErr)
    }
    defer channel.Close()

    deadline := time.After(timeout)
    for {
        select {
        case <-deadline:
            return false
        default:
            message, ok, getErr := channel.Get(deadLetterQueue, true)
            if nil != getErr {
                t.Fatalf("get from dead-letter queue: %v", getErr)
            }

            if true == ok {
                _ = message

                return true
            }

            time.Sleep(100 * time.Millisecond)
        }
    }
}
