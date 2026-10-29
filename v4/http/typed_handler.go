package http

import (
    nethttp "net/http"
    "reflect"
    "runtime/debug"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* JsonHandlerErrorResponder renders the refusals JsonHandler makes before the handler runs. It is handed the failure itself beside the status and the message, whose cause carries the decoder's diagnosis and the validation collection under the validationErrors key. A responder that answers no response leaves the refusal to the framework. */
type JsonHandlerErrorResponder func(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    status int,
    message string,
    cause error,
) (httpcontract.Response, error)

/* JsonHandlerConfig is what a json handler declares beyond the function it binds the body for; the zero value renders every refusal through the framework. */
type JsonHandlerConfig struct {
    /* ErrorResponder renders the refusals made before the handler runs, handed the failure beside the status and the message; nil leaves them to the framework. */
    ErrorResponder JsonHandlerErrorResponder
}

/* JsonHandler binds the request body into Req, validates it and calls handle, reading the body through the door Request.BindJson uses: the configured limit with its 413, the decoder's diagnosis as the refusal's cause, an empty or null body refused. A nil handle is refused at construction. */
func JsonHandler[Req any](
    handle func(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, body Req) (httpcontract.Response, error),
    config JsonHandlerConfig,
) httpcontract.Handler {
    if nil == handle {
        exception.Panic(
            exception.NewError("json handler may not be nil", nil, nil),
        )
    }

    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var body Req

        if bindErr := bindJsonBody(request, &body); nil != bindErr {
            return jsonHandlerError(config, runtimeInstance, request, bindErr)
        }

        /* a literal null leaves the bound value nil and passes validation, so every nilable kind is refused here, not the pointer alone */
        if true == boundBodyIsNil(body) {
            return jsonHandlerError(
                config,
                runtimeInstance,
                request,
                exception.NewHttpException(nethttp.StatusBadRequest, "empty request body"),
            )
        }

        if validationErr := validateBoundBody(runtimeInstance, &body); nil != validationErr {
            return jsonHandlerError(config, runtimeInstance, request, validationErr)
        }

        return handle(runtimeInstance, request, body)
    }
}

/* boundBodyIsNil reads every kind a json null can leave nil, reflecting over the typed value; an invalid Value, Req as an unset `any`, reads as nil. */
func boundBodyIsNil(body any) bool {
    bodyValue := reflect.ValueOf(body)

    switch bodyValue.Kind() {
    case reflect.Invalid:
        return true
    case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
        return bodyValue.IsNil()
    }

    return false
}

/* jsonHandlerError renders a pre-handler refusal through the application's responder, under a guard. A responder that answers no response, or panics, leaves the original refusal standing. */
func jsonHandlerError(
    config JsonHandlerConfig,
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    refusalErr error,
) (httpcontract.Response, error) {
    if nil == config.ErrorResponder {
        return nil, refusalErr
    }

    status := nethttp.StatusBadRequest
    message := "bad request"
    if httpException := exception.AsHttpException(refusalErr); nil != httpException {
        status = httpException.StatusCode()
        message = httpException.Message()
    }

    response, responderErr := invokeJsonHandlerErrorResponderSafely(
        config.ErrorResponder,
        runtimeInstance,
        request,
        status,
        message,
        refusalErr,
    )
    if nil != responderErr {
        return nil, responderErr
    }

    if true == internal.IsNilInterface(response) {
        return nil, refusalErr
    }

    return response, nil
}

/* invokeJsonHandlerErrorResponderSafely runs the responder under the kernel's containment, as invokeErrorHandlerSafely does: a panic is recorded with the stack of the recovering site, and the refusal the responder was asked to render stands. */
func invokeJsonHandlerErrorResponderSafely(
    responder JsonHandlerErrorResponder,
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    status int,
    message string,
    cause error,
) (response httpcontract.Response, err error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        response = nil
        err = cause

        loggerInstance := logging.LoggerFromRuntime(runtimeInstance)
        if nil == loggerInstance {
            return
        }

        loggerInstance.Error(
            "json handler error responder panicked",
            exception.LogContext(
                RecoverToError(recoveredValue),
                exceptioncontract.Context{
                    "panicStack": string(debug.Stack()),
                },
            ),
        )
    }()

    return responder(runtimeInstance, request, status, message, cause)
}
