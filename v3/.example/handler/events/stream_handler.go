package events

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func StreamHandler(hub *melodyhttp.SseHub) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        sseWriter, sseErr := melodyhttp.NewSseWriter(writer)
        if nil != sseErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "streaming is not supported"), nil
        }

        topic := queryStringOr(request, "topic", "demo")

        subscriber := hub.Subscribe(topic, 16)
        defer hub.Unsubscribe(subscriber)

        commentErr := sseWriter.Comment("connected")
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

                if sendErr := sseWriter.Send(event); nil != sendErr {
                    return nil, nil
                }
            }
        }
    }
}
