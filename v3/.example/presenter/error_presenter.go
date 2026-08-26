package presenter

import (
    "errors"
    "fmt"
    nethttp "net/http"
    "strings"
    "time"

    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
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
            Context: buildErrorContext(request, statusCode, nil, debugMode(runtimeInstance)),
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
    debugEnabled := debugMode(runtimeInstance)

    return buildApiResponse(
        runtimeInstance,
        request,
        statusCode,
        apiResponse{
            Success: false,
            Payload: nil,
            Errors:  normalizedErrors,
            Context: buildErrorContext(request, statusCode, causeErr, debugEnabled),
            Trace:   buildErrorTrace(causeErr, debugEnabled),
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
        /* every Accept line is joined before parsing: Get answers only the first line of a repeated field, and the accept header is list-typed, so a refusal the client sent on a second line would otherwise vanish */
        acceptHeader = strings.Join(request.HttpRequest().Header.Values("Accept"), ", ")
    }

    serializerManager := melodyserializer.SerializerManagerFromRuntime(runtimeInstance)
    if nil != serializerManager {
        serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)

        /* a header that refuses every available media type is answered as not acceptable on the error path exactly as the result handler answers it on the success path: falling through would render the failure in the very representation the client rejected; the incident itself is already in the application log by the time a presenter runs */
        if true == errors.Is(err, melodyserializer.ErrNotAcceptable) {
            return melodyhttp.EmptyResponse(nethttp.StatusNotAcceptable)
        }

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

/* the debug decision is the kernel environment, exactly as the framework exception listener
reads it; when it cannot be determined the presenter stays closed and emits no cause material */
func debugMode(runtimeInstance melodyruntimecontract.Runtime) bool {
    if nil == runtimeInstance {
        return false
    }

    serviceContainer := runtimeInstance.Container()
    if nil == serviceContainer {
        return false
    }

    configuration, configurationErr := melodycontainer.FromResolver[melodyconfigcontract.Configuration](
        serviceContainer,
        melodyconfig.ServiceConfig,
    )
    if nil != configurationErr || nil == configuration {
        return false
    }

    return melodyconfig.EnvDevelopment == configuration.Kernel().Env()
}

func buildErrorContext(
    request melodyhttpcontract.Request,
    statusCode int,
    causeErr error,
    debugEnabled bool,
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

    if nil != causeErr && true == debugEnabled {
        context["error"] = map[string]any{
            "message": causeErr.Error(),
            "type":    fmt.Sprintf("%T", causeErr),
        }
    }

    return context
}

func buildErrorTrace(err error, debugEnabled bool) []map[string]any {
    if nil == err || false == debugEnabled {
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
