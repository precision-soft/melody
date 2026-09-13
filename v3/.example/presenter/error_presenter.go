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
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

/* ApiErrorWithErr renders a refusal whose cause the handler holds. The cause travels in the body only under
   the development environment; for a status of the server's own class it is JOURNALED here as well, at
   error, because a Response is the one thing the kernel never journals: it journals a handler's failure
   when the failure is RETURNED, and a handler that answered the failure as a 500 reached the terminate
   listener alone — one info line, "request completed 500", no cause — so outside development the reason a
   door answered 500 existed nowhere. A client's own refusal, below 500, is not journaled: the request was
   wrong, and the body says so. */
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

/* journalServerError writes the one record a 500 answered as a Response leaves: the public message the
   client read, the route and the cause, through the runtime's logger. The cause is marked logged so a
   reader further up that files marked errors once does not file it again. */
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

    context := melodyexception.LogContext(causeErr, map[string]any{
        "statusCode":    statusCode,
        "publicMessage": publicMessage,
    })

    if nil != request && nil != request.HttpRequest() && nil != request.HttpRequest().URL {
        context["method"] = request.HttpRequest().Method
        context["path"] = melodyhttp.RequestPathAsRouted(request.HttpRequest().URL.EscapedPath())
    }

    serverErrorLoggerOf(runtimeInstance).Error("handler answered a server error", context)

    _ = melodyexception.MarkLogged(causeErr)
}

/* serverErrorLoggerOf is the application's logger, and the emergency logger when the container holds none:
   the reason a door answered 500 has to reach SOME journal, and a process whose logger is not registered
   is exactly the process whose operator is reading standard error. */
func serverErrorLoggerOf(runtimeInstance melodyruntimecontract.Runtime) melodyloggingcontract.Logger {
    if nil == runtimeInstance.Container() {
        return melodylogging.EmergencyLogger()
    }

    logger, resolveErr := melodycontainer.FromResolver[melodyloggingcontract.Logger](
        runtimeInstance.Container(),
        melodylogging.ServiceLogger,
    )
    if nil != resolveErr || nil == logger {
        return melodylogging.EmergencyLogger()
    }

    return logger
}

/* ApiRefusal renders a refusal a json-binding door made before the handler ran — the decoder's and the validator's alike, since JsonHandler hands both to the same responder.

   A validation failure is rendered field by field, one errors entry per violated field, so an api client can attach each message to the input that earned it instead of splitting a joined string: that collection is public by contract, and the framework's own exception listener projects the very same key into the body for every door that does not install a responder. Every other refusal keeps its generic public message, with the cause in the debug-gated context, because the decoder's diagnosis names internals — a byte offset into a body, a Go type — and this door is reachable by anyone the route lets through. */
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

/* validationErrorMessages answers one message per violation, and whether the refusal carried a collection at all — the second answer is what keeps a refusal that carries none on the generic-message path instead of publishing its cause.

   Each message carries only the field and its sentence: a violation's context, which may name the declaration rather than the input, never reaches the client on this path. The collection travels in the http exception's context under validationErrors, the key BindJsonAndValidate attaches it to and the kernel's exception listener reads; a collection handed directly as the error is read too, so a door that validates by hand renders the same way. An empty collection is not a validation failure: it would render an errors list with nothing in it, where the generic message at least names what was refused. */
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
        /* every Accept line is joined before parsing: Get answers only the first line of a repeated field, and the accept header is list-typed, so a refusal the client sent on a second line would otherwise vanish */
        acceptHeader = strings.Join(request.HttpRequest().Header.Values("Accept"), ", ")
    }

    serializerManager := melodyserializer.SerializerManagerFromRuntime(runtimeInstance)
    if nil != serializerManager {
        serializerInstance, err := serializerManager.ResolveByAcceptHeader(acceptHeader)

        /* a header that refuses every available media type is answered as not acceptable on the SUCCESS path, exactly as the result handler answers it; a REFUSAL keeps the status it earned instead, which is the asymmetry the framework's own error renderer states and the reason it falls back for every resolution failure alike: a 401 or a 404 rendered as an empty 406 tells the client nothing about why it was turned away, and the only thing negotiation could have withheld is a representation it had already rejected. The flag is read off the envelope being rendered rather than passed beside it, so the two can never disagree about which path this is. */
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

/* the debug decision is the kernel environment, exactly as the framework exception listener reads it; when it cannot be determined the presenter stays closed and emits no cause material */
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
