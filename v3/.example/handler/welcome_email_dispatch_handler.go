package handler

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* WelcomeEmailDispatchHandler hands a welcome email to the message bus and answers once the bus has accepted it: sending is slow and may fail for reasons the caller cannot act on, so the transport carries it from there. Without a broker the door answers 503 naming AMQP_DSN, since nothing in this process would read what it accepted. */
func WelcomeEmailDispatchHandler(transportConfigured bool) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        if false == transportConfigured {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusServiceUnavailable, message.ErrTransportNotConfigured.Error()), nil
        }

        bus := melodymessagebus.BusMustFromContainer(runtimeInstance.Container())

        _, dispatchErr := bus.Dispatch(runtimeInstance, message.WelcomeEmail{UserId: 1, Address: "new-user@example.com"})
        if nil != dispatchErr {
            return nil, dispatchErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusAccepted, map[string]string{"status": "dispatched"})
    }
}
