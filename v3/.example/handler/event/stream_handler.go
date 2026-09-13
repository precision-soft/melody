package event

import (
    "context"
    nethttp "net/http"
    "time"

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

        serverSentEventWriter, serverSentEventErr := melodyhttp.NewServerSentEventWriter(writer)
        if nil != serverSentEventErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported", serverSentEventErr), nil
        }

        /* the server arms its write deadline ONCE, from the request line, so a stream that outlived it lost
           the first event after it and was cut with the client none the wiser; the writer re-arms it per
           frame, budget from the frame, with the server's own write timeout as the budget — a client that
           stops reading is still cut, one budget after the last frame it did not take */
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
                    journalServerSideCut(runtimeInstance, requestContext, topic, "keepalive", pingErr)

                    return nil, nil
                }
            case event, open := <-subscriber.Events():
                if false == open {
                    return nil, nil
                }

                if sendErr := serverSentEventWriter.Send(event); nil != sendErr {
                    journalServerSideCut(runtimeInstance, requestContext, topic, "event", sendErr)

                    return nil, nil
                }
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

    return writeBudget / 2
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

/* journalServerSideCut distinguishes the two ways a write fails: the client left, which the request context
   says and which is the ordinary end of a stream, and the server cut the connection with a frame in flight
   — a deadline, a broken pipe on a client still there — which is a frame lost and worth a warning. Both
   used to be swallowed alike into a committed 200. */
func journalServerSideCut(runtimeInstance melodyruntimecontract.Runtime, requestContext context.Context, topic string, frame string, writeErr error) {
    if nil != requestContext.Err() {
        return
    }

    melodylogging.LoggerFromRuntime(runtimeInstance).Warning(
        "event stream cut by the server with a frame in flight",
        melodyexception.LogContext(writeErr, melodyexceptioncontract.Context{"topic": topic, "frame": frame}),
    )
}
