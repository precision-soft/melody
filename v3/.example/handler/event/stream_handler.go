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

/* the hub is RESOLVED here rather than handed in at registration. Handed in, this handler — the one door of an http process that most needs the hub to be reporting — resolved the service zero times, so the provider that installs the logger never ran and the container never learned it had a hub to close. Resolving per request costs a container lookup the request pays anyway for everything else it resolves. */
func StreamHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        topic := queryStringOr(request, "topic", CatalogTopic)

        /* the topic is the client's to name, so being allowed to open a stream is not being allowed to read the one asked for: the catalog topic carries the product and user writes made behind RoleEditor.

           The gate stands ahead of the writer because NewServerSentEventWriter COMMITS the response — it sets the event-stream headers, writes 200 and flushes — and the kernel discards whatever a handler returns after the headers are committed. Decided below it, this refusal would reach neither the client, which reads a successful stream that closes at once and reconnects forever, nor the access log, which records the committed 200. */
        if false == topicIsReadableBy(runtimeInstance, topic) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "not allowed to subscribe to this topic"), nil
        }

        hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
        if nil != hubErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "the event stream is unavailable", hubErr), nil
        }

        /* a hub that shut down still takes a subscription and ends it at once, so the client would read a
           committed 200 that closes and reconnect into the same answer until the process is gone: a stream that
           cannot be served is refused before the writer commits anything */
        if true == hub.IsClosed() {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the event stream is shutting down"), nil
        }

        serverSentEventWriter, serverSentEventErr := melodyhttp.NewServerSentEventWriter(writer)
        if nil != serverSentEventErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported", serverSentEventErr), nil
        }

        /* the server arms its write deadline ONCE, from the request line, so a stream that outlived it lost
           the first event after it and was cut with the client none the wiser; the writer re-arms it per
           frame, budget from the frame, with the server's own write timeout as the budget. What that bounds is
           a WRITE: a client that stops reading is cut one budget after the first frame the socket can no
           longer buffer, not after the first frame it did not read — the kernel buffers absorb small frames
           at a slow cadence for a long time, so a stalled client holds its subscription and its connection
           until they fill. An absolute bound on a stream's life is the application's to declare, and this one
           declares none. */
        writeBudget := serverWriteTimeoutOf(request)
        serverSentEventWriter.WithWriteBudget(writeBudget)

        subscriber := hub.Subscribe(topic, 16)
        defer hub.Unsubscribe(subscriber)

        commentErr := serverSentEventWriter.Comment("connected")
        if nil != commentErr {
            return nil, nil
        }

        requestContext := request.HttpRequest().Context()

        /* the keepalive is what keeps an IDLE stream alive under the re-armed deadline and what finds a
           client that left without closing: a stream nothing is published on writes nothing on its own,
           and a write is the only thing that can fail on a dead connection */
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

                /* an event is a write, and a write is what the keepalive exists to guarantee: the tick is
                   counted from the last frame, so a busy stream does not also pay for keepalives it does not need */
                keepalive.Reset(keepaliveIntervalFor(writeBudget))
            }
        }
    }
}

/* defaultKeepaliveInterval is the keepalive of a stream whose server declared no write timeout, so the
   dead-client detection does not depend on the deadline existing */
const defaultKeepaliveInterval = 15 * time.Second

/* keepaliveIntervalFor is half the write budget, so two keepalives fit inside every budget and one lost to
   scheduling does not let the deadline pass; without a budget it is the default */
func keepaliveIntervalFor(writeBudget time.Duration) time.Duration {
    if 0 >= writeBudget {
        return defaultKeepaliveInterval
    }

    /* a ticker refuses a zero interval, and a budget under two seconds is a server that did not mean a stream: the floor keeps the tick a tick */
    return max(writeBudget/2, time.Second)
}

/* serverWriteTimeoutOf reads the write timeout of the server the request arrived on — net/http puts the
   server on every request's context — so the stream re-arms the very deadline the server would otherwise
   have let expire; a request without a server, a test's, has none to re-arm. */
func serverWriteTimeoutOf(request melodyhttpcontract.Request) time.Duration {
    server, isServer := request.HttpRequest().Context().Value(nethttp.ServerContextKey).(*nethttp.Server)
    if false == isServer || nil == server {
        return 0
    }

    return server.WriteTimeout
}

/* journalServerSideCut distinguishes the two ways a write fails: the client left — a broken pipe, a reset,
   the ordinary end of a stream — and the server cut the connection because the write deadline the writer
   re-arms expired with a frame in flight, which is a frame lost and worth a warning. Both used to be
   swallowed alike into a committed 200. The two are told apart on the ERROR, not on the request context:
   net/http cancels the request's context on the first write error of any kind, so by the time a Send or a
   Ping returns the context is cancelled whoever cut the connection, and a guard on it could never fire; a
   deadline reports itself as a net.Error whose Timeout is true, and nothing else does. */
func journalServerSideCut(runtimeInstance melodyruntimecontract.Runtime, topic string, frame string, writeErr error) {
    var networkErr net.Error
    if false == errors.As(writeErr, &networkErr) || false == networkErr.Timeout() {
        return
    }

    /* the journal is resolved through the application's one door with a fallback: LoggerFromRuntime files an
       emergency record and answers nil for a runtime without a logger, and the warning was then written onto
       the nil, a panic on the one line that says a frame was lost */
    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning(
        "event stream cut by the server with a frame in flight",
        melodyexception.LogContext(writeErr, melodyexceptioncontract.Context{"topic": topic, "frame": frame}),
    )
}
