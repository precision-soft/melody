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

    /* IdleTimeout is the positive ping interval and pong wait for silent peers. Missing pong closes the connection. NewStreamHandler refuses non-positive values. */
    IdleTimeout time.Duration
}

/* NewStreamHandler bridges a websocket connection onto a server-sent-event hub topic.

   A zero IdleTimeout is refused rather than defaulted, because nothing else in the stack can reap a peer that goes away without a fin. coderwebsocket.Accept hijacks the connection, so http.Server's read and write timeouts stop applying to it; the read loop then blocks in Read with no deadline of its own; and a write into a half-open socket keeps succeeding for as long as the send buffer has room, so a broadcast is no liveness signal either. The keepalive ping is the only remaining evidence, which makes its interval a required decision rather than a tunable with a sensible off position — left at zero, an attacker opens connections and abandons them and each one costs a descriptor, a hub subscription and three goroutines for the life of the process.

   Refusing it is a wiring error and panics at construction, the way the framework reports every other unusable configuration: it surfaces at boot rather than as an unbounded leak in production. */
func NewStreamHandler(hub *melodyhttp.ServerSentEventHub, options Options) httpcontract.Handler {

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

        if true == hub.IsClosed() {
            return nil, exception.NewError(
                "websocket stream handler hub is shut down: refusing the connection rather than upgrading it to an instantly-closed stream",
                map[string]any{"path": request.HttpRequest().URL.Path},
                nil,
            )
        }

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

        if 0 != options.ReadLimit {
            connection.SetReadLimit(options.ReadLimit)
        }

        subscriber := hub.Subscribe(topic, subscribeBuffer(options))
        defer hub.Unsubscribe(subscriber)

        if true == hub.IsClosed() {
            logDebug(runtimeInstance, "websocket hub shut down during connect, closing the stream", nil)

            return nil, nil
        }

        connectionContext, cancel := context.WithCancel(request.HttpRequest().Context())
        defer cancel()

        liveness := newConnectionLiveness()
        liveness.recordActivity()

        readLoopDone := make(chan struct{})
        go func() {
            defer close(readLoopDone)

            readLoop(connectionContext, cancel, connection, runtimeInstance, options, liveness)
        }()

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

            logDebug(runtimeInstance, "websocket read loop ended", readErr)
            cancel()
            return
        }

        liveness.recordActivity()

        if nil != options.OnMessage {

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

const maximumCallbackGraceFactor = 10

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

func (instance *connectionLiveness) leaveCallback() {
    instance.recordActivity()
    instance.callbacksRunning.Add(-1)
}

func (instance *connectionLiveness) enterWrite() {
    instance.writeStartedOffset.Store(int64(instance.elapsed()))
    instance.writesRunning.Add(1)
}

func (instance *connectionLiveness) leaveWrite() {
    instance.writesRunning.Add(-1)
}

func (instance *connectionLiveness) writeInFlight() bool {
    return 0 < instance.writesRunning.Load()
}

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
                exception.NewError(fmt.Sprintf("websocket OnMessage panicked: %v", recovered), nil, exception.PanicCause(recovered)),
            )
            panicked = true
        }
    }()

    options.OnMessage(runtimeInstance, messageType, payload)

    return false
}

const closeHandshakeGrace = 1 * time.Second

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
