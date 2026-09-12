package event

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
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
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported"), nil
        }

        subscriber := hub.Subscribe(topic, 16)
        defer hub.Unsubscribe(subscriber)

        commentErr := serverSentEventWriter.Comment("connected")
        if nil != commentErr {
            return nil, nil
        }

        requestContext := request.HttpRequest().Context()

        for {
            select {
            case <-requestContext.Done():
                return nil, nil
            case event, open := <-subscriber.Events():
                if false == open {
                    return nil, nil
                }

                if sendErr := serverSentEventWriter.Send(event); nil != sendErr {
                    return nil, nil
                }
            }
        }
    }
}
