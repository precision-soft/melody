package http

import (
    "errors"
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/serializer"
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

    /* a value implementing the Response contract is served with its own status and headers, not serialized; a typed nil is read through the interface */
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
            /* every Accept line is joined before parsing, since the header is list-typed */
            acceptHeader := joinedAcceptHeader(request)

            serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)

            /* a header refusing every available media type is answered not acceptable; one that matches nothing falls through to the default representation */
            if true == errors.Is(err, serializer.ErrNotAcceptable) {
                return EmptyResponse(nethttp.StatusNotAcceptable), nil
            }

            /* any other resolution failure is recorded before the fallback serves the default representation */
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
