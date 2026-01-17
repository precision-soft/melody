package presenter

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	melodyexception "github.com/precision-soft/melody/exception"
	melodyhttp "github.com/precision-soft/melody/http"
	melodyhttpcontract "github.com/precision-soft/melody/http/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
	melodyserializer "github.com/precision-soft/melody/serializer"
)

type apiResponse struct {
	Success bool             `json:"success"`
	Payload any              `json:"payload"`
	Errors  []string         `json:"errors"`
	Context map[string]any   `json:"context,omitempty"`
	Trace   []map[string]any `json:"trace,omitempty"`
}

func ApiSuccess(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	statusCode int,
	payload any,
) melodyhttpcontract.Response {
	return buildApiResponse(
		runtimeInstance,
		request,
		statusCode,
		apiResponse{
			Success: true,
			Payload: payload,
			Errors:  []string{},
		},
	)
}

func ApiError(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	statusCode int,
	errors ...string,
) melodyhttpcontract.Response {
	normalizedErrors := normalizeErrors(errors)

	return buildApiResponse(
		runtimeInstance,
		request,
		statusCode,
		apiResponse{
			Success: false,
			Payload: nil,
			Errors:  normalizedErrors,
			Context: buildErrorContext(runtimeInstance, request, statusCode, nil),
		},
	)
}

func ApiErrorWithErr(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	statusCode int,
	publicMessage string,
	causeErr error,
) melodyhttpcontract.Response {
	normalizedErrors := normalizeErrors([]string{publicMessage})

	return buildApiResponse(
		runtimeInstance,
		request,
		statusCode,
		apiResponse{
			Success: false,
			Payload: nil,
			Errors:  normalizedErrors,
			Context: buildErrorContext(runtimeInstance, request, statusCode, causeErr),
			Trace:   buildErrorTrace(causeErr),
		},
	)
}

func HtmlError(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, statusCode int, message string) melodyhttpcontract.Response {
	_ = runtimeInstance
	_ = request

	htmlString := "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>Error</title></head><body>"
	htmlString += "<div style=\"max-width:720px;margin:40px auto;font-family:system-ui\">"
	htmlString += "<h1>Request failed</h1>"
	htmlString += "<p>" + strings.TrimSpace(message) + "</p>"
	htmlString += "<p><a href=\"/login\">Go to login</a></p>"
	htmlString += "</div></body></html>"

	return melodyhttp.HtmlResponse(statusCode, htmlString)
}

func Redirect(runtimeInstance melodyruntimecontract.Runtime, request melodyhttpcontract.Request, location string) melodyhttpcontract.Response {
	_ = runtimeInstance
	_ = request

	return melodyhttp.RedirectResponse(location, 0)
}

func normalizeErrors(errors []string) []string {
	normalizedErrors := make([]string, 0, len(errors))

	for _, errorValue := range errors {
		errorString := strings.TrimSpace(errorValue)
		if "" == errorString {
			continue
		}

		normalizedErrors = append(normalizedErrors, errorString)
	}

	if 0 == len(normalizedErrors) {
		return []string{"error"}
	}

	return normalizedErrors
}

func buildApiResponse(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	statusCode int,
	payload any,
) melodyhttpcontract.Response {
	if nil == runtimeInstance {
		return fallbackJsonResponse(statusCode, payload)
	}

	acceptHeader := ""
	if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().Header {
		acceptHeader = request.HttpRequest().Header.Get("Accept")
	}

	serializerManager := melodyserializer.SerializerManagerFromRuntime(runtimeInstance)
	if nil != serializerManager {
		serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)
		if nil == err && nil != serializerInstance {
			return serializeWith(statusCode, payload, serializerInstance)
		}
	}

	serializerInstance := melodyserializer.SerializerFromRuntime(runtimeInstance)
	if nil != serializerInstance {
		return serializeWith(statusCode, payload, serializerInstance)
	}

	return fallbackJsonResponse(statusCode, payload)
}

type serializerInstance interface {
	Serialize(value any) ([]byte, error)
	ContentType() string
}

func serializeWith(
	statusCode int,
	payload any,
	serializerInstance serializerInstance,
) melodyhttpcontract.Response {
	serializedBytes, err := serializerInstance.Serialize(payload)
	if nil != err {
		return melodyhttp.JsonErrorResponse(nethttp.StatusInternalServerError, "failed to serialize response")
	}

	response := melodyhttp.NewResponse(statusCode, serializedBytes)
	response.Headers().Set("Content-Type", serializerInstance.ContentType())

	return response
}

func fallbackJsonResponse(statusCode int, payload any) melodyhttpcontract.Response {
	response, err := melodyhttp.JsonResponse(statusCode, payload)
	if nil != err {
		return melodyhttp.JsonErrorResponse(
			nethttp.StatusInternalServerError,
			melodyexception.NewError("failed to build response", map[string]any{}, err).Error(),
		)
	}

	return response
}

func buildErrorContext(
	runtimeInstance melodyruntimecontract.Runtime,
	request melodyhttpcontract.Request,
	statusCode int,
	causeErr error,
) map[string]any {
	context := map[string]any{
		"time":       time.Now().UTC().Format(time.RFC3339Nano),
		"statusCode": statusCode,
	}

	if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().URL {
		context["method"] = request.HttpRequest().Method
		context["path"] = request.HttpRequest().URL.Path
		context["routeName"] = request.RouteName()
		context["routePattern"] = request.RoutePattern()
		context["requestId"] = request.Header(melodyhttp.HeaderRequestId)
		context["params"] = request.Params()
	}

	if nil != causeErr {
		context["error"] = map[string]any{
			"message": causeErr.Error(),
			"type":    fmt.Sprintf("%T", causeErr),
		}
	}

	return context
}

func buildErrorTrace(err error) []map[string]any {
	if nil == err {
		return nil
	}

	trace := make([]map[string]any, 0, 4)

	current := err
	for nil != current {
		trace = append(
			trace,
			map[string]any{
				"message": current.Error(),
				"type":    fmt.Sprintf("%T", current),
			},
		)

		unwrapped := errors.Unwrap(current)
		if nil == unwrapped {
			break
		}

		current = unwrapped
	}

	return trace
}
