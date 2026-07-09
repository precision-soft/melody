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
        liveness := &connectionLiveness{}
        liveness.recordActivity()

        go readLoop(connectionContext, cancel, connection, runtimeInstance, options, liveness)

        if 0 < options.IdleTimeout {
            go pingLoop(connectionContext, cancel, connection, options.IdleTimeout, liveness)
        }

        for {
            select {
            case <-connectionContext.Done():
                closeNormally(connection)
                return nil, nil
            case event, open := <-subscriber.Events():
                if false == open {
                    closeNormally(connection)
                    return nil, nil
                }

                writeContext, writeCancel := context.WithTimeout(connectionContext, writeTimeout(options))
                writeErr := connection.Write(writeContext, writeMessageType(options), []byte(event.Data))
                writeCancel()
                if nil != writeErr {
                    logDebug(runtimeInstance, "websocket write failed, closing connection", writeErr)
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

/* connectionLiveness lets the ping loop distinguish a peer that is gone from a reader that is merely unable to answer: a pong is processed only inside connection.Read, which the read loop leaves while it runs a synchronous OnMessage callback. */
type connectionLiveness struct {
    lastActivityUnixNano atomic.Int64
    callbacksRunning     atomic.Int64
}

func (instance *connectionLiveness) recordActivity() {
    instance.lastActivityUnixNano.Store(time.Now().UnixNano())
}

func (instance *connectionLiveness) enterCallback() {
    instance.callbacksRunning.Add(1)
}

func (instance *connectionLiveness) leaveCallback() {
    instance.callbacksRunning.Add(-1)
    instance.recordActivity()
}

/* cannotAnswer reports whether a pong could not have been processed even by a perfectly healthy peer: the reader is inside a callback, or it returned from one so recently that the peer was demonstrably alive within the window. */
func (instance *connectionLiveness) cannotAnswer(window time.Duration) bool {
    if 0 < instance.callbacksRunning.Load() {
        return true
    }

    return time.Since(time.Unix(0, instance.lastActivityUnixNano.Load())) < window
}

/* @important keepalive ping loop: a half-open or silently-stalled client cannot be detected by reads alone on a broadcast stream that never expects client frames, so without this an idle connection would pin a goroutine forever. Each tick sends a ping bounded by the same interval; the read loop delivers the pong, so a still-connected client keeps the connection alive while an unresponsive one trips the timeout and cancels the connection context, unwinding the handler and read loop.

A pong is only ever processed inside connection.Read, and the read loop is not inside Read while it runs a synchronous OnMessage callback. A ping issued in that window therefore times out no matter how healthy the peer is. A timed-out ping is treated as death only when the read loop has also seen nothing from the peer for two intervals; a write failure needs no such grace, since the socket itself is gone. */
func pingLoop(
    ctx context.Context,
    cancel context.CancelFunc,
    connection *coderwebsocket.Conn,
    interval time.Duration,
    liveness *connectionLiveness,
) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            pingContext, pingCancel := context.WithTimeout(ctx, interval)
            pingErr := connection.Ping(pingContext)
            pingCancel()

            if nil == pingErr {
                continue
            }

            if nil != ctx.Err() {
                return
            }

            if true == errors.Is(pingErr, context.DeadlineExceeded) && true == liveness.cannotAnswer(2*interval) {
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

func closeNormally(connection *coderwebsocket.Conn) {
    _ = connection.Close(coderwebsocket.StatusNormalClosure, "")
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
