package amqp

import (
    "context"
    "errors"
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

    first, firstErr := backplane.ensurePublishChannel()
    if nil != firstErr {
        t.Fatalf("first ensurePublishChannel: %v", firstErr)
    }

    first.Close()
    if false == first.IsClosed() {
        t.Fatalf("expected the channel to report closed after Close")
    }

    second, secondErr := backplane.ensurePublishChannel()
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

    /* a static connection closed under the backplane, with no dialer: the terminal receive-death this report exists for */
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

/* recordingBackplaneLogger captures error records so a test can read what the backplane reported. */
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

/* awaitBackplaneSubscribed waits for the listen goroutine to finish its subscribe RPCs, so a wedge set afterwards catches a publish write and not the subscription setup. */
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
    if false == errorChainContains(secondErr, "an earlier write is still blocked on the caller-owned connection") {
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

/* the socket wedges with nothing in flight, so no publish is there to cut it: Close's own deadline is the only bound */
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
