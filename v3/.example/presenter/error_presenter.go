package presenter

import (
    "context"
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
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodyserializer "github.com/precision-soft/melody/v3/serializer"
    melodyvalidation "github.com/precision-soft/melody/v3/validation"
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

/* ApiErrorWithErr renders a refusal whose cause the handler holds. The cause travels in the body only under the development environment. A status of the server's own class is journaled here at error, because the kernel journals a returned failure and never a Response; a client's refusal below 500 is not journaled. */
func ApiErrorWithErr(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    statusCode int,
    publicMessage string,
    causeErr error,
) melodyhttpcontract.Response {
    normalizedErrors := normalizeErrors([]string{publicMessage})
    debugEnabled := debugMode(runtimeInstance)

    journalServerError(runtimeInstance, request, statusCode, publicMessage, causeErr)

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

/* journalServerError writes the one record a 500 answered as a Response leaves: the public message, the route and the cause. The cause is marked logged, so a reader that files marked errors once does not file it again. */
func journalServerError(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    statusCode int,
    publicMessage string,
    causeErr error,
) {
    if nethttp.StatusInternalServerError > statusCode || nil == causeErr || nil == runtimeInstance {
        return
    }

    logContext := melodyexception.LogContext(causeErr, map[string]any{
        "statusCode":    statusCode,
        "publicMessage": publicMessage,
    })

    if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().URL {
        logContext["method"] = request.HttpRequest().Method
        logContext["path"] = melodyhttp.RequestPathAsRouted(request.HttpRequest().URL.EscapedPath())
    }

    /* a client that left mid-request is not a server failure: the kernel files a returned context.Canceled as "request cancelled by client" at warning, and a 500 answered for the same cause is filed the same way */
    if true == errors.Is(causeErr, context.Canceled) && true == requestContextIsDone(request) {
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Warning("handler answered a server error to a client that left", logContext)
    } else {
        examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error("handler answered a server error", logContext)
    }

    _ = melodyexception.MarkLogged(causeErr)
}

/* requestContextIsDone answers whether the request's own context has ended, telling a client that left from a context.Canceled raised by something else. */
func requestContextIsDone(request melodyhttpcontract.Request) bool {
    if nil == request || nil == request.HttpRequest() || nil == request.HttpRequest().Context() {
        return false
    }

    return nil != request.HttpRequest().Context().Err()
}

/* ApiRefusal renders a refusal a json-binding door made before the handler ran, the decoder's or the validator's. A validation failure is rendered field by field, one errors entry per violated field, the same public collection the framework's exception listener projects. Every other refusal keeps its generic public message with the cause in the debug-gated context, because the decoder's diagnosis names internals. */
func ApiRefusal(
    runtimeInstance melodyruntimecontract.Runtime,
    request melodyhttpcontract.Request,
    statusCode int,
    publicMessage string,
    causeErr error,
) melodyhttpcontract.Response {
    fieldErrors, carriesValidation := validationErrorMessages(causeErr)
    if true == carriesValidation {
        return ApiError(runtimeInstance, request, statusCode, fieldErrors...)
    }

    return ApiErrorWithErr(runtimeInstance, request, statusCode, publicMessage, causeErr)
}

/* validationErrorMessages answers one message per violation, and whether the refusal carried a collection at all, which keeps a refusal without one on the generic-message path. Each message carries only the field and its sentence. The collection is read from the http exception's context under validationErrors, or directly as the error for a door that validates by hand; an empty collection is not a validation failure. */
func validationErrorMessages(refusalErr error) ([]string, bool) {
    validationErrors, carriesValidation := validationErrorsOf(refusalErr)
    if false == carriesValidation {
        return nil, false
    }

    fieldErrors := make([]string, 0, len(validationErrors))
    for _, validationError := range validationErrors {
        if nil == validationError {
            continue
        }

        fieldErrors = append(fieldErrors, validationError.Error())
    }

    if 0 == len(fieldErrors) {
        return nil, false
    }

    return fieldErrors, true
}

func validationErrorsOf(refusalErr error) (melodyvalidation.ValidationErrors, bool) {
    var validationErrors melodyvalidation.ValidationErrors
    if true == errors.As(refusalErr, &validationErrors) {
        return validationErrors, true
    }

    httpException := melodyexception.AsHttpException(refusalErr)
    if nil == httpException {
        return nil, false
    }

    contextValue, exists := httpException.Context()["validationErrors"]
    if false == exists {
        return nil, false
    }

    validationErrors, isCollection := contextValue.(melodyvalidation.ValidationErrors)
    if false == isCollection {
        return nil, false
    }

    return validationErrors, true
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
    payload apiResponse,
) melodyhttpcontract.Response {
    if nil == runtimeInstance {
        return fallbackJsonResponse(statusCode, payload)
    }

    acceptHeader := ""
    if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().Header {
        /* every Accept line is joined before parsing: Get answers only the first line of a repeated field, and the accept header is list-typed */
        acceptHeader = strings.Join(request.HttpRequest().Header.Values("Accept"), ", ")
    }

    serializerManager := melodyserializer.SerializerManagerFromRuntime(runtimeInstance)
    if nil != serializerManager {
        serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)

        /* a header that refuses every media type is answered 406 on the success path, as the result handler answers it, while a refusal keeps the status it earned, as the framework's error renderer does: a 401 or 404 rendered as an empty 406 tells the client nothing. The flag is read off the envelope being rendered, so the two cannot disagree. */
        if true == payload.Success && true == errors.Is(err, melodyserializer.ErrNotAcceptable) {
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

/* the debug decision is the kernel environment, as the framework exception listener reads it; when it cannot be determined the presenter emits no cause material */
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
