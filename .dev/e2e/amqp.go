package main

import (
    "strconv"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
)

type amqpOrder struct {
    Id   int    `json:"id"`
    Name string `json:"name"`
}

/* runAmqpCheck exercises a full publish → consume → ack round-trip against a live rabbitmq, and verifies a
producer-assigned message id survives the round-trip (so an at-least-once consumer can deduplicate). It
uses a per-run queue name so leftover messages from an earlier run never bleed into this one. */
func runAmqpCheck(dsn string) {
    runtimeInstance := newRuntime()

    provider := amqp.NewProvider()

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[amqpOrder](registry, "amqp.e2e.order")

    queueName := "melody-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 10)

    transport := amqp.NewTransport(amqp.TransportConfig{
        Dialer:   provider.Dialer(dsn),
        Queue:    queueName,
        Registry: registry,
    })
    defer transport.Close(runtimeInstance)

    /* publish two orders, each under a stable producer-assigned message id */
    sent := map[int]string{1: "widget", 2: "gadget"}
    for id, name := range sent {
        envelope := melodymessagebus.NewEnvelope(
            amqpOrder{Id: id, Name: name},
            melodymessagebus.MessageIdStamp{MessageId: "order-" + strconv.Itoa(id)},
        )
        if sendErr := transport.Send(runtimeInstance, envelope); nil != sendErr {
            fail("amqp send order-%d: %v", id, sendErr)
        }
    }
    pass("published 2 orders to queue %q with stable message ids", queueName)

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        fail("amqp receive: %v", receiveErr)
    }

    received := map[int]string{}
    ids := map[int]string{}
    timeout := time.After(15 * time.Second)

    for len(received) < len(sent) {
        select {
        case envelope := <-queue:
            order, isOrder := envelope.Message().(amqpOrder)
            if false == isOrder {
                fail("amqp: unexpected message type %T", envelope.Message())
            }

            received[order.Id] = order.Name
            if messageId, hasMessageId := melodymessagebus.MessageId(envelope); true == hasMessageId {
                ids[order.Id] = messageId
            }

            if ackErr := transport.Ack(runtimeInstance, envelope); nil != ackErr {
                fail("amqp ack order-%d: %v", order.Id, ackErr)
            }
        case <-timeout:
            fail("amqp: timed out waiting for messages, received=%v", received)
        }
    }

    for id, name := range sent {
        if received[id] != name {
            fail("amqp: order-%d payload mismatch: sent %q, received %q", id, name, received[id])
        }
        if expected := "order-" + strconv.Itoa(id); ids[id] != expected {
            fail("amqp: order-%d message id mismatch: expected %q, got %q", id, expected, ids[id])
        }
    }
    pass("consumed + acked 2 orders; payloads and message ids round-tripped %v", ids)
}
