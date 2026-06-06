package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/message"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func MessageBusDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        bus := melodymessagebus.BusMustFromContainer(runtimeInstance.Container())

        _, dispatchErr := bus.Dispatch(runtimeInstance, message.WelcomeEmail{UserId: 1, Address: "demo@example.com"})
        if nil != dispatchErr {
            return nil, dispatchErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusAccepted, map[string]string{"status": "dispatched"})
    }
}
