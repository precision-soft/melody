package event

import (
    "errors"
    "net"
    nethttp "net/http"
    "time"

    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* StreamHandler resolves the hub per request rather than taking it at registration, so the provider that installs the hub's logger runs and the container knows it has a hub to close. */
func StreamHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        topic := queryStringOr(request, "topic", CatalogTopic)

        /* the client names the topic, so opening a stream is not permission to read the topic asked for. The gate stands ahead of the writer because NewServerSentEventWriter commits the response (headers, 200, flush), and the kernel discards what a handler returns after that. */
        if false == topicIsReadableBy(runtimeInstance, topic) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "not allowed to subscribe to this topic"), nil
        }

        hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
        if nil != hubErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the event stream is unavailable", hubErr), nil
        }

        /* a hub that shut down still takes a subscription and ends it at once, so a stream that cannot be served is refused before the writer commits a 200 the client would reconnect into */
        if true == hub.IsClosed() {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the event stream is shutting down"), nil
        }

        serverSentEventWriter, serverSentEventErr := melodyhttp.NewServerSentEventWriter(writer)
        if nil != serverSentEventErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported", serverSentEventErr), nil
        }

        /* the server arms its write deadline once, from the request line, so the writer re-arms it per frame with the server's own write timeout as the budget. It bounds a write: a client that stops reading is cut one budget after the socket stops buffering, and the application declares no bound on a stream's whole life. */
        writeBudget := serverWriteTimeoutOf(request)
        serverSentEventWriter.WithWriteBudget(writeBudget)

        subscriber := hub.Subscribe(topic, 16)
        defer hub.Unsubscribe(subscriber)

        commentErr := serverSentEventWriter.Comment("connected")
        if nil != commentErr {
            return nil, nil
        }

        requestContext := request.HttpRequest().Context()

        /* the keepalive keeps an idle stream alive under the re-armed deadline and finds a client that left without closing, since only a write can fail on a dead connection */
        keepalive := time.NewTicker(keepaliveIntervalFor(writeBudget))
        defer keepalive.Stop()

        for {
            select {
            case <-requestContext.Done():
                return nil, nil
            case <-keepalive.C:
                if pingErr := serverSentEventWriter.Ping(); nil != pingErr {
                    journalServerSideCut(runtimeInstance, topic, "keepalive", pingErr)

                    return nil, nil
                }
            case event, open := <-subscriber.Events():
                if false == open {
                    return nil, nil
                }

                if sendErr := serverSentEventWriter.Send(event); nil != sendErr {
                    journalServerSideCut(runtimeInstance, topic, "event", sendErr)

                    return nil, nil
                }

                /* the tick is counted from the last frame, so a busy stream pays for no keepalive it does not need */
                keepalive.Reset(keepaliveIntervalFor(writeBudget))
            }
        }
    }
}

/* defaultKeepaliveInterval is the keepalive of a stream whose server declared no write timeout, so dead-client detection does not depend on a deadline */
const defaultKeepaliveInterval = 15 * time.Second

/* keepaliveIntervalFor is half the write budget, so two keepalives fit inside every budget; without a budget it is the default */
func keepaliveIntervalFor(writeBudget time.Duration) time.Duration {
    if 0 >= writeBudget {
        return defaultKeepaliveInterval
    }

    /* a ticker refuses a zero interval, and the floor keeps a budget under two seconds from producing one */
    return max(writeBudget/2, time.Second)
}

/* serverWriteTimeoutOf reads the write timeout of the server the request arrived on, from the context net/http puts it on; a request without a server has none to re-arm. */
func serverWriteTimeoutOf(request melodyhttpcontract.Request) time.Duration {
    server, isServer := request.HttpRequest().Context().Value(nethttp.ServerContextKey).(*nethttp.Server)
    if false == isServer || nil == server {
        return 0
    }

    return server.WriteTimeout
}

/* journalServerSideCut tells a client that left, a broken pipe or a reset, from a server cut because the re-armed write deadline expired with a frame in flight, which is a lost frame worth a warning. The two are told apart on the error: net/http cancels the request context on any write error, while only a deadline reports a net.Error whose Timeout is true. */
func journalServerSideCut(runtimeInstance melodyruntimecontract.Runtime, topic string, frame string, writeErr error) {
    var networkErr net.Error
    if false == errors.As(writeErr, &networkErr) || false == networkErr.Timeout() {
        return
    }

    /* the journal goes through the application's door with a fallback, since LoggerFromRuntime answers nil for a runtime without a logger */
    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
        "event stream cut by the server with a frame in flight",
        melodyexception.LogContext(writeErr, melodyexceptioncontract.Context{"topic": topic, "frame": frame}),
    )
}
