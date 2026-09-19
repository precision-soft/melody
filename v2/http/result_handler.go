package http

import (
    "errors"
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/serializer"
)

type ResultHandler func(
    runtimeInstance runtimecontract.Runtime,
    writer nethttp.ResponseWriter,
    request httpcontract.Request,
) (any, error)

func WrapResultHandler(resultHandler ResultHandler) httpcontract.Handler {
    return func(
        runtimeInstance runtimecontract.Runtime,
        writer nethttp.ResponseWriter,
        request httpcontract.Request,
    ) (httpcontract.Response, error) {
        value, err := resultHandler(runtimeInstance, writer, request)
        if nil != err {
            return nil, err
        }

        return NormalizeResultToResponse(runtimeInstance, request, value)
    }
}

func NormalizeResultToResponse(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    value any,
) (httpcontract.Response, error) {
    if nil == value {
        return nil, nil
    }

    /* the assertion is against the contract, the same question the controller registration door asks: a caller's own Response implementation is served with its status and headers rather than being handed to the serializer, which rendered it as a value — all unexported fields, so an empty body under a 200 that replaced the status the caller chose. The typed nil is read through the interface, where a bare comparison would take it for a live response. */
    responseInstance, ok := value.(httpcontract.Response)
    if true == ok {
        if true == internal.IsNilInterface(responseInstance) {
            return nil, nil
        }

        return responseInstance, nil
    }

    stringValue, ok := value.(string)
    if true == ok {
        return TextResponse(nethttp.StatusOK, stringValue), nil
    }

    bytesValue, ok := value.([]byte)
    if true == ok {
        return NewResponse(nethttp.StatusOK, bytesValue), nil
    }

    if nil != runtimeInstance {
        serializerManager := serializer.SerializerManagerFromRuntime(runtimeInstance)

        if nil != serializerManager {
            /* every Accept line is joined before parsing: Get answers only the first line of a repeated field, and the accept header is list-typed, so a refusal the client sent on a second line would otherwise vanish */
            acceptHeader := joinedAcceptHeader(request)

            serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)

            /* a header that refuses every available media type is answered as not acceptable rather than served the very type it rejected; a header that simply matches nothing still falls through to the default representation */
            if true == errors.Is(err, serializer.ErrNotAcceptable) {
                return EmptyResponse(nethttp.StatusNotAcceptable), nil
            }

            /* a resolution failure that is not the not-acceptable refusal is recorded before the fallback serves the default representation: it was dropped whole, so a client that named an available type and received another had no diagnostic anywhere */
            if nil != err {
                loggerInstance := logging.LoggerFromRuntime(runtimeInstance)
                if nil != loggerInstance {
                    loggerInstance.Warning(
                        "serializer resolution failed, serving the default representation",
                        exception.LogContext(
                            err,
                            exceptioncontract.Context{
                                "acceptHeader": acceptHeader,
                            },
                        ),
                    )
                }
            }

            if nil == err && nil != serializerInstance {
                serializedBytes, err := serializerInstance.Serialize(value)
                if nil != err {
                    return nil, exception.NewError("failed to serialize controller result", map[string]any{}, err)
                }

                response := NewResponse(nethttp.StatusOK, serializedBytes)
                if nil == response.headers {
                    response.headers = make(nethttp.Header)
                }
                response.headers.Set("Content-Type", serializerInstance.ContentType())

                return response, nil
            }
        }

        serializerInstance := serializer.SerializerFromRuntime(runtimeInstance)
        if nil != serializerInstance {
            serializedBytes, err := serializerInstance.Serialize(value)
            if nil != err {
                return nil, exception.NewError("failed to serialize controller result", map[string]any{}, err)
            }

            response := NewResponse(nethttp.StatusOK, serializedBytes)
            if nil == response.headers {
                response.headers = make(nethttp.Header)
            }
            response.headers.Set("Content-Type", serializerInstance.ContentType())

            return response, nil
        }
    }

    response, jsonResponseErr := JsonResponse(nethttp.StatusOK, value)
    if nil != jsonResponseErr {
        return nil, exception.NewError("failed to normalize controller result", map[string]any{}, jsonResponseErr)
    }

    return response, nil
}
