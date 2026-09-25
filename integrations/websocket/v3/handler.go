package websocket

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "sync/atomic"
    "time"

    coderwebsocket "github.com/coder/websocket"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Options struct {
    TopicResolver func(request httpcontract.Request) string

    OnMessage       func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte)
    SubscribeBuffer int
    WriteTimeout    time.Duration
    OriginPatterns  []string
    BinaryWrites    bool

    ReadLimit int64

    /* the interval at which the handler pings a silent peer and the window it allows for the pong; a peer that misses it has its connection closed, so an idle or half-open client cannot hold a descriptor, a hub subscription and three goroutines. It is required and positive; NewStreamHandler refuses a zero. A subscribed client that only receives stays connected, since RFC 6455 obliges it to answer the ping and browsers answer inside the protocol stack. 30s suits a browser client. */
    IdleTimeout time.Duration
}

/* NewStreamHandler bridges a websocket connection onto a server-sent-event hub topic. A zero IdleTimeout is refused rather than defaulted, because nothing else reaps a peer that goes away without a fin: coderwebsocket.Accept hijacks the connection so http.Server's timeouts stop applying, the read loop blocks in Read with no deadline, and a write into a half-open socket succeeds while the send buffer has room. The keepalive ping is the only liveness evidence, so a missing interval panics at construction, as every unusable configuration does, rather than leaking each abandoned connection's descriptor, hub subscription and three goroutines. */
func NewStreamHandler(hub *melodyhttp.ServerSentEventHub, options Options) httpcontract.Handler {
    /* the hub is dereferenced on every accepted connection, so a nil one must fail the boot the way the missing IdleTimeout does — not the first request */
    if nil == hub {
        exception.Panic(exception.NewError("websocket stream handler hub is nil", nil, nil))
    }

    if 0 >= options.IdleTimeout {
        exception.Panic(
            exception.NewError(
                "websocket options require a positive IdleTimeout: the keepalive ping is the only thing that can reap a peer that vanishes without a fin, because accept hijacks the connection out of http.Server's timeouts, the read loop blocks with no deadline and a write into a half-open socket still succeeds; set it to the interval at which a silent peer should be pinged, 30s for a browser client",
                map[string]any{"idleTimeout": options.IdleTimeout.String()},
                nil,
            ),
        )
    }

    return newStreamHandler(hub, options)
}

func newStreamHandler(hub *melodyhttp.ServerSentEventHub, options Options) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        /* a hub that has shut down serves no stream: Subscribe on it hands back a closed channel, which the event loop would read as an ordinary end of stream after a 101, indistinguishable from a peer that went away. The request is refused before the upgrade, as an empty resolved topic is, so a client connecting during the shutdown drain gets a plain error. */
        if true == hub.IsClosed() {
            return nil, exception.NewError(
                "websocket stream handler hub is shut down: refusing the connection rather than upgrading it to an instantly-closed stream",
                map[string]any{"path": request.HttpRequest().URL.Path},
                nil,
            )
        }

        /* the topic is resolved before the upgrade so a resolver that comes up empty refuses the request outright: an empty topic is only ever a failed extraction, the nil-resolver default being "default", and subscribing it would pool every mis-resolved client of every tenant on one shared "" topic */
        topic := "default"
        if nil != options.TopicResolver {
            topic = options.TopicResolver(request)
            if "" == topic {
                return nil, exception.NewError(
                    "websocket topic resolver returned an empty topic: refusing the connection rather than subscribing it to a shared degenerate topic",
                    map[string]any{"path": request.HttpRequest().URL.Path},
                    nil,
                )
            }
        }

        connection, acceptErr := coderwebsocket.Accept(writer, request.HttpRequest(), &coderwebsocket.AcceptOptions{
            OriginPatterns: options.OriginPatterns,
        })
        if nil != acceptErr {
            logError(runtimeInstance, "websocket upgrade failed", acceptErr)
            return nil, nil
        }
        defer connection.CloseNow()

        /* a positive limit caps the frame size and a negative one is passed through as coder/websocket's documented "no limit", so the library's 32 KiB default is not left armed for the payloads the option allows */
        if 0 != options.ReadLimit {
            connection.SetReadLimit(options.ReadLimit)
        }

        subscriber := hub.Subscribe(topic, subscribeBuffer(options))
        defer hub.Unsubscribe(subscriber)

        /* the hub can shut down between the check above and this Subscribe, handing back a closed channel; it is named at debug so a stream that never delivered is not silent in the journal, and the deferred CloseNow tears the socket down before any read or ping goroutine starts */
        if true == hub.IsClosed() {
            logDebug(runtimeInstance, "websocket hub shut down during connect, closing the stream", nil)

            return nil, nil
        }

        connectionContext, cancel := context.WithCancel(request.HttpRequest().Context())
        defer cancel()

        /* the read loop is the only place the library observes client frames — including pongs — so the ping loop reads these to tell "the peer is gone" from "the reader cannot answer right now" */
        liveness := newConnectionLiveness()
        liveness.recordActivity()

        /* the handler waits on readLoopDone before returning so a still-running OnMessage callback cannot outlive the request scope the kernel closes on return */
        readLoopDone := make(chan struct{})
        go func() {
            defer close(readLoopDone)

            readLoop(connectionContext, cancel, connection, runtimeInstance, options, liveness)
        }()

        /* unconditional: a positive IdleTimeout is established at construction, so every connection is reaped by the ping loop or by nothing at all */
        go pingLoop(connectionContext, cancel, connection, options.IdleTimeout, pingWriteGrace(options), liveness)

        for {
            select {
            case <-connectionContext.Done():
                closeConnection(connection, liveness, readLoopDone)
                return nil, nil
            case event, open := <-subscriber.Events():
                if false == open {
                    closeConnection(connection, liveness, readLoopDone)
                    return nil, nil
                }

                writeContext, writeCancel := context.WithTimeout(connectionContext, writeTimeout(options))
                liveness.enterWrite()
                writeErr := connection.Write(writeContext, writeMessageType(options), []byte(event.Data))
                liveness.leaveWrite()
                writeCancel()
                if nil != writeErr {
                    logDebug(runtimeInstance, "websocket write failed, closing connection", writeErr)
                    closeConnection(connection, liveness, readLoopDone)
                    return nil, nil
                }
            }
        }
    }
}

func readLoop(
    ctx context.Context,
    cancel context.CancelFunc,
    connection *coderwebsocket.Conn,
    runtimeInstance runtimecontract.Runtime,
    options Options,
    liveness *connectionLiveness,
) {
    for {
        messageType, payload, readErr := connection.Read(ctx)
        if nil != readErr {
            /* debug, not error: most read failures are ordinary disconnects, but a peer killed for exceeding the read limit or for a protocol violation still leaves a record */
            logDebug(runtimeInstance, "websocket read loop ended", readErr)
            cancel()
            return
        }

        liveness.recordActivity()

        if nil != options.OnMessage {
            /* the reader cannot answer a pong for as long as the callback runs, so tell the ping loop not to read its timeouts as a death */
            liveness.enterCallback()
            panicked := dispatchOnMessage(runtimeInstance, options, messageType, payload)
            liveness.leaveCallback()

            if true == panicked {
                cancel()
                return
            }
        }
    }
}

/* maximumCallbackGraceFactor bounds, in ping intervals, how long a running OnMessage callback may excuse a timed-out ping. A pong is processed only inside connection.Read, which the reader leaves while the callback runs, but a callback that never returns would otherwise excuse every ping forever and leak the hijacked connection's descriptor, hub subscription and three goroutines; past the grace the connection is closed and the callback is left to finish on its own. */
const maximumCallbackGraceFactor = 10

/* connectionLiveness lets the ping loop tell a peer that is gone from a connection that cannot carry a ping or a pong right now: a pong is processed only inside connection.Read, which the read loop leaves while it runs a synchronous OnMessage callback, and a ping is written only once the data frame being flushed has left the socket. The activity, callback-start and write marks are nanoseconds since base, a monotonic reading taken at accept, so a wall-clock step can neither let a wedged callback excuse pings past the grace nor reap a healthy connection. */
type connectionLiveness struct {
    base                  time.Time
    lastActivityOffset    atomic.Int64
    callbackStartedOffset atomic.Int64
    callbacksRunning      atomic.Int64
    writeStartedOffset    atomic.Int64
    writesRunning         atomic.Int64
}

func newConnectionLiveness() *connectionLiveness {
    return &connectionLiveness{base: time.Now()}
}

/* elapsed reports the monotonic time since the connection was accepted. */
func (instance *connectionLiveness) elapsed() time.Duration {
    return time.Since(instance.base)
}

func (instance *connectionLiveness) recordActivity() {
    instance.lastActivityOffset.Store(int64(instance.elapsed()))
}

func (instance *connectionLiveness) enterCallback() {
    instance.callbackStartedOffset.Store(int64(instance.elapsed()))
    instance.callbacksRunning.Add(1)
}

/* leaveCallback refreshes the activity mark before it decrements the running count, never the reverse: a ping loop that observes callbacksRunning drop to zero must already see a fresh activity mark, or it would read the pre-callback timestamp as staleness and reap a healthy connection at the very moment the callback returns. */
func (instance *connectionLiveness) leaveCallback() {
    instance.recordActivity()
    instance.callbacksRunning.Add(-1)
}

/* enterWrite marks the start before it increments the running count, never the reverse: a ping loop that observes writesRunning rise must already see the start mark, or a write with no mark yet would be read as one that outran its grace. */
func (instance *connectionLiveness) enterWrite() {
    instance.writeStartedOffset.Store(int64(instance.elapsed()))
    instance.writesRunning.Add(1)
}

func (instance *connectionLiveness) leaveWrite() {
    instance.writesRunning.Add(-1)
}

/* writeInFlight reports whether the handler is flushing a data frame right now; the ping loop samples it as it issues a ping, because that is the only moment at which a data frame can queue the ping behind itself. */
func (instance *connectionLiveness) writeInFlight() bool {
    return 0 < instance.writesRunning.Load()
}

/* cannotAnswer reports whether a pong could not have been processed even by a healthy peer. Either the ping was issued while a data frame was being flushed, and coder/websocket serialises control frames behind it, which excuses that one ping and a frame started after it only until writeGrace; a completed write is no evidence, since a write into a half-open connection succeeds while the send buffer has room. Or the reader is inside a callback still within its grace, or left one within the window; a callback past the grace excuses nothing, since holding the connection on its strength is what leaks it. */
func (instance *connectionLiveness) cannotAnswer(
    writeInFlightAtPing bool,
    window time.Duration,
    callbackGrace time.Duration,
    writeGrace time.Duration,
) bool {
    now := instance.elapsed()

    if true == writeInFlightAtPing {
        return true
    }

    if 0 < instance.writesRunning.Load() && now-time.Duration(instance.writeStartedOffset.Load()) < writeGrace {
        return true
    }

    if 0 < instance.callbacksRunning.Load() {
        return now-time.Duration(instance.callbackStartedOffset.Load()) < callbackGrace
    }

    return now-time.Duration(instance.lastActivityOffset.Load()) < window
}

/* pingLoop is the keepalive that detects a half-open or stalled client, which reads alone cannot on a broadcast stream that expects no client frames. Each tick sends a ping bounded by the interval; the read loop delivers the pong, and an unresponsive peer trips the timeout and cancels the connection context, unwinding the handler and the read loop. A ping issued during a synchronous OnMessage callback, or behind a data frame being flushed, times out however healthy the peer is, so a timed-out ping is death only when nothing excuses it and the peer was silent for two intervals; a write failure needs no grace. A successful ping records activity, the only liveness signal a receive-only client produces. */
func pingLoop(
    ctx context.Context,
    cancel context.CancelFunc,
    connection *coderwebsocket.Conn,
    interval time.Duration,
    writeGrace time.Duration,
    liveness *connectionLiveness,
) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            /* sampled before the ping, not in the timeout branch: only at this instant can a data frame queue the ping behind itself, and a later read would excuse a frame that did not block the ping while losing the excuse of the one that did */
            writeInFlight := liveness.writeInFlight()

            pingContext, pingCancel := context.WithTimeout(ctx, interval)
            pingErr := connection.Ping(pingContext)
            pingCancel()

            if nil == pingErr {
                liveness.recordActivity()

                continue
            }

            if nil != ctx.Err() {
                return
            }

            if true == errors.Is(pingErr, context.DeadlineExceeded) && true == liveness.cannotAnswer(writeInFlight, 2*interval, maximumCallbackGraceFactor*interval, writeGrace) {
                continue
            }

            cancel()

            return
        }
    }
}

/* the read goroutine runs outside the kernel's panic recovery, so a panic in the user OnMessage callback would crash the whole process; recover it, log it, and signal the connection to close. */
func dispatchOnMessage(
    runtimeInstance runtimecontract.Runtime,
    options Options,
    messageType coderwebsocket.MessageType,
    payload []byte,
) (panicked bool) {
    defer func() {
        recovered := recover()
        if nil != recovered {
            /* PanicCause keeps an error panic value whole, cause chain and context included; a non-error value renders through the message */
            logError(
                runtimeInstance,
                "websocket OnMessage panicked",
                exception.NewError(fmt.Sprintf("websocket OnMessage panicked: %v", recovered), nil, exception.PanicCause(recovered)),
            )
            panicked = true
        }
    }()

    options.OnMessage(runtimeInstance, messageType, payload)

    return false
}

/* closeHandshakeGrace bounds the closing handshake. connection.Close writes the close frame and waits for the peer's, which only the read loop reads; when the connection closes because that loop is wedged in a callback the handshake cannot complete, and coder/websocket would hold the handler goroutine and the hub subscription for five seconds. */
const closeHandshakeGrace = 1 * time.Second

/* closeConnection tears the socket down and holds, bounded by closeHandshakeGrace, until the read loop has exited before the handler returns to the kernel, which keeps the request scope alive for a synchronous OnMessage callback resolving services against it. The bound is deliberately much shorter than the reaper's callback grace, because this wait is on the teardown path, where a long wait per wedged callback would stall hub shutdown and process exit; a callback that outruns it is abandoned, its next scope resolution panics, dispatchOnMessage recovers and logs it, and the message is lost by design. With a callback in flight the read loop is outside connection.Read, so a graceful Close cannot complete and would hold the transport for the library's five seconds while the deferred CloseNow loses the close CAS; CloseNow taken here wins it and frees the transport at once. With none in flight a graceful Close exchanges a close frame and the read loop's cancel frees the transport promptly. */
func closeConnection(connection *coderwebsocket.Conn, liveness *connectionLiveness, readLoopDone <-chan struct{}) {
    if 0 < liveness.callbacksRunning.Load() {
        _ = connection.CloseNow()
    } else {
        go func() {
            _ = connection.Close(coderwebsocket.StatusNormalClosure, "")
        }()
    }

    timer := time.NewTimer(closeHandshakeGrace)
    defer timer.Stop()

    select {
    case <-readLoopDone:
    case <-timer.C:
    }
}

func writeMessageType(options Options) coderwebsocket.MessageType {
    if true == options.BinaryWrites {
        return coderwebsocket.MessageBinary
    }

    return coderwebsocket.MessageText
}

func subscribeBuffer(options Options) int {
    if 0 < options.SubscribeBuffer {
        return options.SubscribeBuffer
    }

    return 16
}

func writeTimeout(options Options) time.Duration {
    if 0 < options.WriteTimeout {
        return options.WriteTimeout
    }

    return 10 * time.Second
}

/* pingWriteGrace bounds how long an in-flight write may go on excusing a timed-out ping: one ping interval past the write's own timeout, by which point the write has failed and the handler has closed the connection. */
func pingWriteGrace(options Options) time.Duration {
    return writeTimeout(options) + options.IdleTimeout
}

func logError(runtimeInstance runtimecontract.Runtime, message string, err error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err))
}

func logDebug(runtimeInstance runtimecontract.Runtime, message string, err error) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Debug(message, exception.LogContext(err))
}
