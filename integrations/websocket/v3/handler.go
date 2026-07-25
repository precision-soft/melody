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

    /* @info when greater than zero, the handler sends a websocket ping every IdleTimeout and closes the connection if the peer does not pong within that window, so an idle or half-open client cannot hold a goroutine and connection indefinitely (the hijacked connection is not covered by http.Server read timeouts). Zero (the default) disables keepalive and preserves the previous behavior; an actively-subscribed client that only receives stays connected because it answers the pings. */
    IdleTimeout time.Duration
}

func NewStreamHandler(hub *melodyhttp.ServerSentEventHub, options Options) httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        connection, acceptErr := coderwebsocket.Accept(writer, request.HttpRequest(), &coderwebsocket.AcceptOptions{
            OriginPatterns: options.OriginPatterns,
        })
        if nil != acceptErr {
            logError(runtimeInstance, "websocket upgrade failed", acceptErr)
            return nil, nil
        }
        defer connection.CloseNow()

        if 0 < options.ReadLimit {
            connection.SetReadLimit(options.ReadLimit)
        }

        topic := "default"
        if nil != options.TopicResolver {
            topic = options.TopicResolver(request)
        }

        subscriber := hub.Subscribe(topic, subscribeBuffer(options))
        defer hub.Unsubscribe(subscriber)

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

        if 0 < options.IdleTimeout {
            go pingLoop(connectionContext, cancel, connection, options.IdleTimeout, pingWriteGrace(options), liveness)
        }

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

/* maximumCallbackGraceFactor bounds, in ping intervals, how long a running OnMessage callback may go on excusing a timed-out ping. The excuse is legitimate — a pong is processed only inside connection.Read, which the reader leaves while the callback runs — but a callback that never returns would otherwise excuse every ping forever, and nothing else reaps a hijacked connection: the descriptor, the hub subscription and three goroutines would leak for the lifetime of the process, once per connection. Past the grace the connection is closed and the callback is left to finish (or not) on its own. */
const maximumCallbackGraceFactor = 10

/* connectionLiveness lets the ping loop distinguish a peer that is gone from a connection that merely cannot carry a ping or a pong right now: a pong is processed only inside connection.Read, which the read loop leaves while it runs a synchronous OnMessage callback, and a ping is written only once the data frame the handler is flushing has left the socket.

The activity, callback-start and write marks are stored as nanoseconds elapsed since base, a monotonic reading captured when the connection is accepted. A wall-clock timestamp (time.Now().UnixNano() compared with time.Since(time.Unix(0, n))) is defenceless against a clock step: a backward step would let a wedged callback excuse pings past the grace and leak the connection, and a forward step would reap a healthy one. time.Since(base) reads the monotonic clock, so the windows hold across any wall-clock adjustment. */
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

/* cannotAnswer reports whether a pong could not have been processed even by a perfectly healthy peer.

The ping was issued while the handler was flushing a data frame: coder/websocket serialises control frames behind the frame in flight, so that ping never reached the socket and its timeout tested nothing at all. This excuses the one ping that frame queued behind itself and nothing more — a write that has since COMPLETED is no evidence of anything, because a write into a half-open connection succeeds for as long as the socket send buffer has room, and by the time the next ping is issued the control-frame queue is free again. A frame that only started after the ping was issued is excused too, but only until writeGrace, by which point the write's own timeout has failed and the handler has torn the connection down.

Or the reader is inside a callback that is still within its grace, or it returned from one so recently that the peer was demonstrably alive within the window. A callback that has outrun the grace stops excusing anything — the peer's health is then unknowable, and holding the connection open on the strength of a stuck callback is what leaks it. */
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

/* @important keepalive ping loop: a half-open or silently-stalled client cannot be detected by reads alone on a broadcast stream that never expects client frames, so without this an idle connection would pin a goroutine forever. Each tick sends a ping bounded by the same interval; the read loop delivers the pong, so a still-connected client keeps the connection alive while an unresponsive one trips the timeout and cancels the connection context, unwinding the handler and read loop.

A pong is only ever processed inside connection.Read, and the read loop is not inside Read while it runs a synchronous OnMessage callback; a ping is also serialised behind whatever data frame the handler is flushing at the moment it is issued, which is why that is sampled before the ping rather than after it. A ping issued in either window therefore times out no matter how healthy the peer is. A timed-out ping is treated as death only when nothing excuses it and the peer has been silent for two intervals; a write failure needs no such grace, since the socket itself is gone.

Ping returns without error exactly when the peer's pong came back, which is the only liveness signal a receive-only client ever produces, so a successful ping records activity. */
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
            /* @important sampled BEFORE the ping, not inside the timeout branch below: only at this instant can a data frame put the ping behind itself. Reading it after the ping has expired inverts the rule both ways — the frame that actually blocked the ping has finished, so its excuse is lost and a slow client draining one large frame is disconnected, while a frame that started later grants an excuse it never earned and a dead peer survives for as long as broadcasts keep starting. */
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

/* @important the read goroutine runs outside the kernel's panic recovery, so a panic in the user OnMessage callback would crash the whole process; recover it, log it, and signal the connection to close. */
func dispatchOnMessage(
    runtimeInstance runtimecontract.Runtime,
    options Options,
    messageType coderwebsocket.MessageType,
    payload []byte,
) (panicked bool) {
    defer func() {
        recovered := recover()
        if nil != recovered {
            logError(
                runtimeInstance,
                "websocket OnMessage panicked",
                exception.NewError(fmt.Sprintf("%v", recovered), nil, nil),
            )
            panicked = true
        }
    }()

    options.OnMessage(runtimeInstance, messageType, payload)

    return false
}

/* closeHandshakeGrace bounds the closing handshake. connection.Close writes the close frame and then waits for the peer to send its own — a frame only the read loop ever reads. When the connection is being closed BECAUSE that read loop is wedged in a callback, the handshake can never complete, and coder/websocket waits five seconds before giving up: five seconds of a held handler goroutine and a live hub subscription, per connection. */
const closeHandshakeGrace = 1 * time.Second

/* closeConnection tears the socket down and then holds, bounded by closeHandshakeGrace, until the read loop has exited before the handler returns to the kernel.

The wait keeps the request scope alive. The runtime handed to a synchronous OnMessage callback is built over that scope, and the kernel closes the scope the instant the handler returns; a healthy in-flight callback would then resolve a service against a closed scope and panic. Holding until the read loop — and therefore the callback — has finished removes that race, unless the callback outruns the grace, the stuck-callback case the ping reaper already accepts.

The teardown path is chosen by whether a callback is in flight. When one is, the read loop is not inside connection.Read, so a graceful Close can never complete its handshake: it wins coder/websocket's close CAS and then holds the transport for the library's full five-second timeout, while the handler's deferred CloseNow loses that CAS and blocks the same span. CloseNow taken here wins the CAS and closes the transport at once, freeing the descriptor and the goroutines when the grace lapses rather than seconds later. With no callback in flight the read loop is blocked in Read (or already gone), so a graceful Close exchanges a close frame and the read loop's own cancel frees the transport promptly. */
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
