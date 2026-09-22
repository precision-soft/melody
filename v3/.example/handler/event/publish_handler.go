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

/* queryStringOr differs from StringOrDefault in one place, which is why it
stays: a present but EMPTY value falls back here, where the door answers the
empty string it was given. */
func queryStringOr(request melodyhttpcontract.Request, name string, fallback string) string {
    value := melodybag.StringOrDefault(request.Query(), name, fallback)
    if "" == value {
        return fallback
    }

    return value
}

