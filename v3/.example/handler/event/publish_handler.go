package event

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func PublishHandler(bus melodymessagebuscontract.Bus) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        topic := queryStringOr(request, "topic", CatalogTopic)
        text := queryStringOr(request, "text", "hello")

        _, dispatchErr := bus.Dispatch(runtimeInstance, message.Notification{Topic: topic, Text: text})
        if nil != dispatchErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not publish notification", dispatchErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusAccepted, map[string]any{
            "published": true,
            "topic":     topic,
        }), nil
    }
}

/* queryStringOr falls back on a present but empty value too, where StringOrDefault answers the empty string. */
func queryStringOr(request melodyhttpcontract.Request, name string, fallback string) string {
    value := melodybag.StringOrDefault(request.Query(), name, fallback)
    if "" == value {
        return fallback
    }

    return value
}

