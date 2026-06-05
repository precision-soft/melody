package websocket

import (
    "context"
    nethttp "net/http"
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

    /**
     * OnMessage is invoked for each inbound frame on the connection's read goroutine, in order. It
     * must not block: a slow OnMessage stalls the read loop, delaying close/ping detection for that
     * connection. Hand long work off to your own queue/worker and return promptly.
     */
    OnMessage       func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte)
    SubscribeBuffer int
    WriteTimeout    time.Duration
    OriginPatterns  []string
    BinaryWrites    bool

    /** ReadLimit caps the byte size of a single inbound message; 0 keeps coder/websocket's default (32 KiB). */
    ReadLimit int64
}

func NewStreamHandler(hub *melodyhttp.SseHub, options Options) httpcontract.Handler {
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

        go readLoop(connectionContext, cancel, connection, runtimeInstance, options)

        for {
            select {
            case <-connectionContext.Done():
                return nil, nil
            case event, open := <-subscriber.Events():
                if false == open {
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
) {
    for {
        messageType, payload, readErr := connection.Read(ctx)
        if nil != readErr {
            cancel()
            return
        }

        if nil != options.OnMessage {
            options.OnMessage(runtimeInstance, messageType, payload)
        }
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
