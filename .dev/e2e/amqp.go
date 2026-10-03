package main

import (
    "strconv"
    "sync"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

type amqpOrder struct {
    Id   int    `json:"id"`
    Name string `json:"name"`
}

/* runAmqpCheck exercises a full publish → consume → ack round-trip against a live rabbitmq, and verifies a producer-assigned message id survives the round-trip (so an at-least-once consumer can deduplicate). It uses a per-run queue name so leftover messages from an earlier run never bleed into this one. */
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
    teardown := amqpProbeTeardown(dsn, transport, amqpProbeQueueNameList(queueName, amqpDefaultDelayBucketList), nil)
    removeTeardownOnFailure := pushFailureCleanup(teardown)
    defer removeTeardownOnFailure()
    defer teardown()

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


const amqpRedeliveryDelay = 2 * time.Second

/* runAmqpDelayedRedeliveryCheck drives the retry path a consumer's RetryPolicy takes: a delivery handed back with a DelayStamp is parked in the delay bucket and returns only once the bucket's ttl expired, carrying the redelivery count it was requeued with; refused without requeue it lands in the dead-letter queue. The dead-letter depth is read out of band, on a connection of the harness's own through a passive declare, so the transport's own view of the queue proves nothing here. */
func runAmqpDelayedRedeliveryCheck(dsn string) {
    runtimeInstance := newRuntime()

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[amqpOrder](registry, "amqp.e2e.order")

    queueName := "melody-e2e-redelivery-" + strconv.FormatInt(time.Now().UnixNano(), 10)

    transport := amqp.NewTransport(amqp.TransportConfig{
        Dialer:       amqp.NewProvider().Dialer(dsn),
        Queue:        queueName,
        Registry:     registry,
        DeadLetter:   true,
        DelayBuckets: []time.Duration{amqpRedeliveryDelay},
    })

    teardown := amqpProbeTeardown(dsn, transport, append(amqpProbeQueueNameList(queueName, []time.Duration{amqpRedeliveryDelay}), queueName+".dlq"), []string{queueName + ".dlx"})
    removeTeardownOnFailure := pushFailureCleanup(teardown)
    defer removeTeardownOnFailure()
    defer teardown()

    inspection, inspectionErr := amqp091.Dial(dsn)
    if nil != inspectionErr {
        fail("amqp redelivery: dial the inspection connection: %v", inspectionErr)
    }
    defer inspection.Close()

    inspectionChannel, channelErr := inspection.Channel()
    if nil != channelErr {
        fail("amqp redelivery: open the inspection channel: %v", channelErr)
    }
    defer inspectionChannel.Close()

    envelope := melodymessagebus.NewEnvelope(
        amqpOrder{Id: 3, Name: "retried"},
        melodymessagebus.MessageIdStamp{MessageId: "order-3"},
    )
    if sendErr := transport.Send(runtimeInstance, envelope); nil != sendErr {
        fail("amqp redelivery: send: %v", sendErr)
    }

    queue, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        fail("amqp redelivery: receive: %v", receiveErr)
    }

    first := receiveAmqpOrder(queue, 15*time.Second, "the first delivery")

    requeuedAt := time.Now()
    retried := first.WithStamp(
        melodymessagebus.RedeliveryStamp{Count: 1},
        melodymessagebus.DelayStamp{Delay: amqpRedeliveryDelay},
    )
    if nackErr := transport.Nack(runtimeInstance, retried, true); nil != nackErr {
        fail("amqp redelivery: requeue with a delay: %v", nackErr)
    }

    select {
    case early := <-queue:
        fail("amqp redelivery: the requeued delivery came back after %s, before the %s delay: %v", time.Since(requeuedAt), amqpRedeliveryDelay, early.Message())
    case <-time.After(amqpRedeliveryDelay - 500*time.Millisecond):
    }

    second := receiveAmqpOrder(queue, 15*time.Second, "the delayed redelivery")
    elapsed := time.Since(requeuedAt)

    if 1 != melodymessagebus.RedeliveryCount(second) {
        fail("amqp redelivery: expected the redelivery to carry count 1, got %d", melodymessagebus.RedeliveryCount(second))
    }

    if messageId, hasMessageId := melodymessagebus.MessageId(second); false == hasMessageId || "order-3" != messageId {
        fail("amqp redelivery: expected the redelivery under message id order-3, got %q", messageId)
    }
    pass("a delivery requeued with a %s delay came back after %s, not before, carrying redelivery count 1 and its message id", amqpRedeliveryDelay, elapsed.Round(10*time.Millisecond))

    if nackErr := transport.Nack(runtimeInstance, second, false); nil != nackErr {
        fail("amqp redelivery: refuse without requeue: %v", nackErr)
    }

    deadLetterDepth := 0
    deadline := time.Now().Add(10 * time.Second)
    for time.Now().Before(deadline) {
        deadLetterQueue, inspectErr := inspectionChannel.QueueDeclarePassive(queueName+".dlq", true, false, false, false, nil)
        if nil != inspectErr {
            fail("amqp redelivery: inspect the dead-letter queue out of band: %v", inspectErr)
        }

        deadLetterDepth = deadLetterQueue.Messages
        if 1 <= deadLetterDepth {
            break
        }

        time.Sleep(100 * time.Millisecond)
    }

    if 1 != deadLetterDepth {
        fail("amqp redelivery: expected the refused delivery in %s.dlq, read %d messages there out of band", queueName, deadLetterDepth)
    }

    /* the transport's consumer is closed first: while it is attached, a delivery wrongly requeued to it sits unacknowledged at the consumer and the passive declare counts only the ready messages, so the empty main queue would prove nothing. Closed, every unacknowledged delivery returns to the queue where the count sees it. */
    if closeErr := transport.Close(); nil != closeErr {
        fail("amqp redelivery: close the transport before reading the main queue: %v", closeErr)
    }

    mainQueue, inspectErr := inspectionChannel.QueueDeclarePassive(queueName, true, false, false, false, nil)
    if nil != inspectErr {
        fail("amqp redelivery: inspect the main queue out of band: %v", inspectErr)
    }

    if 0 != mainQueue.Messages {
        fail("amqp redelivery: expected the main queue empty after the dead-lettering, read %d messages out of band", mainQueue.Messages)
    }
    pass("refused without requeue, the delivery is the one message in %s.dlq and the main queue, read after the consumer closed, is empty, both out of band", queueName)
}

func receiveAmqpOrder(queue <-chan messagebuscontract.Envelope, within time.Duration, what string) messagebuscontract.Envelope {
    select {
    case envelope := <-queue:
        if _, isOrder := envelope.Message().(amqpOrder); false == isOrder {
            fail("amqp redelivery: %s: unexpected message type %T", what, envelope.Message())
        }

        return envelope
    case <-time.After(within):
        fail("amqp redelivery: timed out waiting for %s", what)
    }

    return nil
}

/* amqpDefaultDelayBucketList mirrors the transport's defaultDelayBuckets, which a transport declared with no DelayBuckets declares a queue for each of. The harness cannot read the unexported list, so a change there leaves queues behind here until this list follows it. */
var amqpDefaultDelayBucketList = []time.Duration{5 * time.Second, time.Minute, 10 * time.Minute, time.Hour}

/* amqpProbeQueueNameList answers every queue a transport declares for its queue and delay buckets: the queue itself, the per-message-ttl delay queue and one queue per bucket. */
func amqpProbeQueueNameList(queueName string, bucketList []time.Duration) []string {
    nameList := []string{queueName, queueName + ".delay"}
    for _, bucket := range bucketList {
        nameList = append(nameList, queueName+".delay."+strconv.FormatInt(bucket.Milliseconds(), 10)+"ms")
    }

    return nameList
}

/* amqpProbeTeardown answers the teardown of a probe's transport and of the queues and exchanges it declared, on a connection of its own. The transport is closed before its queues are deleted, since a consumer whose queue the broker deletes under it reports the lost channel as a failure. The section registers it both deferred and as a failure cleanup, because fail() exits without running deferred functions; the once keeps the two from running it twice. */
func amqpProbeTeardown(dsn string, transport *amqp.Transport, queueNameList []string, exchangeNameList []string) func() {
    var once sync.Once

    return func() {
        once.Do(func() {
            _ = transport.Close()

            connection, dialErr := amqp091.Dial(dsn)
            if nil != dialErr {
                return
            }
            defer connection.Close()

            channel, channelErr := connection.Channel()
            if nil != channelErr {
                return
            }
            defer channel.Close()

            for _, name := range queueNameList {
                _, _ = channel.QueueDelete(name, false, false, false)
            }
            for _, name := range exchangeNameList {
                _ = channel.ExchangeDelete(name, false, false)
            }
        })
    }
}
