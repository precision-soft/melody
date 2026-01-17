package http

import (
	nethttp "net/http"

	"github.com/precision-soft/melody/exception"
	httpcontract "github.com/precision-soft/melody/http/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
	"github.com/precision-soft/melody/serializer"
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

	responseInstance, ok := value.(*Response)
	if true == ok {
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
			acceptHeader := ""
			if nil != request.HttpRequest() && nil != request.HttpRequest().Header {
				acceptHeader = request.HttpRequest().Header.Get("Accept")
			}

			serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)
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
