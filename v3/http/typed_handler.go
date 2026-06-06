package http

import (
    "encoding/json"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/validation"
)

type JsonHandlerErrorResponder func(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    status int,
    message string,
) (httpcontract.Response, error)

type JsonHandlerOption func(*jsonHandlerOptions)

type jsonHandlerOptions struct {
    errorResponder JsonHandlerErrorResponder
}

func WithJsonHandlerErrorResponder(responder JsonHandlerErrorResponder) JsonHandlerOption {
    return func(options *jsonHandlerOptions) {
        options.errorResponder = responder
    }
}

func JsonHandler[Req any](
    handle func(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, body Req) (httpcontract.Response, error),
    options ...JsonHandlerOption,
) httpcontract.Handler {
    settings := &jsonHandlerOptions{}
    for _, option := range options {
        option(settings)
    }

    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var body Req

        decodeErr := json.NewDecoder(request.HttpRequest().Body).Decode(&body)
        if nil != decodeErr {
            return jsonHandlerError(settings, runtimeInstance, request, nethttp.StatusBadRequest, "invalid json")
        }

        validatorInstance := validation.ValidatorMustFromContainer(runtimeInstance.Container())

        validationErr := validatorInstance.Validate(body)
        if nil != validationErr {
            return jsonHandlerError(settings, runtimeInstance, request, nethttp.StatusBadRequest, validationErr.Error())
        }

        return handle(runtimeInstance, request, body)
    }
}

func jsonHandlerError(
    settings *jsonHandlerOptions,
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    status int,
    message string,
) (httpcontract.Response, error) {
    if nil != settings.errorResponder {
        return settings.errorResponder(runtimeInstance, request, status, message)
    }

    return nil, exception.NewHttpException(status, message)
}
