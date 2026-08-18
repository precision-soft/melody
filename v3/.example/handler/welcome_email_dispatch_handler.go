package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/message"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* WelcomeEmailDispatchHandler hands a welcome email to the message bus and answers as soon as the bus has taken it, rather than after it has been sent.

That is the whole point of the bus for this kind of work: sending mail is slow and may fail for reasons the caller cannot act on, so the request ends when the message is accepted and the transport carries it from there. */
func WelcomeEmailDispatchHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        bus := melodymessagebus.BusMustFromContainer(runtimeInstance.Container())

        _, dispatchErr := bus.Dispatch(runtimeInstance, message.WelcomeEmail{UserId: 1, Address: "new-user@example.com"})
        if nil != dispatchErr {
            return nil, dispatchErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusAccepted, map[string]string{"status": "dispatched"})
    }
}
