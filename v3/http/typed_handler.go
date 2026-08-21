package http

import (
    nethttp "net/http"
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* JsonHandlerErrorResponder renders the refusals JsonHandler makes before the handler runs. It is
   handed the failure itself and not only a status and a message: the cause carries the decoder's own
   diagnosis and the validation collection under the validationErrors context key, so a responder can
   render the framework's own envelope, or its own shape, without the detail having been destroyed on
   the way to it. A responder that answers no response leaves the refusal to the framework — it can
   never turn a refused request into a success. */
type JsonHandlerErrorResponder func(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    status int,
    message string,
    cause error,
) (httpcontract.Response, error)

type JsonHandlerOption func(*jsonHandlerOptions)

type jsonHandlerOptions struct {
    errorResponder JsonHandlerErrorResponder
}

func WithJsonHandlerErrorResponder(responder JsonHandlerErrorResponder) JsonHandlerOption {
    if nil == responder {
        exception.Panic(
            exception.NewError("json handler error responder may not be nil", nil, nil),
        )
    }

    return func(options *jsonHandlerOptions) {
        options.errorResponder = responder
    }
}

/* JsonHandler binds the request body into Req, validates it and calls handle. It reads the body
   through the same door Request.BindJson is: the configured body limit with its 413, the decoder's
   diagnosis kept as the refusal's cause, the empty and nil bodies refused by name — a door of its
   own drifted from all three, answering an oversized upload as malformed json and filing a refusal
   the operator could not read.

   A nil handle is refused at construction rather than at the first request that passes validation:
   the route registered clean, the manifest listed it and the health check was green while every
   valid request answered 500. */
func JsonHandler[Req any](
    handle func(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, body Req) (httpcontract.Response, error),
    options ...JsonHandlerOption,
) httpcontract.Handler {
    if nil == handle {
        exception.Panic(
            exception.NewError("json handler may not be nil", nil, nil),
        )
    }

    settings := &jsonHandlerOptions{}
    for index, option := range options {
        if nil == option {
            exception.Panic(
                exception.NewError(
                    "json handler option may not be nil",
                    map[string]any{
                        "index": index,
                    },
                    nil,
                ),
            )
        }

        option(settings)
    }

    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        var body Req

        if bindErr := bindJsonBody(request, &body); nil != bindErr {
            return jsonHandlerError(settings, runtimeInstance, request, bindErr)
        }

        /* a literal `null` body decodes without error and leaves the bound value nil, which the validator reports valid (it has nothing to walk) and the handler then dereferences. Every nilable kind is read, not the pointer alone: a bulk endpoint typed on a slice, or a handler typed on a map, took the same `null` past a guard that only asked about pointers. */
        if true == boundBodyIsNil(body) {
            return jsonHandlerError(
                settings,
                runtimeInstance,
                request,
                exception.NewHttpException(nethttp.StatusBadRequest, "empty request body"),
            )
        }

        if validationErr := validateBoundBody(runtimeInstance, &body); nil != validationErr {
            return jsonHandlerError(settings, runtimeInstance, request, validationErr)
        }

        return handle(runtimeInstance, request, body)
    }
}

/* boundBodyIsNil reads every kind a json null can leave nil. internal.IsNilInterface answers the same
   question for a value already boxed in an interface; this one is handed the typed value itself, so
   it reflects over Req directly and reads an invalid Value — Req instantiated as `any` and left
   unset — as the nil it means. */
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

/* jsonHandlerError renders a pre-handler refusal. The application's responder runs under the kernel's
   own containment discipline — third-party code invoked from framework internals runs under a guard,
   because a panic here lands inside the failure path itself — and a responder that hands back no
   response leaves the original refusal standing: returned as it was, the kernel read the nil pair as
   a handler that answered nothing and served an empty 204, so a refused write reported success to
   its client with no record filed anywhere. */
func jsonHandlerError(
    settings *jsonHandlerOptions,
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    refusalErr error,
) (httpcontract.Response, error) {
    if nil == settings.errorResponder {
        return nil, refusalErr
    }

    status := nethttp.StatusBadRequest
    message := "bad request"
    if httpException := exception.AsHttpException(refusalErr); nil != httpException {
        status = httpException.StatusCode()
        message = httpException.Message()
    }

    response, responderErr := invokeJsonHandlerErrorResponderSafely(
        settings.errorResponder,
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
        err = RecoverToError(recoveredValue)
        if nil == err {
            err = cause
        }
    }()

    return responder(runtimeInstance, request, status, message, cause)
}
