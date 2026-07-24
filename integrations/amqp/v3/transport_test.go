package amqp

import (
    "context"
    "encoding/json"
    "errors"
    "math"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
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

type closeJoinMessage struct {
    Id int
}

/* blockingDeserializer parks the consume goroutine inside decode until the test releases it, holding open the window in which Close used to return while the loop was still running. */
type blockingDeserializer struct {
    inner   serializercontract.Serializer
    once    sync.Once
    entered chan struct{}
    release chan struct{}
}

func newBlockingDeserializer() *blockingDeserializer {
    return &blockingDeserializer{
        inner:   melodyserializer.NewJsonSerializer(),
        entered: make(chan struct{}),
        release: make(chan struct{}),
    }
}

func (instance *blockingDeserializer) Serialize(value any) ([]byte, error) {
    return instance.inner.Serialize(value)
}

func (instance *blockingDeserializer) Deserialize(payload []byte, target any) error {
    instance.once.Do(func() {
        close(instance.entered)
    })

    <-instance.release

    return instance.inner.Deserialize(payload, target)
}

func (instance *blockingDeserializer) ContentType() string {
    return instance.inner.ContentType()
}

var _ serializercontract.Serializer = (*blockingDeserializer)(nil)

func newReconnectRuntime(ctx context.Context) runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)
}

type logRecord struct {
    level   loggingcontract.Level
    message string
}

type recordingLogger struct {
    mutex   sync.Mutex
    records []logRecord
}

func (instance *recordingLogger) Log(level loggingcontract.Level, message string, logContext loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, logRecord{level: level, message: message})
}

func (instance *recordingLogger) Debug(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, logContext)
}

func (instance *recordingLogger) Info(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, logContext)
}

func (instance *recordingLogger) Warning(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, logContext)
}

func (instance *recordingLogger) Error(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, logContext)
}

func (instance *recordingLogger) Emergency(message string, logContext loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, logContext)
}

func (instance *recordingLogger) errorMessages() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    messages := make([]string, 0, len(instance.records))
    for _, record := range instance.records {
        if loggingcontract.LevelError != record.level {
            continue
        }

        messages = append(messages, record.message)
    }

    return messages
}

var _ loggingcontract.Logger = (*recordingLogger)(nil)

/* newRecordingLoggerRuntime carries a logger the transport can actually reach: newReconnectRuntime builds an empty container, so LoggerFromRuntime hands back nil there and no log is produced or observed either way. */
func newRecordingLoggerRuntime(ctx context.Context) (runtimecontract.Runtime, *recordingLogger) {
    logger := &recordingLogger{}

    serviceContainer := container.NewContainer()
    container.MustRegister[loggingcontract.Logger](
        serviceContainer,
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    return runtime.New(ctx, serviceContainer.NewScope(), serviceContainer), logger
}

func awaitClosedOutput(t *testing.T, out <-chan messagebuscontract.Envelope) {
    t.Helper()

    select {
    case _, open := <-out:
        if true == open {
            t.Fatalf("expected out to be closed without delivering a message")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected consumeLoop to close out")
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

/* @important a consumer that can never re-subscribe must say why it is stopping: the loop returns and closes out either way, so without the log a consumer dies silently in production and the queue simply stops being drained. */
func TestConsumeLoop_NoDialerLogsWhyTheConsumerStops(t *testing.T) {
    runtimeInstance, logger := newRecordingLoggerRuntime(context.Background())

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders"}
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(runtimeInstance, nil, deliveries, out)

    awaitClosedOutput(t, out)

    errorMessages := logger.errorMessages()
    if 1 != len(errorMessages) {
        t.Fatalf("expected exactly one error record naming the stop, got %v", errorMessages)
    }

    if false == strings.Contains(errorMessages[0], "consumer is stopping") {
        t.Fatalf("expected the record to name the stop, got %q", errorMessages[0])
    }
}

/* @important the mirror of the record above: a stop the transport itself asked for is expected, so it must stay silent — otherwise every deploy floods the error dashboards from each consumer that shuts down. */
func TestConsumeLoop_ClosingTransportStopsWithoutLogging(t *testing.T) {
    runtimeInstance, logger := newRecordingLoggerRuntime(context.Background())

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders", closing: true, closeSignal: make(chan struct{})}
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(runtimeInstance, nil, deliveries, out)

    awaitClosedOutput(t, out)

    if errorMessages := logger.errorMessages(); 0 != len(errorMessages) {
        t.Fatalf("expected a closing transport to stop silently, got %v", errorMessages)
    }
}

func TestConsumeLoop_CancelledContextStopsWithoutLogging(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    runtimeInstance, logger := newRecordingLoggerRuntime(ctx)

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    instance := &Transport{queue: "orders", closeSignal: make(chan struct{})}
    out := make(chan messagebuscontract.Envelope)

    go instance.consumeLoop(runtimeInstance, nil, deliveries, out)

    awaitClosedOutput(t, out)

    if errorMessages := logger.errorMessages(); 0 != len(errorMessages) {
        t.Fatalf("expected a cancelled context to stop the consumer silently, got %v", errorMessages)
    }
}

func TestConnectionAlive_ReportsLiveAndGone(t *testing.T) {
    gone := &Transport{queue: "orders"}
    if true == gone.connectionAlive() {
        t.Fatalf("expected connectionAlive to report false without a connection")
    }

    live := &Transport{queue: "orders", connection: &amqp091.Connection{}}
    if false == live.connectionAlive() {
        t.Fatalf("expected connectionAlive to report true for a non-nil open connection")
    }
}

/* @info a failed publish on a live static connection (no dialer) must be retried on a fresh channel — the live connection can still carry it — while a no-dialer transport whose connection is gone, or a closing transport, must not retry. */
func TestPublishRetryable_LiveStaticConnectionRetriesWithoutDialer(t *testing.T) {
    liveStatic := &Transport{queue: "orders", connection: &amqp091.Connection{}}
    if false == liveStatic.publishRetryable() {
        t.Fatalf("expected a publish on a live static connection to be retryable without a dialer")
    }

    gone := &Transport{queue: "orders"}
    if true == gone.publishRetryable() {
        t.Fatalf("expected no retry when there is no dialer and no live connection")
    }

    dialerBacked := &Transport{queue: "orders", dialer: func() (*amqp091.Connection, error) { return nil, nil }}
    if false == dialerBacked.publishRetryable() {
        t.Fatalf("expected a dialer-backed transport to stay retryable")
    }

    closing := &Transport{queue: "orders", connection: &amqp091.Connection{}, closing: true}
    if true == closing.publishRetryable() {
        t.Fatalf("expected a closing transport never to retry a publish")
    }
}

/* @info a transient subscribe failure on a live static connection (no dialer) must be retried on a fresh channel — the live connection can still carry the subscription — while a no-dialer transport whose connection is gone, or a closing transport, must give up. */
func TestSubscribeRetryable_LiveStaticConnectionRetriesWithoutDialer(t *testing.T) {
    liveStatic := &Transport{queue: "orders", connection: &amqp091.Connection{}}
    if false == liveStatic.subscribeRetryable() {
        t.Fatalf("expected a subscribe on a live static connection to be retryable without a dialer")
    }

    gone := &Transport{queue: "orders"}
    if true == gone.subscribeRetryable() {
        t.Fatalf("expected no retry when there is no dialer and no live connection")
    }

    dialerBacked := &Transport{queue: "orders", dialer: func() (*amqp091.Connection, error) { return nil, nil }}
    if false == dialerBacked.subscribeRetryable() {
        t.Fatalf("expected a dialer-backed transport to stay retryable")
    }

    closing := &Transport{queue: "orders", connection: &amqp091.Connection{}, closing: true}
    if true == closing.subscribeRetryable() {
        t.Fatalf("expected a closing transport never to retry a subscribe")
    }
}

/* @info a consumer built on a live static connection with no dialer must recover from a channel-only loss (queue deleted, broker basic.cancel, a PRECONDITION_FAILED that closes only the channel): the live connection can still open a fresh channel, so the loop must re-subscribe instead of closing out and stopping. */
func TestConsumeLoop_StaticLiveConnectionRecoversFromChannelOnlyLoss(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &Transport{
        queue:       "orders",
        connection:  &amqp091.Connection{},
        closeSignal: make(chan struct{}),
        reconnect:   ReconnectConfig{InitialBackoff: 5 * time.Second, MaxBackoff: 5 * time.Second, BackoffFactor: 2},
    }

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    out := make(chan messagebuscontract.Envelope)
    done := make(chan struct{})

    go func() {
        instance.consumeLoop(newReconnectRuntime(ctx), nil, deliveries, out)
        close(done)
    }()

    /* on the old code a nil dialer made consumeLoop return immediately and close out; on a live static connection it must instead park on the reconnect backoff, keeping the subscription open for recovery. */
    select {
    case _, open := <-out:
        if false == open {
            t.Fatalf("consumer closed out and stopped on a channel-only loss even though the static connection is still alive")
        }

        t.Fatalf("did not expect a delivery on a bare consume loop")
    case <-done:
        t.Fatalf("consumeLoop returned instead of recovering on the live static connection")
    case <-time.After(200 * time.Millisecond):
    }

    /* clean shutdown still works: cancelling the runtime unblocks the backoff wait and closes out */
    cancel()

    select {
    case <-done:
    case <-time.After(2 * time.Second):
        t.Fatalf("consumeLoop did not return after context cancellation")
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

/* @important Close must join the consume goroutine, not merely signal it. While it returned early the loop was still inside decode and went on to hand an envelope to the application AFTER Close returned — an envelope that can never be acked, because consumeChannelForAck reports the torn-down channel as gone, so the broker redelivers it; a decode failure racing the same window nacks on the channel Close has already closed. */
func TestClose_WaitsForTheConsumeGoroutine(t *testing.T) {
    registry := NewMessageRegistry()
    RegisterMessage[closeJoinMessage](registry, "amqp.test.close-join")

    serializer := newBlockingDeserializer()

    instance := &Transport{
        queue:       "orders",
        registry:    registry,
        serializer:  serializer,
        closeSignal: make(chan struct{}),
        reconnect:   resolveReconnectConfig(nil, nil),
    }

    body, marshalErr := json.Marshal(closeJoinMessage{Id: 1})
    if nil != marshalErr {
        t.Fatalf("marshal: %v", marshalErr)
    }

    deliveries := make(chan amqp091.Delivery, 1)
    deliveries <- amqp091.Delivery{
        Headers:     amqp091.Table{headerMessageType: "amqp.test.close-join"},
        Body:        body,
        DeliveryTag: 1,
    }

    runtimeInstance := newReconnectRuntime(context.Background())
    out := make(chan messagebuscontract.Envelope)

    instance.startConsumeLoop(runtimeInstance, nil, deliveries, out)

    select {
    case <-serializer.entered:
    case <-time.After(2 * time.Second):
        t.Fatalf("the consume goroutine never reached the deserializer")
    }

    closed := make(chan struct{})
    go func() {
        instance.Close(runtimeInstance)

        close(closed)
    }()

    /* shorter than closeJoinTimeout on purpose: past the bound Close is entitled to give up, and the assertion would stop proving anything */
    select {
    case <-closed:
        t.Fatalf("Close returned while the consume goroutine was still inside the deserializer")
    case <-time.After(100 * time.Millisecond):
    }

    close(serializer.release)

    select {
    case <-closed:
    case <-time.After(2 * time.Second):
        t.Fatalf("Close did not return after the consume goroutine finished")
    }

    select {
    case envelopeInstance, open := <-out:
        if true == open {
            t.Fatalf("an envelope reached the application after Close returned: %+v", envelopeInstance.Message())
        }
    default:
        t.Fatalf("expected the consume goroutine to have closed out before Close returned")
    }
}

/* @important the join is bounded: closeSignal is out of the loop's sight only while it is parked inside the caller-supplied dialer, and Close must not inherit that dialer's timeout — teardown that never blocked before has to keep not blocking. */
/* @info the one stretch the loop cannot observe closeSignal is inside the caller-supplied dialer, and Close waits through it rather than tearing the channels down under a running loop */
func TestClose_WaitsForALoopStuckInTheDialer(t *testing.T) {
    dialing := make(chan struct{})
    release := make(chan struct{})

    var once sync.Once

    instance := &Transport{
        queue:       "orders",
        closeSignal: make(chan struct{}),
        reconnect:   ReconnectConfig{InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffFactor: 2},
        dialer: func() (*amqp091.Connection, error) {
            once.Do(func() {
                close(dialing)
            })

            <-release

            return nil, exception.NewError("dial refused", nil, nil)
        },
    }

    deliveries := make(chan amqp091.Delivery)
    close(deliveries)

    runtimeInstance := newReconnectRuntime(context.Background())
    out := make(chan messagebuscontract.Envelope)

    instance.startConsumeLoop(runtimeInstance, nil, deliveries, out)

    select {
    case <-dialing:
    case <-time.After(2 * time.Second):
        t.Fatalf("the consume goroutine never reached the dialer")
    }

    dialDuration := 200 * time.Millisecond

    go func() {
        time.Sleep(dialDuration)

        close(release)
    }()

    start := time.Now()
    instance.Close(runtimeInstance)
    elapsed := time.Since(start)

    if elapsed < dialDuration {
        t.Fatalf("Close returned after %s without waiting for the dial to finish", elapsed)
    }

    if closeJoinTimeout < elapsed {
        t.Fatalf("Close blocked for %s, past the %s bound", elapsed, closeJoinTimeout)
    }
}

/* @important the same join through the public path: Receive is what registers the goroutine, so a consumer started there must be joined too. */
func TestTransport_CloseJoinsTheConsumerStartedByReceive(t *testing.T) {
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
    RegisterMessage[closeJoinMessage](registry, "amqp.test.close-join.live")

    queueName := "melody.amqp.close-join"
    serializer := newBlockingDeserializer()

    transport := NewTransport(TransportConfig{
        Connection: connection,
        Queue:      queueName,
        Prefetch:   1,
        Registry:   registry,
        Serializer: serializer,
    })

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(closeJoinMessage{Id: 1})); nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    out, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }

    select {
    case <-serializer.entered:
    case <-time.After(10 * time.Second):
        t.Fatalf("the consumer never reached the deserializer")
    }

    closed := make(chan struct{})
    go func() {
        transport.Close(runtimeInstance)

        close(closed)
    }()

    select {
    case <-closed:
        t.Fatalf("Close returned while the consumer was still inside the deserializer")
    case <-time.After(100 * time.Millisecond):
    }

    close(serializer.release)

    select {
    case <-closed:
    case <-time.After(5 * time.Second):
        t.Fatalf("Close did not return after the consumer finished")
    }

    select {
    case envelopeInstance, open := <-out:
        if true == open {
            t.Fatalf("an envelope reached the application after Close returned: %+v", envelopeInstance.Message())
        }
    default:
        t.Fatalf("expected the consumer to have closed its output before Close returned")
    }

    /* the blocked message was never acked, so the broker returns it to the durable queue when the consume channel closes — drain it rather than leaving one behind per run */
    admin, adminErr := connection.Channel()
    if nil != adminErr {
        t.Fatalf("open admin channel: %v", adminErr)
    }
    defer admin.Close()

    if _, purgeErr := admin.QueuePurge(queueName, false); nil != purgeErr {
        t.Fatalf("purge queue: %v", purgeErr)
    }
}

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

/* the AMQP 0-9-1 prefetch-count field is encoded as uint16 by channel.Qos, so a configured prefetch above 65535 wraps on the wire — 65536 becomes 0, which RabbitMQ treats as UNLIMITED prefetch. newTransport must clamp the value to 65535 before it can reach Qos, otherwise the flow-control cap silently inverts into no cap at all. */
func TestNewTransport_ClampsPrefetchToTheWireMaximum(t *testing.T) {
    newInstance := func(prefetch int) *Transport {
        return newTransport(TransportConfig{
            Dialer:   func() (*amqp091.Connection, error) { return nil, nil },
            Queue:    "orders",
            Prefetch: prefetch,
            Registry: NewMessageRegistry(),
        }, nil)
    }

    if 65535 != newInstance(65536).prefetch {
        t.Fatalf("expected a prefetch of 65536 to clamp to 65535 (uint16 wrap to 0 = unlimited), got %d", newInstance(65536).prefetch)
    }

    if 65535 != newInstance(70000).prefetch {
        t.Fatalf("expected a prefetch of 70000 to clamp to 65535, got %d", newInstance(70000).prefetch)
    }

    if 65535 != newInstance(65535).prefetch {
        t.Fatalf("expected a prefetch at the wire maximum to stay 65535, got %d", newInstance(65535).prefetch)
    }

    if 10 != newInstance(10).prefetch {
        t.Fatalf("expected an in-range prefetch to be preserved, got %d", newInstance(10).prefetch)
    }

    if 1 != newInstance(0).prefetch {
        t.Fatalf("expected a non-positive prefetch to stay clamped up to 1, got %d", newInstance(0).prefetch)
    }
}

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

/* @info delay buckets */

func TestResolveDelayBuckets_DefaultsWhenEmpty(t *testing.T) {
    buckets := resolveDelayBuckets(nil)

    if 4 != len(buckets) || 5*time.Second != buckets[0] || 1*time.Hour != buckets[3] {
        t.Fatalf("unexpected default buckets: %v", buckets)
    }
}

func TestResolveDelayBuckets_RejectsNonAscending(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected non-ascending buckets to be rejected")
        }
    }()

    _ = resolveDelayBuckets([]time.Duration{time.Minute, time.Second})
}

func TestResolveDelayBuckets_RejectsNonPositive(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a non-positive bucket to be rejected")
        }
    }()

    _ = resolveDelayBuckets([]time.Duration{0, time.Second})
}

func TestResolveDelayBuckets_RejectsTooMany(t *testing.T) {
    tooMany := make([]time.Duration, maxDelayBuckets+1)
    for index := range tooMany {
        tooMany[index] = time.Duration(index+1) * time.Second
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("expected more than %d buckets to be rejected", maxDelayBuckets)
        }
    }()

    _ = resolveDelayBuckets(tooMany)
}

func TestDelayBucketFor_SelectsLargestNotExceeding(t *testing.T) {
    buckets := []time.Duration{5 * time.Second, time.Minute, 10 * time.Minute, time.Hour}

    cases := []struct {
        name  string
        delay time.Duration
        want  time.Duration
        found bool
    }{
        {name: "below smallest keeps legacy queue", delay: time.Second, want: 0, found: false},
        {name: "exact smallest", delay: 5 * time.Second, want: 5 * time.Second, found: true},
        {name: "between buckets quantizes down", delay: 90 * time.Second, want: time.Minute, found: true},
        {name: "exact middle", delay: 10 * time.Minute, want: 10 * time.Minute, found: true},
        {name: "above largest clamps to largest", delay: 3 * time.Hour, want: time.Hour, found: true},
        {name: "just below smallest", delay: 5*time.Second - time.Millisecond, want: 0, found: false},
    }

    for _, current := range cases {
        got, found := delayBucketFor(buckets, current.delay)
        if current.found != found || current.want != got {
            t.Fatalf("%s: delayBucketFor(%v) = (%v, %v), want (%v, %v)", current.name, current.delay, got, found, current.want, current.found)
        }
    }
}

func TestDelayBucketQueueName_IsDeterministic(t *testing.T) {
    if "orders.delay.60000ms" != delayBucketQueueName("orders", time.Minute) {
        t.Fatalf("unexpected bucket queue name: %s", delayBucketQueueName("orders", time.Minute))
    }
}

func TestTransport_BucketedDelaysAvoidHeadOfLineBlocking(t *testing.T) {
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
    RegisterMessage[testMessage](registry, "amqp.test.delay.bucket")

    transport := NewTransport(TransportConfig{
        Connection:   connection,
        Queue:        "melody.amqp.delay.bucket",
        Prefetch:     2,
        Registry:     registry,
        DelayBuckets: []time.Duration{2 * time.Second, 8 * time.Second},
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

    /* @important the queues are durable, so a message parked in a bucket queue by a crashed or older run dead-letters back into the main queue and would corrupt the identity assertions below — drain any leftovers before producing this run's messages */
    for draining := true; true == draining; {
        select {
        case leftover := <-queue:
            if ackErr := transport.Ack(runtimeInstance, leftover); nil != ackErr {
                t.Fatalf("ack leftover: %v", ackErr)
            }
        case <-time.After(1500 * time.Millisecond):
            draining = false
        }
    }

    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 1, Name: "long"})); nil != sendErr {
        t.Fatalf("send long: %v", sendErr)
    }
    if sendErr := transport.Send(runtimeInstance, melodymessagebus.NewEnvelope(testMessage{Id: 2, Name: "short"})); nil != sendErr {
        t.Fatalf("send short: %v", sendErr)
    }

    first := receiveWithin(t, queue, 10*time.Second)
    second := receiveWithin(t, queue, 10*time.Second)

    longDelayed := first.
        WithStamp(melodymessagebus.RedeliveryStamp{Count: 1}).
        WithStamp(melodymessagebus.DelayStamp{Delay: 8 * time.Second})
    shortDelayed := second.
        WithStamp(melodymessagebus.RedeliveryStamp{Count: 1}).
        WithStamp(melodymessagebus.DelayStamp{Delay: 2 * time.Second})

    /* @important requeue the LONG delay first: on the single-delay-queue topology it parks at the head with the longer per-message ttl and RabbitMQ's head-of-queue-only expiry stalls the 2s message behind it for the full 8s — the bucketed topology parks them in separate uniform-ttl queues, so the short one must come back well before the long one */
    start := time.Now()
    if nackErr := transport.Nack(runtimeInstance, longDelayed, true); nil != nackErr {
        t.Fatalf("nack long: %v", nackErr)
    }
    if nackErr := transport.Nack(runtimeInstance, shortDelayed, true); nil != nackErr {
        t.Fatalf("nack short: %v", nackErr)
    }

    redelivered := receiveWithin(t, queue, 6*time.Second)

    elapsed := time.Since(start)
    if 6*time.Second <= elapsed {
        t.Fatalf("short delay stalled behind the long one for %s", elapsed)
    }

    /* @important assert the IDENTITY of the redelivered message, not just its timing: a bucket-misrouting regression (short delay parked in the long bucket and vice versa) would otherwise still deliver A message within the window */
    shortMessage, isShort := redelivered.Message().(testMessage)
    if false == isShort || "short" != shortMessage.Name {
        t.Fatalf("expected the short-delayed message first, got %+v", redelivered.Message())
    }

    if ackErr := transport.Ack(runtimeInstance, redelivered); nil != ackErr {
        t.Fatalf("ack: %v", ackErr)
    }

    /* @important drain the long-delayed message too, so the durable bucket queue is left empty for the next run instead of leaking one parked message per run */
    longRedelivered := receiveWithin(t, queue, 12*time.Second)

    longMessage, isLong := longRedelivered.Message().(testMessage)
    if false == isLong || "long" != longMessage.Name {
        t.Fatalf("expected the long-delayed message second, got %+v", longRedelivered.Message())
    }

    if ackErr := transport.Ack(runtimeInstance, longRedelivered); nil != ackErr {
        t.Fatalf("ack long: %v", ackErr)
    }
}

func TestStartConsumeLoop_RefusesOnceCloseHasBegun(t *testing.T) {
    instance := &Transport{
        queue:       "orders",
        registry:    NewMessageRegistry(),
        serializer:  melodyserializer.NewJsonSerializer(),
        closeSignal: make(chan struct{}),
        reconnect:   resolveReconnectConfig(nil, nil),
    }

    runtimeInstance := newReconnectRuntime(context.Background())

    if closeErr := instance.Close(runtimeInstance); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    deliveries := make(chan amqp091.Delivery)
    out := make(chan messagebuscontract.Envelope)

    if true == instance.startConsumeLoop(runtimeInstance, nil, deliveries, out) {
        t.Fatalf("expected a closing transport to refuse a new consume loop")
    }

    joined := make(chan struct{})
    go func() {
        instance.wait.Wait()

        close(joined)
    }()

    select {
    case <-joined:
    case <-time.After(2 * time.Second):
        t.Fatalf("a refused start must leave the wait group at zero, so nothing outlives the join")
    }
}
