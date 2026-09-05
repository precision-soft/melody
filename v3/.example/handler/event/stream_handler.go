package event

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func StreamHandler(hub *melodyhttp.ServerSentEventHub) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        serverSentEventWriter, serverSentEventErr := melodyhttp.NewServerSentEventWriter(writer)
        if nil != serverSentEventErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported"), nil
        }

        topic := queryStringOr(request, "topic", CatalogTopic)

        /* the topic is the client's to name, so being allowed to open a stream is not being allowed to read the one asked for: the catalog topic carries the product and user writes made behind RoleEditor, and an authenticated reader without that role used to watch them go by. */
        if false == topicIsReadableBy(runtimeInstance, topic) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusForbidden, "not allowed to subscribe to this topic"), nil
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
