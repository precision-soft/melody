package amqp

import (
    "context"
    "encoding/json"
    "errors"
    "math"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

type testMessage struct {
    Id   int
    Name string
}

type reconnectMessage struct {
    Id int
}

type closeUnblockMessage struct {
    Id int
}

func newReconnectRuntime(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
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

/* @info send/receive integration */

func TestTransport_SendReceiveAck(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.message")

    transport := NewTransport(TransportConfig{
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

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.retry")

    queueName := "melody.amqp.retry"
    transport := NewTransport(TransportConfig{
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

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.delay")

    transport := NewTransport(TransportConfig{
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

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.test.reconnect")

    queueName := "melody.amqp.reconnect"
    transport := NewTransport(TransportConfig{
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

    publisher := NewTransport(TransportConfig{
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

/* @info reconnect and backoff */

func TestNextBackoff_GrowsAndCaps(t *testing.T) {
    expected := []time.Duration{
        2 * time.Second,
        4 * time.Second,
        8 * time.Second,
        16 * time.Second,
        30 * time.Second,
        30 * time.Second,
    }

    instance := &Transport{reconnect: resolveReconnectConfig(nil, nil)}

    current := instance.reconnect.InitialBackoff
    for index, want := range expected {
        current = nextReconnectBackoff(instance.reconnect, current)
        if want != current {
            t.Fatalf("step %d: expected %s, got %s", index, want, current)
        }
    }
}

func TestConnect_NoDialerReturnsError(t *testing.T) {
    instance := &Transport{queue: "orders"}

    _, connectErr := instance.connect()
    if nil == connectErr {
        t.Fatalf("expected an error when no connection and no dialer are configured")
    }
}

func TestConnect_DialFailureIsWrapped(t *testing.T) {
    calls := 0
    instance := &Transport{
        queue: "orders",
        dialer: func() (*amqp091.Connection, error) {
            calls++
            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    _, connectErr := instance.connect()
    if nil == connectErr {
        t.Fatalf("expected the dial failure to surface")
    }

    if 1 != calls {
        t.Fatalf("expected the dialer to be invoked once, got %d", calls)
    }

    if true == instance.reconnecting {
        t.Fatalf("expected the reconnecting flag to be cleared after a failed dial")
    }
}

func TestSubscribeWithRetry_NoDialerDoesNotLoop(t *testing.T) {
    instance := &Transport{queue: "orders", closeSignal: make(chan struct{})}

    _, _, subscribeErr := instance.subscribeWithRetry(newReconnectRuntime(context.Background()))
    if nil == subscribeErr {
        t.Fatalf("expected an error when no connection and no dialer are configured")
    }
}

func TestSubscribeWithRetry_RetriesThenStopsOnContextCancel(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    calls := 0
    instance := &Transport{
        queue:       "orders",
        closeSignal: make(chan struct{}),
        reconnect:   ReconnectConfig{InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffFactor: 2},
        dialer: func() (*amqp091.Connection, error) {
            calls++
            if 3 <= calls {
                cancel()
            }

            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    _, _, subscribeErr := instance.subscribeWithRetry(newReconnectRuntime(ctx))
    if nil == subscribeErr {
        t.Fatalf("expected an error after the context is cancelled")
    }

    if 3 > calls {
        t.Fatalf("expected the initial subscribe to be retried, got %d calls", calls)
    }
}

func TestSubscribeWithRetry_ZeroBackoffDoesNotBusyLoop(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    calls := 0
    instance := &Transport{
        queue:       "orders",
        closeSignal: make(chan struct{}),
        reconnect:   ReconnectConfig{InitialBackoff: 0, MaxBackoff: time.Second, BackoffFactor: 2},
        dialer: func() (*amqp091.Connection, error) {
            calls++

            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    go func() {
        time.Sleep(50 * time.Millisecond)
        cancel()
    }()

    _, _, subscribeErr := instance.subscribeWithRetry(newReconnectRuntime(ctx))
    if nil == subscribeErr {
        t.Fatalf("expected an error after the context is cancelled")
    }

    if 100 < calls {
        t.Fatalf("expected the zero initial backoff to be clamped, got %d dial attempts in 50ms", calls)
    }
}

func TestConsumeLoop_ZeroBackoffDoesNotBusyLoop(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    calls := 0
    instance := &Transport{
        queue:       "orders",
        closeSignal: make(chan struct{}),
        reconnect:   ReconnectConfig{InitialBackoff: 0, MaxBackoff: time.Second, BackoffFactor: 2},
        dialer: func() (*amqp091.Connection, error) {
            calls++

            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    out := make(chan messagebuscontract.Envelope)
    done := make(chan struct{})

    go func() {
        instance.consumeLoop(newReconnectRuntime(ctx), nil, deliveries, out)
        close(done)
    }()

    go func() {
        time.Sleep(50 * time.Millisecond)
        cancel()
    }()

    select {
    case <-done:
    case <-time.After(2 * time.Second):
        cancel()
        t.Fatalf("consumeLoop did not return after context cancellation")
    }

    if 100 < calls {
        t.Fatalf("expected the zero initial backoff to be clamped, got %d reconnect attempts in 50ms", calls)
    }
}

func TestConnect_SingleFlight(t *testing.T) {
    entered := make(chan struct{})
    release := make(chan struct{})

    instance := &Transport{
        queue: "orders",
        dialer: func() (*amqp091.Connection, error) {
            close(entered)
            <-release
            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    go instance.connect()

    <-entered

    _, secondErr := instance.connect()
    if errReconnectInProgress != secondErr {
        t.Fatalf("expected a concurrent connect to report reconnect-in-progress, got %v", secondErr)
    }

    close(release)
}

func TestForwardDeliveries_ChannelLost(t *testing.T) {
    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders"}
    out := make(chan messagebuscontract.Envelope, 1)

    reason := instance.forwardDeliveries(newReconnectRuntime(context.Background()), nil, deliveries, out)
    if forwardChannelLost != reason {
        t.Fatalf("expected forwardChannelLost, got %v", reason)
    }
}

func TestForwardDeliveries_ContextDone(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    instance := &Transport{queue: "orders"}
    deliveries := make(chan amqp091.Delivery)
    out := make(chan messagebuscontract.Envelope, 1)

    reason := instance.forwardDeliveries(newReconnectRuntime(ctx), nil, deliveries, out)
    if forwardDone != reason {
        t.Fatalf("expected forwardDone, got %v", reason)
    }
}

func TestConsumeLoop_NoDialerClosesOut(t *testing.T) {
    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders"}
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(newReconnectRuntime(context.Background()), nil, deliveries, out)

    select {
    case _, open := <-out:
        if true == open {
            t.Fatalf("expected out to be closed without delivering a message")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected consumeLoop to close out after the channel was lost")
    }
}

func TestDecode_StampsCurrentGeneration(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[reconnectMessage](registry, "amqp.test.gen")

    serializer := melodyserializer.NewJsonSerializer()
    instance := &Transport{queue: "orders", registry: registry, serializer: serializer}

    body, serializeErr := serializer.Serialize(reconnectMessage{Id: 1})
    if nil != serializeErr {
        t.Fatalf("serialize: %v", serializeErr)
    }

    delivery := amqp091.Delivery{
        Headers:     amqp091.Table{headerMessageType: "amqp.test.gen"},
        DeliveryTag: 5,
        Body:        body,
    }

    envelopeInstance, decodeErr := instance.decode(delivery, 9)
    if nil != decodeErr {
        t.Fatalf("decode: %v", decodeErr)
    }

    stamp, exists := melodymessagebus.LastStampOfType[DeliveryStamp](envelopeInstance)
    if false == exists {
        t.Fatalf("expected a delivery stamp on the decoded envelope")
    }

    if 9 != stamp.Generation {
        t.Fatalf("expected the delivery to carry generation 9, got %d", stamp.Generation)
    }
}

func TestTransport_RequeuePersistsDeadLetterAttemptCount(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[reconnectMessage](registry, "amqp.test.deadletter")

    serializer := melodyserializer.NewJsonSerializer()
    instance := &Transport{queue: "orders", registry: registry, serializer: serializer}

    envelope := melodymessagebus.NewEnvelope(reconnectMessage{Id: 1}).
        WithStamp(melodymessagebus.DeadLetterAttemptStamp{Count: 2})

    publishing, buildErr := instance.buildPublishing(envelope, "")
    if nil != buildErr {
        t.Fatalf("build publishing: %v", buildErr)
    }

    /* @important a requeued exhausted message must carry its dead-letter attempt count across the broker round-trip; MaxDeadLetterAttempts re-reads the count on every consume, so dropping it resets the counter to 0 on each requeue and the bound is never reached for a value >= 2, looping forever — the very loop the feature was added to break */
    delivery := amqp091.Delivery{Headers: publishing.Headers, Body: publishing.Body}
    decoded, decodeErr := instance.decode(delivery, 1)
    if nil != decodeErr {
        t.Fatalf("decode: %v", decodeErr)
    }

    if 2 != melodymessagebus.DeadLetterAttemptCount(decoded) {
        t.Fatalf("expected the decoded envelope to keep dead-letter attempt count 2, got %d", melodymessagebus.DeadLetterAttemptCount(decoded))
    }
}

/* a message id stamped by a producer (for example the outbox relay) is carried as the AMQP message id so a consumer can deduplicate at-least-once redeliveries. */
func TestTransport_BuildPublishingCarriesMessageId(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[reconnectMessage](registry, "amqp.test.messageid")

    serializer := melodyserializer.NewJsonSerializer()
    instance := &Transport{queue: "orders", registry: registry, serializer: serializer}

    envelope := melodymessagebus.NewEnvelope(reconnectMessage{Id: 1}).
        WithStamp(melodymessagebus.MessageIdStamp{MessageId: "melody-outbox-42"})

    publishing, buildErr := instance.buildPublishing(envelope, "")
    if nil != buildErr {
        t.Fatalf("build publishing: %v", buildErr)
    }

    if "melody-outbox-42" != publishing.MessageId {
        t.Fatalf("expected the stamped message id on the publishing, got %q", publishing.MessageId)
    }
}

/* the producer-assigned message id must survive a broker round-trip and an application requeue: decode reads delivery.MessageId back into a stamp so a consumer can read it and a republish (Nack-with-requeue / delayed retry) re-emits the SAME id rather than an empty one. */
func TestTransport_MessageIdSurvivesDecodeAndRepublish(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[reconnectMessage](registry, "amqp.test.messageid.roundtrip")

    serializer := melodyserializer.NewJsonSerializer()
    instance := &Transport{queue: "orders", registry: registry, serializer: serializer}

    /* first publish carries the producer id */
    sent := melodymessagebus.NewEnvelope(reconnectMessage{Id: 1}).
        WithStamp(melodymessagebus.MessageIdStamp{MessageId: "melody-outbox-42"})
    published, buildErr := instance.buildPublishing(sent, "")
    if nil != buildErr {
        t.Fatalf("build publishing: %v", buildErr)
    }

    /* the broker delivers it back; decode must expose the id as a stamp */
    delivery := amqp091.Delivery{
        Headers:   published.Headers,
        Body:      published.Body,
        MessageId: published.MessageId,
    }
    decoded, decodeErr := instance.decode(delivery, 1)
    if nil != decodeErr {
        t.Fatalf("decode: %v", decodeErr)
    }

    roundTripped, present := melodymessagebus.MessageId(decoded)
    if false == present || "melody-outbox-42" != roundTripped {
        t.Fatalf("expected decode to surface the message id, got %q present=%v", roundTripped, present)
    }

    /* a requeue re-publishes the decoded envelope; the id must not be lost */
    republished, republishErr := instance.buildPublishing(decoded, "")
    if nil != republishErr {
        t.Fatalf("rebuild publishing: %v", republishErr)
    }

    if "melody-outbox-42" != republished.MessageId {
        t.Fatalf("expected the republished message to keep its id, got %q", republished.MessageId)
    }
}

/* negative control: without a message id stamp the publishing leaves MessageId empty rather than inventing one. */
func TestTransport_BuildPublishingWithoutMessageIdStampLeavesItEmpty(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[reconnectMessage](registry, "amqp.test.nomessageid")

    serializer := melodyserializer.NewJsonSerializer()
    instance := &Transport{queue: "orders", registry: registry, serializer: serializer}

    publishing, buildErr := instance.buildPublishing(melodymessagebus.NewEnvelope(reconnectMessage{Id: 1}), "")
    if nil != buildErr {
        t.Fatalf("build publishing: %v", buildErr)
    }

    if "" != publishing.MessageId {
        t.Fatalf("expected no message id without a stamp, got %q", publishing.MessageId)
    }
}

func TestAckNack_StaleGenerationIsNoOp(t *testing.T) {
    runtimeInstance := newReconnectRuntime(context.Background())

    newInstance := func() *Transport {
        return &Transport{
            queue:             "orders",
            consumeChannel:    &amqp091.Channel{},
            consumeGeneration: 2,
        }
    }

    staleEnvelope := melodymessagebus.NewEnvelope(reconnectMessage{Id: 1}, DeliveryStamp{Tag: 5, Generation: 1})

    if ackErr := newInstance().Ack(runtimeInstance, staleEnvelope); nil != ackErr {
        t.Fatalf("expected a stale-generation ack to be a no-op, got %v", ackErr)
    }

    if nackErr := newInstance().Nack(runtimeInstance, staleEnvelope, false); nil != nackErr {
        t.Fatalf("expected a stale-generation drop nack to be a no-op, got %v", nackErr)
    }

    if nackErr := newInstance().Nack(runtimeInstance, staleEnvelope, true); nil != nackErr {
        t.Fatalf("expected a stale-generation requeue nack to be a no-op, got %v", nackErr)
    }
}

func TestConsumeLoop_ContextDoneClosesOut(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())

    instance := &Transport{queue: "orders"}
    deliveries := make(chan amqp091.Delivery)
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(newReconnectRuntime(ctx), nil, deliveries, out)

    cancel()

    select {
    case _, open := <-out:
        if true == open {
            t.Fatalf("expected out to be closed on context cancellation")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected consumeLoop to close out after context cancellation")
    }
}

/* @info close unblocks parked goroutines */

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

func TestReopenConsume_CloseUnblocksGoroutineParkedOnBackoff(t *testing.T) {
    transport := NewTransport(TransportConfig{
        Dialer:   func() (*amqp091.Connection, error) { return nil, errors.New("broker down") },
        Queue:    "melody.amqp.reopen-backoff",
        Registry: NewMessageRegistry(),
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    backoff := 10 * time.Second
    done := make(chan error, 1)

    go func() {
        _, _, reopenErr := transport.reopenConsume(runtimeInstance, &backoff)
        done <- reopenErr
    }()

    time.Sleep(50 * time.Millisecond)

    transport.Close(runtimeInstance)

    select {
    case reopenErr := <-done:
        if nil == reopenErr {
            t.Fatalf("expected reopenConsume to return an error after Close")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("reopenConsume did not return after Close — the reconnect goroutine leaked while parked on backoff")
    }
}

/* @info publisher confirms */

func TestTransport_SendSurfacesUnroutablePublishAfterQueueDelete(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, "amqp.confirm.message")

    queueName := "melody.amqp.confirm-unroutable"

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      queueName,
        Registry:   registry,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
    defer transport.Close(runtimeInstance)

    firstErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "routable"}))
    if nil != firstErr {
        t.Fatalf("first send: %v", firstErr)
    }

    admin, adminErr := connection.Channel()
    if nil != adminErr {
        t.Fatalf("open admin channel: %v", adminErr)
    }
    defer admin.Close()

    if _, deleteErr := admin.QueueDelete(queueName, false, false, false); nil != deleteErr {
        t.Fatalf("delete queue: %v", deleteErr)
    }

    secondErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 2, Name: "unroutable"}))
    if nil == secondErr {
        t.Fatalf("expected Send to fail after the queue was deleted; the broker silently discarded the message")
    }
}

/* @info channel reopen */

func TestEnsurePublishChannel_ReopensClosedChannelWithoutDialer(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.reopen-publish",
        Registry:   NewMessageRegistry(),
    })

    first, _, firstErr := transport.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, _, secondErr := transport.ensurePublishChannel()
    if nil != secondErr {
        t.Fatalf("second ensurePublishChannel: %v", secondErr)
    }
    if true == second.IsClosed() {
        t.Fatalf("expected a fresh open channel, got a closed one (the stale channel was reused)")
    }
    if second == first {
        t.Fatalf("expected the stale closed channel to be replaced, got the same channel")
    }
}

func TestEnsureConsumeChannel_ReopensClosedChannelWithoutDialer(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      "melody.amqp.reopen-consume",
        Registry:   NewMessageRegistry(),
    })

    first, firstErr := transport.ensureConsumeChannel()
    if nil != firstErr {
        t.Fatalf("first ensureConsumeChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := transport.ensureConsumeChannel()
    if nil != secondErr {
        t.Fatalf("second ensureConsumeChannel: %v", secondErr)
    }
    if true == second.IsClosed() {
        t.Fatalf("expected a fresh open channel, got a closed one (the stale channel was reused)")
    }
    if second == first {
        t.Fatalf("expected the stale closed channel to be replaced, got the same channel")
    }
}

/* @info delay expiration */

func TestDelayExpirationMilliseconds_ClampsSubMillisecondToOne(t *testing.T) {
    if 1 != delayExpirationMilliseconds(200*time.Microsecond) {
        t.Fatalf("expected a sub-millisecond delay to clamp to 1ms, got %d (a \"0\" TTL expires immediately and drops the backoff)", delayExpirationMilliseconds(200*time.Microsecond))
    }

    if 1 != delayExpirationMilliseconds(999*time.Microsecond) {
        t.Fatalf("expected 999us to clamp to 1ms, got %d", delayExpirationMilliseconds(999*time.Microsecond))
    }

    if 5 != delayExpirationMilliseconds(5*time.Millisecond) {
        t.Fatalf("expected 5ms to stay 5, got %d", delayExpirationMilliseconds(5*time.Millisecond))
    }
}

/* a delay whose milliseconds exceed RabbitMQ's 32-bit expiration must clamp to the cap rather than be passed through to wrap to a tiny ttl that would expire the message almost immediately. */
func TestDelayExpirationMilliseconds_ClampsHugeDelayToCap(t *testing.T) {
    huge := time.Duration(math.MaxUint32+1000) * time.Millisecond
    if maxDelayExpirationMilliseconds != delayExpirationMilliseconds(huge) {
        t.Fatalf("expected a huge delay to clamp to %d, got %d", maxDelayExpirationMilliseconds, delayExpirationMilliseconds(huge))
    }

    atCap := time.Duration(maxDelayExpirationMilliseconds) * time.Millisecond
    if maxDelayExpirationMilliseconds != delayExpirationMilliseconds(atCap) {
        t.Fatalf("expected a delay at the cap to stay %d, got %d", maxDelayExpirationMilliseconds, delayExpirationMilliseconds(atCap))
    }
}

/* drainPublishReturn must remove every queued return, not just one, so a publish is reported unroutable even when more than one return has accumulated and so no stale return is left behind to be misattributed to the next publish. */
func TestDrainPublishReturn_DrainsEveryQueuedReturn(t *testing.T) {
    returns := make(chan amqp091.Return, 8)
    returns <- amqp091.Return{ReplyCode: 312, ReplyText: "first"}
    returns <- amqp091.Return{ReplyCode: 312, ReplyText: "second"}
    returns <- amqp091.Return{ReplyCode: 312, ReplyText: "third"}

    last, drained := drainPublishReturn(returns)
    if false == drained {
        t.Fatal("expected drained to report the accumulated returns")
    }

    if "third" != last.ReplyText {
        t.Fatalf("expected the last return reported, got %q", last.ReplyText)
    }

    if 0 != len(returns) {
        t.Fatalf("expected every queued return drained, %d left", len(returns))
    }

    if _, stillDrained := drainPublishReturn(returns); true == stillDrained {
        t.Fatal("expected an empty channel to report nothing drained")
    }
}

/* @info redelivery header */

func TestRedeliveryCountFromHeader(t *testing.T) {
    cases := []struct {
        name     string
        headers  amqp091.Table
        expected int
    }{
        {name: "missing", headers: amqp091.Table{}, expected: 0},
        {name: "int64", headers: amqp091.Table{headerRedeliveryCount: int64(3)}, expected: 3},
        {name: "int32", headers: amqp091.Table{headerRedeliveryCount: int32(2)}, expected: 2},
        {name: "int", headers: amqp091.Table{headerRedeliveryCount: 5}, expected: 5},
        {name: "float64", headers: amqp091.Table{headerRedeliveryCount: float64(4)}, expected: 4},
        {name: "float32", headers: amqp091.Table{headerRedeliveryCount: float32(6)}, expected: 6},
        {name: "uint", headers: amqp091.Table{headerRedeliveryCount: uint(8)}, expected: 8},
        {name: "uint32", headers: amqp091.Table{headerRedeliveryCount: uint32(9)}, expected: 9},
        {name: "wrong type", headers: amqp091.Table{headerRedeliveryCount: "7"}, expected: 0},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            got := redeliveryCountFromHeader(testCase.headers)
            if testCase.expected != got {
                t.Fatalf("expected %d, got %d", testCase.expected, got)
            }
        })
    }
}

/* @info message type name */

func TestMessageTypeName_NilDoesNotPanic(t *testing.T) {
    if "<nil>" != messageTypeName(nil) {
        t.Fatalf("expected a placeholder name for a nil message, got %q", messageTypeName(nil))
    }
}

func TestMessageTypeName_ReportsConcreteType(t *testing.T) {
    type sample struct{}

    if "amqp.sample" != messageTypeName(sample{}) {
        t.Fatalf("unexpected type name: %q", messageTypeName(sample{}))
    }
}
