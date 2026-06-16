package events

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func PublishHandler(bus melodymessagebuscontract.Bus) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        topic := queryStringOr(request, "topic", "demo")
        text := queryStringOr(request, "text", "hello")

        _, dispatchErr := bus.Dispatch(runtimeInstance, message.Notification{Topic: topic, Text: text})
        if nil != dispatchErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not publish notification"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusAccepted, map[string]any{
            "published": true,
            "topic":     topic,
        }), nil
    }
}

func queryStringOr(request melodyhttpcontract.Request, name string, fallback string) string {
    value, exists := request.Query().Get(name)
    if false == exists {
        return fallback
    }

    switch typed := value.(type) {
    case string:
        if "" == typed {
            return fallback
        }
        return typed
    case []string:
        if 0 == len(typed) || "" == typed[0] {
            return fallback
        }
        return typed[0]
    default:
        return fallback
    }
}
