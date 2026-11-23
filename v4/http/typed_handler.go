package http

import (
    nethttp "net/http"
    "reflect"
    "runtime/debug"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
)

/* JsonHandlerRefusal is one refusal JsonHandler makes before the handler runs; fields may be added. */
type JsonHandlerRefusal struct {
    /* Status is the http status the framework would answer. */
    Status int

    /* Message is the message the framework would answer. */
    Message string

    /* Cause is the failure itself, carrying the decoder's diagnosis and the validation collection under the validationErrors key. */
    Cause error
}

/* JsonHandlerErrorResponder renders the refusals JsonHandler makes before the handler runs, handed the refusal. A responder that answers no response leaves the refusal to the framework. */
type JsonHandlerErrorResponder func(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    refusal JsonHandlerRefusal,
) (httpcontract.Response, error)

/* JsonHandlerConfig is what a json handler declares beyond the function it binds the body for; the zero value renders every refusal through the framework. */
type JsonHandlerConfig struct {
    /* ErrorResponder renders the refusals made before the handler runs, handed the refusal; nil leaves them to the framework. */
    ErrorResponder JsonHandlerErrorResponder
}

/* JsonHandler binds the request body into RequestBody, validates it and calls handle, reading the body through the door Request.BindJson uses: the configured limit with its 413, the decoder's diagnosis as the refusal's cause, an empty or null body refused. A nil handle is refused at construction. */
func JsonHandler[RequestBody any](
    handle func(runtimeInstance containercontract.Runtime, request httpcontract.Request, body RequestBody) (httpcontract.Response, error),
    config JsonHandlerConfig,
) httpcontract.Handler {
    if nil == handle {
        exception.Panic(
            exception.NewError("json handler may not be nil", nil, nil),
        )
    }

    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var body RequestBody

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

/* HandleJson registers JsonHandler's handler for the pattern and declares RequestBody as the route's request type, so the boot verifies its validate tags; options declaring another request type are refused. */
func HandleJson[RequestBody any](
    routeHandler httpcontract.RouteHandler,
    pattern string,
    handle func(runtimeInstance containercontract.Runtime, request httpcontract.Request, body RequestBody) (httpcontract.Response, error),
    options httpcontract.RouteOptions,
    config JsonHandlerConfig,
) {
    boundType := reflect.TypeFor[RequestBody]()

    if nil != options.RequestType && boundType != options.RequestType {
        exception.Panic(
            exception.NewError(
                "the route options declare another request type than the handler binds",
                exceptioncontract.Context{
                    "pattern":  pattern,
                    "declared": options.RequestType.String(),
                    "bound":    boundType.String(),
                },
                nil,
            ),
        )
    }

    handler := JsonHandler(handle, config)

    options.RequestType = boundType

    routeHandler.Handle(pattern, handler, options)
}

/* boundBodyIsNil reads every kind a json null can leave nil, reflecting over the typed value; an invalid Value, RequestBody as an unset `any`, reads as nil. */
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
    runtimeInstance containercontract.Runtime,
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
        JsonHandlerRefusal{Status: status, Message: message, Cause: refusalErr},
    )
    if nil != responderErr {
        return nil, responderErr
    }

    if true == internal.IsNilInterface(response) {
        return nil, refusalErr
    }

    return response, nil
}

/* invokeJsonHandlerErrorResponderSafely runs the responder under the kernel's containment, as the kernel's final step does: a panic is recorded with the stack of the recovering site, and the refusal the responder was asked to render stands. */
func invokeJsonHandlerErrorResponderSafely(
    responder JsonHandlerErrorResponder,
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    refusal JsonHandlerRefusal,
) (response httpcontract.Response, err error) {
    cause := refusal.Cause

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
                exception.LogContextConfig{
                    Extra: exceptioncontract.Context{
                        "panicStack": string(debug.Stack()),
                    },
                },
            ),
        )
    }()

    return responder(runtimeInstance, request, refusal)
}
