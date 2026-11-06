package amqp

import (
    "context"
    "errors"
    "fmt"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func TestServerSentEventBackplane_PublishAfterCloseDoesNotRetry(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            return nil, errors.New("no broker")
        },
        Hub: hub,
    })

    if closeErr := backplane.Close(); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    done := make(chan error, 1)
    go func() {
        done <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "after-close"})
    }()

    select {
    case publishErr := <-done:
        if nil == publishErr {
            t.Fatalf("expected publish on a closed backplane to fail")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("publish on a closed backplane hung instead of short-circuiting the retry")
    }
}

func TestServerSentEventBackplane_CloseDoesNotDeadlockDuringReconnect(t *testing.T) {
    dialStarted := make(chan struct{}, 1)
    dialUnblock := make(chan struct{})

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            select {
            case dialStarted <- struct{}{}:
            default:
            }
            <-dialUnblock
            return nil, errors.New("dial cancelled")
        },
        Hub: hub,
    })

    select {
    case <-dialStarted:
    case <-time.After(2 * time.Second):
        t.Fatalf("dial never started")
    }

    done := make(chan error, 1)
    go func() { done <- backplane.Close() }()

    time.Sleep(50 * time.Millisecond)
    close(dialUnblock)

    select {
    case closeErr := <-done:
        if nil != closeErr {
            t.Fatalf("close: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("Close() deadlocked — mutex was held during dial and blocked Close()")
    }
}

func TestServerSentEventBackplane_CloseReturnsWhileDialStillBlocked(t *testing.T) {
    dialStarted := make(chan struct{}, 1)
    dialUnblock := make(chan struct{})

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer: func() (*amqp091.Connection, error) {
            select {
            case dialStarted <- struct{}{}:
            default:
            }
            <-dialUnblock
            return nil, errors.New("dial released")
        },
        Hub: hub,
    })

    select {
    case <-dialStarted:
    case <-time.After(2 * time.Second):
        close(dialUnblock)
        t.Fatalf("dial never started")
    }

    done := make(chan error, 1)
    go func() { done <- backplane.Close() }()

    select {
    case closeErr := <-done:
        if nil != closeErr {
            close(dialUnblock)
            t.Fatalf("close: %v", closeErr)
        }
    case <-time.After(2 * time.Second):
        close(dialUnblock)
        t.Fatalf("Close() blocked on the in-flight dial instead of returning once the context was cancelled")
    }

    close(dialUnblock)
}

func TestServerSentEventBackplane_ReplicatesBroadcastToAnotherInstance(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp sse backplane integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    exchange := "melody.sse.test"

    hubA := melodyhttp.NewServerSentEventHub()
    backplaneA := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hubA, Exchange: exchange})
    defer backplaneA.Close()

    hubB := melodyhttp.NewServerSentEventHub()
    backplaneB := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hubB, Exchange: exchange})
    defer backplaneB.Close()

    subscriber := hubB.Subscribe("orders", 4)
    defer hubB.Unsubscribe(subscriber)

    deadline := time.After(10 * time.Second)
    tick := time.NewTicker(150 * time.Millisecond)
    defer tick.Stop()

    for {
        hubA.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "from-a"})

        select {
        case event := <-subscriber.Events():
            if "from-a" != event.Data {
                t.Fatalf("unexpected replicated event: %q", event.Data)
            }

            return
        case <-tick.C:
        case <-deadline:
            t.Fatalf("expected the broadcast to be replicated to the other instance")
        }
    }
}

func TestServerSentEventBackplane_DoesNotEchoToOriginInstanceTwice(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp sse backplane integration test")
    }

    provider := NewProvider()
    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        t.Fatalf("open connection: %v", openErr)
    }
    defer provider.Close(connection)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{Connection: connection, Hub: hub, Exchange: "melody.sse.test.echo"})
    defer backplane.Close()

    subscriber := hub.Subscribe("orders", 4)
    defer hub.Unsubscribe(subscriber)

    if delivered := hub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "once"}); 1 != delivered {
        t.Fatalf("expected exactly one local delivery, got %d", delivered)
    }

    select {
    case event := <-subscriber.Events():
        if "once" != event.Data {
            t.Fatalf("unexpected event: %q", event.Data)
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the local delivery")
    }

    select {
    case event := <-subscriber.Events():
        t.Fatalf("expected no echoed re-delivery of the origin's own broadcast, got %q", event.Data)
    case <-time.After(time.Second):
    }
}

func TestShouldResetReconnectBackoff(t *testing.T) {
    config := resolveReconnectConfig(nil, nil)
    initialBackoff := config.InitialBackoff

    if true == reconnectBackoffShouldReset(config, initialBackoff-time.Nanosecond) {
        t.Fatalf("expected no backoff reset for a subscription that died sooner than the initial backoff")
    }

    if false == reconnectBackoffShouldReset(config, initialBackoff) {
        t.Fatalf("expected a backoff reset for a subscription that lived at least the initial backoff")
    }

    if false == reconnectBackoffShouldReset(config, 2*initialBackoff) {
        t.Fatalf("expected a backoff reset for a long-lived subscription")
    }
}

func TestResolveReconnectConfig_RejectsSubUnitBackoffFactor(t *testing.T) {
    defaultFactor := DefaultReconnectConfig().BackoffFactor

    if resolved := resolveReconnectConfig(nil, &ReconnectConfig{BackoffFactor: 0.5}); defaultFactor != resolved.BackoffFactor {
        t.Fatalf("expected a sub-unit override backoff factor to fall back to the default %v, got %v", defaultFactor, resolved.BackoffFactor)
    }

    if resolved := resolveReconnectConfig(&ReconnectConfig{BackoffFactor: 0.5}, nil); defaultFactor != resolved.BackoffFactor {
        t.Fatalf("expected a sub-unit general backoff factor to fall back to the default %v, got %v", defaultFactor, resolved.BackoffFactor)
    }

    if resolved := resolveReconnectConfig(nil, &ReconnectConfig{BackoffFactor: 3}); 3 != resolved.BackoffFactor {
        t.Fatalf("expected a valid override backoff factor to be honoured, got %v", resolved.BackoffFactor)
    }
}

func TestServerSentEventBackplane_ListenStopsWhenConnectionGoneAndNoDialer(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &ServerSentEventBackplane{
        reconnect: resolveReconnectConfig(nil, nil),
        ctx:       ctx,
        cancel:    cancel,
    }

    instance.wait.Add(1)

    done := make(chan struct{})
    go func() {
        instance.listen()
        close(done)
    }()

    select {
    case <-done:
    case <-time.After(2 * time.Second):
        cancel()
        t.Fatalf("listen kept backing off instead of stopping when the connection is gone and no dialer is configured")
    }
}

func TestServerSentEventBackplane_EnsurePublishChannel_ReopensClosedChannel(t *testing.T) {
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

    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection: connection,
        Hub:        melodyhttp.NewServerSentEventHub(),
        Exchange:   "melody.sse.reopen-publish",
    })

    first, _, firstErr := backplane.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, _, secondErr := backplane.ensurePublishChannel()
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

func TestBackplane_TerminalStopIsReportedThroughTheConfiguredLogger(t *testing.T) {
    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    connection, dialErr := amqp091.Dial(dsn)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }

    logger := &recordingBackplaneLogger{}
    hub := melodyhttp.NewServerSentEventHub()

    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection: connection,
        Hub:        hub,
        Logger:     logger,
    })
    defer backplane.Close()

    _ = connection.Close()

    deadline := time.Now().Add(5 * time.Second)
    for false == logger.sawTerminalStop() {
        if true == time.Now().After(deadline) {
            t.Fatal("expected the terminal listen stop to be reported: the receive half died and the operator must see it")
        }
        time.Sleep(10 * time.Millisecond)
    }
}

type recordingBackplaneLogger struct {
    mutex    sync.Mutex
    messages []string
}

func (instance *recordingBackplaneLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
}
func (instance *recordingBackplaneLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *recordingBackplaneLogger) Info(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *recordingBackplaneLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *recordingBackplaneLogger) Error(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *recordingBackplaneLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *recordingBackplaneLogger) sawTerminalStop() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, message := range instance.messages {
        if true == strings.Contains(message, "permanently stops receiving") {
            return true
        }
    }

    return false
}

func awaitBackplaneSubscribed(t *testing.T, backplane *ServerSentEventBackplane) {
    t.Helper()

    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        backplane.mutex.Lock()
        subscribed := nil != backplane.consumeChannel
        backplane.mutex.Unlock()

        if true == subscribed {
            return
        }

        time.Sleep(10 * time.Millisecond)
    }

    t.Fatalf("the backplane never finished subscribing")
}

func TestServerSentEventBackplane_PublishReturnsWithinTheCallTimeoutOnAWedgedWrite(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    gated.Wedge()

    outcome := make(chan error, 1)
    go func() { outcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    publishErr := awaitOutcome(t, "publish on a wedged write", outcome, 2*time.Second)
    if nil == publishErr {
        t.Fatalf("expected the wedged publish to fail")
    }

    if false == errors.Is(publishErr, errServerSentEventBackplanePublishTimedOut) {
        t.Fatalf("expected the call-timeout sentinel, got: %v", publishErr)
    }

    if 1 != gated.BlockedWrites() {
        t.Fatalf("expected exactly one write to have been blocked, got %d", gated.BlockedWrites())
    }
}

func TestServerSentEventBackplane_ATimedOutPublishOnAnOwnedConnectionRedialsOnTheNextPublish(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    defer backplane.Close()
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    if 1 != dialer.Dials() {
        t.Fatalf("expected one dial before the wedge, got %d", dialer.Dials())
    }

    dialer.Latest().Wedge()

    outcome := make(chan error, 1)
    go func() { outcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    if publishErr := awaitOutcome(t, "publish on a wedged write", outcome, 2*time.Second); nil == publishErr {
        t.Fatalf("expected the wedged publish to fail")
    }

    go func() { outcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "after-the-wedge"}) }()

    if publishErr := awaitOutcome(t, "publish after the wedge", outcome, 5*time.Second); nil != publishErr {
        t.Fatalf("expected the publish after the wedge to succeed on a fresh connection, got: %v", publishErr)
    }

    if 2 != dialer.Dials() {
        t.Fatalf("expected the wedged connection to have been cut and redialed once, got %d dials", dialer.Dials())
    }
}

func TestServerSentEventBackplane_ACutOwnedConnectionIsLetGoBeforeTheAbandonReturns(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    redialed := dialFakeBroker(t)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    dials := 0
    instance := &ServerSentEventBackplane{
        exchange:       "melody.sse.test.wedge",
        callTimeout:    time.Second,
        ctx:            ctx,
        cancel:         cancel,
        ownsConnection: true,
        connection:     wedge.connection,
        publishChannel: wedge.channel,
        consumeChannel: wedge.channel,
        dialer: func() (*amqp091.Connection, error) {
            dials++

            return redialed.connection, nil
        },
    }
    instance.joinBound = 2 * time.Second

    if abandonErr := instance.abandonWedgedPublish(wedge.connection, wedge.written); false == errors.Is(abandonErr, errServerSentEventBackplanePublishTimedOut) {
        t.Fatalf("expected the publish-timeout sentinel, got %v", abandonErr)
    }

    instance.mutex.Lock()
    connection := instance.connection
    publishChannel := instance.publishChannel
    consumeChannel := instance.consumeChannel
    instance.mutex.Unlock()

    if nil != connection || nil != publishChannel || nil != consumeChannel {
        t.Fatalf("expected the cut connection and its channels let go of, got connection %v publish channel %v consume channel %v", connection, publishChannel, consumeChannel)
    }

    connected, connectErr := instance.liveConnection()
    if nil != connectErr {
        t.Fatalf("expected the next publish to redial, got %v", connectErr)
    }

    if redialed.connection != connected || 1 != dials {
        t.Fatalf("expected the next publish to be handed the redialed connection after one dial, got the cut one %v after %d dials", wedge.connection == connected, dials)
    }
}

func TestServerSentEventBackplane_ASecondPublishOnAWedgedCallerOwnedConnectionIsRefusedAtOnce(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    gated.Wedge()

    outcome := make(chan error, 1)
    go func() { outcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    firstErr := awaitOutcome(t, "publish on a wedged write", outcome, 2*time.Second)
    if false == errorChainContains(firstErr, "did not return within the call timeout on a caller-owned connection") {
        t.Fatalf("expected the first publish to report the call timeout on a caller-owned connection, got: %v", firstErr)
    }

    go func() { outcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "while-wedged"}) }()

    secondErr := awaitOutcome(t, "publish while wedged", outcome, 2*time.Second)
    if false == errorChainContains(secondErr, "an earlier write is still blocked on this connection") {
        t.Fatalf("expected the second publish to be refused for the earlier blocked write, got: %v", secondErr)
    }

    if 1 != gated.BlockedWrites() {
        t.Fatalf("expected the refusal to reach the socket zero times, got %d blocked writes", gated.BlockedWrites())
    }
}

func TestServerSentEventBackplane_CloseReturnsWhileAPublishWriteIsWedged(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    dialer.Latest().Wedge()

    publishOutcome := make(chan error, 1)
    go func() { publishOutcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    awaitOutcome(t, "close while a publish write is wedged", closeOutcome, 3*time.Second)
    awaitOutcome(t, "the wedged publish after close", publishOutcome, 3*time.Second)
}

func TestServerSentEventBackplane_HubShutdownReturnsWhileABroadcastIsWedged(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    hub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "healthy"})

    gated.Wedge()

    broadcastOutcome := make(chan error, 1)
    go func() {
        hub.Broadcast("orders", melodyhttp.ServerSentEvent{Data: "wedged"})
        broadcastOutcome <- nil
    }()

    awaitBlockedWrites(t, gated, 1)

    shutdownOutcome := make(chan error, 1)
    go func() {
        hub.Shutdown()
        shutdownOutcome <- nil
    }()

    awaitOutcome(t, "hub shutdown while a broadcast is wedged", shutdownOutcome, 3*time.Second)
    awaitOutcome(t, "the wedged broadcast", broadcastOutcome, 3*time.Second)

    if 0 == hub.BackplaneFailures() {
        t.Fatalf("expected the wedged broadcast to be counted as a backplane failure")
    }
}

func TestServerSentEventBackplane_CloseReturnsWhenTheSocketWedgedWhileIdle(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    dialer.Latest().Wedge()

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    awaitOutcome(t, "close on a socket that wedged while idle", closeOutcome, 3*time.Second)
}

func TestServerSentEventBackplane_CloseNamesTheBlockedWriteOnAWedgedCallerOwnedConnection(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    gated.Wedge()

    publishOutcome := make(chan error, 1)
    go func() { publishOutcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    if publishErr := awaitOutcome(t, "publish on a wedged write", publishOutcome, 2*time.Second); nil == publishErr {
        t.Fatalf("expected the wedged publish to fail")
    }

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    closeErr := awaitOutcome(t, "close on a wedged caller-owned connection", closeOutcome, 3*time.Second)
    if false == errorChainContains(closeErr, "left a publish write blocked on a caller-owned connection") {
        t.Fatalf("expected close to name the write it could not end, got: %v", closeErr)
    }
}

func TestServerSentEventBackplane_RefusalKeepsTheChannelForATimedOutWriteAndForAClosingBackplane(t *testing.T) {
    instance := &ServerSentEventBackplane{}

    if true == instance.refusalKeepsTheChannel(errors.New("channel is closed")) {
        t.Fatalf("an ordinary failure must let the caller reset the channel it failed on")
    }

    if false == instance.refusalKeepsTheChannel(fmt.Errorf("wrapped: %w", errServerSentEventBackplanePublishTimedOut)) {
        t.Fatalf("a write that ran out of time is still holding the channel; resetting it joins that write")
    }

    instance.mutex.Lock()
    instance.closing = true
    instance.mutex.Unlock()

    if false == instance.refusalKeepsTheChannel(errors.New("channel is closed")) {
        t.Fatalf("a closing backplane has nothing to reopen, so it keeps the channel it has")
    }
}

func TestServerSentEventBackplane_IsClosingAnswersWhileAChannelCloseIsOnAWedgedSocket(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.mutex",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    channel, _, channelErr := backplane.ensurePublishChannel()
    if nil != channelErr {
        t.Fatalf("ensurePublishChannel: %v", channelErr)
    }

    gated.Wedge()

    go backplane.resetPublishChannel(channel)

    awaitBlockedWrites(t, gated, 1)

    answered := make(chan bool, 1)
    go func() { answered <- backplane.isClosing() }()

    select {
    case <-answered:
    case <-time.After(2 * time.Second):
        t.Fatalf("isClosing did not answer while a channel close was on the wedged socket")
    }
}

func backplaneWatchQueueDeliveries(t *testing.T, connection *amqp091.Connection, exchange string) <-chan amqp091.Delivery {
    t.Helper()

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        t.Fatalf("watch channel: %v", channelErr)
    }
    t.Cleanup(func() { _ = channel.Close() })

    queue, declareErr := channel.QueueDeclare("", false, true, true, false, nil)
    if nil != declareErr {
        t.Fatalf("watch queue: %v", declareErr)
    }

    if bindErr := channel.QueueBind(queue.Name, "", exchange, false, nil); nil != bindErr {
        t.Fatalf("watch bind: %v", bindErr)
    }

    deliveries, consumeErr := channel.Consume(queue.Name, "", true, true, false, false, nil)
    if nil != consumeErr {
        t.Fatalf("watch consume: %v", consumeErr)
    }

    return deliveries
}

func TestServerSentEventBackplane_CloseClosesTheChannelsWhenNoWriteIsInFlight(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, _ := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    backplane.mutex.Lock()
    publishChannel := backplane.publishChannel
    consumeChannel := backplane.consumeChannel
    backplane.mutex.Unlock()

    releaseBackplanePublish := holdPublishMutex(t, &backplane.publishMutex)

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    closeErr := awaitOutcome(t, "close with the publish half merely busy", closeOutcome, 5*time.Second)

    releaseBackplanePublish()

    if true == errorChainContains(closeErr, "left a publish write blocked on a caller-owned connection") {
        t.Fatalf("teardown named a blocked write over a socket nothing was ever written to: %v", closeErr)
    }

    if nil == publishChannel || false == publishChannel.IsClosed() {
        t.Fatalf("the publish channel was left open, and the field it lived in is already nil")
    }

    if nil == consumeChannel || false == consumeChannel.IsClosed() {
        t.Fatalf("the consume channel was left open, and the field it lived in is already nil")
    }
}

func TestServerSentEventBackplane_ABroadcastQueuedBehindAnotherIsNotReportedAsAWedgedWrite(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, _ := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    backplane.mutex.Lock()
    publishChannelBefore := backplane.publishChannel
    backplane.mutex.Unlock()

    releaseBackplanePublish := holdPublishMutex(t, &backplane.publishMutex)

    publishOutcome := make(chan error, 1)
    go func() { publishOutcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "queued"}) }()

    publishErr := awaitOutcome(t, "broadcast queued behind the publish mutex", publishOutcome, 3*time.Second)

    releaseBackplanePublish()

    if nil == publishErr || false == errorChainContains(publishErr, "did not reach the socket within the call timeout") {
        t.Fatalf("expected the refusal to name the queue rather than a blocked write, got: %v", publishErr)
    }

    backplane.mutex.Lock()
    wedged := backplane.wedged
    publishChannelAfter := backplane.publishChannel
    backplane.mutex.Unlock()

    if true == wedged {
        t.Fatalf("a broadcast that never reached the socket marked the whole backplane wedged, so every later broadcast is refused at once")
    }

    if publishChannelBefore != publishChannelAfter {
        t.Fatalf("a broadcast that only stood in the queue reset the publish channel the broadcasts ahead of it were writing on")
    }
}

func TestServerSentEventBackplane_ABroadcastAbandonedWhileQueuedIsNeverWritten(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, _ := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    deliveries := backplaneWatchQueueDeliveries(t, connection, "melody.sse.test.wedge")

    releaseBackplanePublish := holdPublishMutex(t, &backplane.publishMutex)

    publishOutcome := make(chan error, 1)
    go func() { publishOutcome <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "abandoned"}) }()

    if publishErr := awaitOutcome(t, "broadcast abandoned while queued", publishOutcome, 3*time.Second); nil == publishErr {
        t.Fatalf("expected the queued broadcast to be refused")
    }

    releaseBackplanePublish()

    awaitNoPublishGoroutine(t, "(*ServerSentEventBackplane).publishOnce.func", 3*time.Second)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "fence"}); nil != publishErr {
        t.Fatalf("fence publish: %v", publishErr)
    }

    fenceTerm := time.After(3 * time.Second)

    for {
        select {
        case delivery, open := <-deliveries:
            if false == open {
                t.Fatalf("the watching queue's delivery channel closed before the fence arrived")
            }

            body := string(delivery.Body)

            if true == strings.Contains(body, `"abandoned"`) {
                t.Fatalf("the broadcast the caller was told had failed was published anyway: %s", body)
            }

            if true == strings.Contains(body, `"fence"`) {
                return
            }
        case <-fenceTerm:
            t.Fatalf("the fence published after the abandoned broadcast never reached the watching queue")
        }
    }
}

func TestServerSentEventBackplane_CloseWaitsForACallerOwnedChannelCloseBeyondTheCallTimeout(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Connection:  connection,
        Hub:         hub,
        Exchange:    "melody.sse.test.slowclose",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    gated.HoldReplies()
    go func() {
        time.Sleep(600 * time.Millisecond)
        gated.ReleaseReplies()
    }()

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    if closeErr := awaitOutcome(t, "close over a broker that answers the channel close late", closeOutcome, 5*time.Second); nil != closeErr {
        t.Fatalf("a channel close the broker answered three call timeouts later was reported as a failure: %v", closeErr)
    }
}

func TestServerSentEventBackplane_ResolveExpiredWriteAnswersAWriteThatAlreadyReturned(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    backplane.mutex.Lock()
    connection := backplane.connection
    backplane.mutex.Unlock()

    if nil == connection || true == connection.IsClosed() {
        t.Fatalf("the backplane must own a live connection for this to mean anything")
    }

    written := make(chan struct{})
    close(written)

    finished := errors.New("the outcome the write itself produced")
    outcome := make(chan error, 1)
    outcome <- finished

    if resolvedErr := backplane.resolveExpiredWrite(nil, written, outcome); false == errors.Is(resolvedErr, finished) {
        t.Fatalf("expected the write's own outcome, got: %v", resolvedErr)
    }

    if true == connection.IsClosed() {
        t.Fatalf("a write that had already returned was abandoned: the owned connection was cut under a broadcast that was done")
    }
}

func TestServerSentEventBackplane_CloseGivesAnOwnedConnectionItsHandshakeWhenNothingIsInFlight(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.wedge",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("healthy publish: %v", publishErr)
    }

    releaseBackplanePublish := holdPublishMutex(t, &backplane.publishMutex)

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.Close() }()

    closeErr := awaitOutcome(t, "close of an owned connection with the publish half busy", closeOutcome, 5*time.Second)

    releaseBackplanePublish()

    if nil != closeErr {
        t.Fatalf("the close handshake was cut off over a socket nothing was written to: %v", closeErr)
    }
}

func TestServerSentEventBackplane_CloseWithContextClosesAnOwnedConnectionUnderACancelledContext(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.spent",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if false == backplane.ownsConnection {
        t.Fatalf("this test needs a backplane that OWNS its connection; it measures the owned branch")
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, hasDeadline := cancelledContext.Deadline(); true == hasDeadline {
        t.Fatalf("this test needs a cancellation with NO deadline, this context carries one")
    }

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.CloseWithContext(cancelledContext) }()

    if closeErr := awaitOutcome(t, "close of a healthy owned connection under a cancellation", closeOutcome, 5*time.Second); nil != closeErr {
        t.Fatalf("a healthy owned connection closed under a cancellation reported a failure: %v", closeErr)
    }
}

func TestServerSentEventBackplane_CloseWithContextClosesAnOwnedConnectionUnderASpentDeadline(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.spent.deadline",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.CloseWithContext(spentContext) }()

    if closeErr := awaitOutcome(t, "close of a healthy owned connection under a spent deadline", closeOutcome, 5*time.Second); nil != closeErr {
        t.Fatalf("a healthy owned connection closed under a spent deadline reported a failure: %v", closeErr)
    }
}

func TestServerSentEventBackplane_CloseWithContextStillReportsACutWedgedWrite(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    dialer := newGatedDialer(t, dsn)

    hub := melodyhttp.NewServerSentEventHub()
    backplane := NewServerSentEventBackplane(ServerSentEventBackplaneConfig{
        Dialer:      dialer.Dial,
        Hub:         hub,
        Exchange:    "melody.sse.test.spent.wedged",
        CallTimeout: 200 * time.Millisecond,
    })
    awaitBackplaneSubscribed(t, backplane)

    if false == backplane.ownsConnection {
        t.Fatalf("this test needs a backplane that OWNS its connection; it measures the cut of the owned branch")
    }

    gated := dialer.Latest()
    if nil == gated {
        t.Fatalf("the gated connection was never recorded; there is no socket to wedge")
    }

    if publishErr := backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "healthy"}); nil != publishErr {
        t.Fatalf("the healthy publish failed, so nothing below measures a wedged one: %v", publishErr)
    }

    gated.Wedge()

    publishing := make(chan error, 1)
    go func() { publishing <- backplane.Publish("orders", melodyhttp.ServerSentEvent{Data: "wedged"}) }()

    deadline := time.Now().Add(2 * time.Second)
    for 0 == backplane.writesInFlight.Load() {
        if true == time.Now().After(deadline) {
            t.Fatalf("the publish never reached the socket; there is nothing wedged for the close to cut")
        }

        time.Sleep(5 * time.Millisecond)
    }

    if 0 != gated.BlockedWrites() && 0 == backplane.writesInFlight.Load() {
        t.Fatalf("the write in flight ended before the close; the measurement would be of a free publish half")
    }

    spentContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    closeOutcome := make(chan error, 1)
    go func() { closeOutcome <- backplane.CloseWithContext(spentContext) }()

    closeErr := awaitOutcome(t, "close of an owned connection with a wedged write", closeOutcome, 5*time.Second)

    <-publishing

    if nil == closeErr {
        t.Fatalf("a cut wedged write was reported as a clean close")
    }
}

func TestServerSentEventBackplane_AZeroCallTimeoutIsTheDefaultBudget(t *testing.T) {
    if resolved := (&ServerSentEventBackplane{}).resolvedCallTimeout(); defaultServerSentEventBackplaneCallTimeout != resolved {
        t.Fatalf("expected a backplane built without a call timeout to take the default %v, got %v", defaultServerSentEventBackplaneCallTimeout, resolved)
    }
}

func TestServerSentEventBackplane_CloseWithContextReturnsWithinTheDeadlineWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    backplaneContext, backplaneCancel := context.WithCancel(context.Background())
    backplane := &ServerSentEventBackplane{
        connection:     wedge.connection,
        ownsConnection: true,
        publishChannel: wedge.channel,
        hub:            melodyhttp.NewServerSentEventHub(),
        exchange:       "melody.sse.test.fake.broker",
        ctx:            backplaneContext,
        cancel:         backplaneCancel,
    }

    holdPublishMutex(t, &backplane.publishMutex)
    backplane.writesInFlight.Add(1)
    defer backplane.writesInFlight.Add(-1)

    bound := 500 * time.Millisecond
    closeContext, cancel := context.WithTimeout(context.Background(), bound)
    defer cancel()

    closeOutcome := make(chan error, 1)
    started := time.Now()
    go func() { closeOutcome <- backplane.CloseWithContext(closeContext) }()

    select {
    case closeErr := <-closeOutcome:
        t.Logf("close returned after %s under a deadline of %s: %v", time.Since(started), bound, closeErr)

        if nil == closeErr {
            t.Fatal("expected the close that could not finish reported")
        }
    case <-time.After(bound + 2*time.Second):
        t.Fatalf("the close did not return within the deadline %s plus two seconds; it is held behind the client's stalled shutdown", bound)
    }
}

func TestServerSentEventBackplane_ARedialOverAWedgedPublishChannelDoesNotHoldTheMutex(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    fresh := dialFakeBroker(t)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &ServerSentEventBackplane{
        connection:     wedge.connection,
        ownsConnection: true,
        publishChannel: wedge.channel,
        dialer:         func() (*amqp091.Connection, error) { return fresh.connection, nil },
        reconnect:      resolveReconnectConfig(nil, nil),
        ctx:            ctx,
        cancel:         cancel,
    }
    instance.joinBound = 5 * time.Second

    redialed := make(chan error, 1)
    started := time.Now()
    go func() {
        _, redialErr := instance.liveConnection()
        redialed <- redialErr
    }()

    select {
    case redialErr := <-redialed:
        if nil != redialErr {
            t.Fatalf("expected the redial to succeed, got %v", redialErr)
        }
    case <-time.After(time.Second):
        t.Fatal("the redial did not return within a second; it waits on the close of the wedged channel")
    }

    t.Logf("the redial returned after %s", time.Since(started))

    answered := make(chan bool, 1)
    go func() { answered <- instance.isClosing() }()

    select {
    case <-answered:
    case <-time.After(100 * time.Millisecond):
        t.Fatal("isClosing did not answer while the old channel's close is pending; the mutex is held")
    }
}

func TestServerSentEventBackplane_CloseWithContextReturnsWithinTheDeadlineWhenAnEarlierCutHasNotFreedTheWrite(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginUnfreedCut(t, 4*time.Second)

    backplaneContext, backplaneCancel := context.WithCancel(context.Background())
    backplane := &ServerSentEventBackplane{
        connection:     wedge.connection,
        ownsConnection: true,
        publishChannel: wedge.channel,
        hub:            melodyhttp.NewServerSentEventHub(),
        exchange:       "melody.sse.test.fake.broker",
        ctx:            backplaneContext,
        cancel:         backplaneCancel,
        callTimeout:    200 * time.Millisecond,
    }

    holdPublishMutex(t, &backplane.publishMutex)
    backplane.writesInFlight.Add(1)
    defer backplane.writesInFlight.Add(-1)

    bound := 500 * time.Millisecond
    closeContext, cancel := context.WithTimeout(context.Background(), bound)
    defer cancel()

    closeOutcome := make(chan error, 1)
    started := time.Now()
    go func() { closeOutcome <- backplane.CloseWithContext(closeContext) }()

    select {
    case closeErr := <-closeOutcome:
        t.Logf("close returned after %s under a deadline of %s: %v", time.Since(started), bound, closeErr)
    case <-time.After(bound + time.Second):
        t.Fatalf("the close did not return within the deadline %s plus one second; the channel close is parked on the mutex the unfreed write holds", bound)
    }
}

func TestServerSentEventBackplane_CloseNamesTheWriteLeftBlockedOnAConnectionAnEarlierAbandonCut(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    backplaneContext, backplaneCancel := context.WithCancel(context.Background())
    backplane := &ServerSentEventBackplane{
        connection:     wedge.connection,
        ownsConnection: true,
        publishChannel: wedge.channel,
        hub:            melodyhttp.NewServerSentEventHub(),
        exchange:       "melody.sse.test.fake.broker",
        ctx:            backplaneContext,
        cancel:         backplaneCancel,
        callTimeout:    200 * time.Millisecond,
    }
    backplane.joinBound = 300 * time.Millisecond

    holdPublishMutex(t, &backplane.publishMutex)
    backplane.writesInFlight.Add(1)
    defer backplane.writesInFlight.Add(-1)

    if abandonErr := backplane.abandonWedgedPublish(wedge.connection, wedge.written); nil == abandonErr || false == strings.Contains(abandonErr.Error(), "the write has not returned") {
        t.Fatalf("expected a cut that did not free the write, got %v", abandonErr)
    }

    closeContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    closeErr := backplane.CloseWithContext(closeContext)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "the owned connection, already cut") {
        t.Fatalf("expected the close to name the write left blocked on the cut connection, got %v", closeErr)
    }
}

func TestServerSentEventBackplane_ADialThatLandsAfterCloseBeganIsClosedOffTheCallersPath(t *testing.T) {
    fake := dialFakeBroker(t)

    var backplane *ServerSentEventBackplane
    backplane = &ServerSentEventBackplane{
        ctx:         context.Background(),
        callTimeout: time.Second,
        dialer: func() (*amqp091.Connection, error) {
            backplane.mutex.Lock()
            backplane.closing = true
            backplane.mutex.Unlock()

            fake.gated.Wedge()

            return fake.connection, nil
        },
    }

    connectOutcome := make(chan error, 1)
    go func() {
        _, connectErr := backplane.liveConnection()
        connectOutcome <- connectErr
    }()

    select {
    case connectErr := <-connectOutcome:
        if nil == connectErr {
            t.Fatal("expected liveConnection to refuse a dial that landed after Close began")
        }
    case <-time.After(300 * time.Millisecond):
        t.Fatal("liveConnection did not return before the late connection's close budget ran out; the close is held on the wedged socket")
    }

    select {
    case <-fake.gated.closed:
    case <-time.After(3 * time.Second):
        t.Fatal("the late connection's socket was never closed; the dial that landed after Close began is leaked")
    }
}

func TestServerSentEventBackplane_ADialThatLandsAfterTheCancellationIsClosedWithinOneCallTimeout(t *testing.T) {
    fake := dialFakeBroker(t)

    release := make(chan struct{})
    backplaneContext, cancel := context.WithCancel(context.Background())
    backplane := &ServerSentEventBackplane{
        ctx:         backplaneContext,
        cancel:      cancel,
        callTimeout: 200 * time.Millisecond,
        dialer: func() (*amqp091.Connection, error) {
            <-release

            fake.gated.Wedge()

            return fake.connection, nil
        },
    }

    dialOutcome := make(chan error, 1)
    go func() {
        _, dialErr := backplane.dialWithContext()
        dialOutcome <- dialErr
    }()

    cancel()

    select {
    case dialErr := <-dialOutcome:
        if nil == dialErr {
            t.Fatal("expected the cancelled dial to report the cancellation")
        }
    case <-time.After(time.Second):
        t.Fatal("the cancellation did not interrupt the dial")
    }

    close(release)

    select {
    case <-fake.gated.closed:
    case <-time.After(2 * time.Second):
        t.Fatal("the late connection's socket was never closed; its close is parked behind a write the broker never reads")
    }
}

func TestServerSentEventBackplane_TheFirstDialIsNotAReconnect(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &ServerSentEventBackplane{
        ctx:    ctx,
        cancel: cancel,
        dialer: func() (*amqp091.Connection, error) {
            return nil, errors.New("dial refused")
        },
    }

    _, firstErr := instance.liveConnection()
    if nil == firstErr || "amqp sse backplane first dial failed" != firstErr.Error() {
        t.Fatalf("expected the first dial named, got %v", firstErr)
    }

    instance.connectedOnce = true

    _, reconnectErr := instance.liveConnection()
    if nil == reconnectErr || "amqp sse backplane reconnect dial failed" != reconnectErr.Error() {
        t.Fatalf("expected the reconnect named after a connection was held, got %v", reconnectErr)
    }
}

func TestServerSentEventBackplane_APanickingDialerIsAnsweredAsADialFailure(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &ServerSentEventBackplane{
        ctx:    ctx,
        cancel: cancel,
        dialer: func() (*amqp091.Connection, error) {
            panic("the application's dialer exploded")
        },
    }

    _, dialErr := instance.liveConnection()
    if nil == dialErr || "amqp sse backplane first dial failed" != dialErr.Error() {
        t.Fatalf("expected the dial failure, got %v", dialErr)
    }

    if cause := errors.Unwrap(dialErr); nil == cause || "amqp sse backplane dialer panicked" != cause.Error() {
        t.Fatalf("expected the dialer's panic as the cause, got %v", cause)
    }
}

func TestServerSentEventBackplane_PublishReturnsOnACyclicChainFromTheDialer(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    instance := &ServerSentEventBackplane{
        ctx:    ctx,
        cancel: cancel,
        dialer: func() (*amqp091.Connection, error) {
            return nil, newCyclicChainError()
        },
    }

    assertReturnsOnACyclicChain(t, func() {
        if publishErr := instance.Publish("topic", melodyhttp.ServerSentEvent{}); nil == publishErr {
            t.Error("expected the dialer's refusal")
        }
    })
}

func TestServerSentEventBackplane_ListenReturnsOnACyclicChainFromTheDialer(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    dialed := make(chan struct{}, 1)
    instance := &ServerSentEventBackplane{
        reconnect: resolveReconnectConfig(nil, nil),
        ctx:       ctx,
        cancel:    cancel,
        dialer: func() (*amqp091.Connection, error) {
            select {
            case dialed <- struct{}{}:
            default:
            }

            return nil, newCyclicChainError()
        },
    }

    instance.wait.Add(1)

    assertReturnsOnACyclicChain(t, func() {
        go func() {
            <-dialed
            cancel()
        }()

        instance.listen()
    })
}
