package http

import (
    "fmt"
    "math"
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/validation"
)

/* BuiltInExceptionListenerPriority is the floor of kernel.exception: the kernel's built-in listener answers there, and a listener registered at it once the kernel serves is refused, since it could never run. */
const BuiltInExceptionListenerPriority = math.MinInt

/* installBuiltInExceptionListener registers the default answer of kernel.exception on the floor of the event. A dispatcher without the floor door cannot keep the listener once per kernel nor refuse a listener at the floor that would never run, so it is refused before any listener is added. */
func installBuiltInExceptionListener(eventDispatcher eventcontract.EventDispatcher) {
    registrar, ok := eventDispatcher.(eventcontract.PriorityFloorRegistrar)
    if false == ok {
        exception.Panic(
            exception.NewError(
                "the event dispatcher cannot reserve the priority of the built-in exception listener",
                exceptioncontract.Context{"dispatcherType": fmt.Sprintf("%T", eventDispatcher)},
                nil,
            ),
        )
    }

    registrar.InstallFloorListener(kernelcontract.EventKernelException, builtInExceptionListener, BuiltInExceptionListenerPriority)
}

/* builtInExceptionListener journals an error no producer filed and answers with RenderExceptionResponse. Debug mode is read per dispatch from the runtime's configuration; a runtime without one renders the production body. */
func builtInExceptionListener(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
    exceptionEvent, ok := eventValue.Payload().(*KernelExceptionEvent)
    if false == ok {
        return nil
    }

    if nil == exceptionEvent {
        return nil
    }

    if nil != exceptionEvent.Response() {
        return nil
    }

    if nil == exceptionEvent.Err() {
        return nil
    }

    if nil != runtimeInstance {
        if loggerInstance := logging.LoggerFromRuntime(runtimeInstance); nil != loggerInstance {
            exceptionEvent.err = recordExceptionOccurrence(loggerInstance, exceptionEvent.Request(), exceptionEvent.Err())
        }
    }

    exceptionEvent.SetResponse(
        RenderExceptionResponse(
            runtimeInstance,
            exceptionEvent.Request(),
            exceptionEvent.Err(),
            ExceptionRenderConfig{
                DebugMode:             exceptionListenerDebugMode(runtimeInstance),
                UnavailableRetryAfter: bound.Of(exceptionEvent.UnavailableRetryAfter()),
            },
        ),
    )

    return nil
}

/* recordExceptionOccurrence files the one record of an error and answers the error to carry on: an error already logged is not filed again, the request coordinates are attached to its occurrence instead, and a new one is marked on a carrier of this occurrence, never on the error value, which another request can share. A deliberate 4xx is a warning. Through the kernel every producer logs before it dispatches; the built-in listener files what a dispatch made by hand left unfiled. */
func recordExceptionOccurrence(loggerInstance loggingcontract.Logger, request httpcontract.Request, err error) error {
    return recordErrorOccurrence(loggerInstance, request, err, "unhandled exception")
}

func recordErrorOccurrence(loggerInstance loggingcontract.Logger, request httpcontract.Request, err error, message string) error {
    return recordOccurrence(loggerInstance, request, err, message, occurrenceRecordConfig{})
}

/* occurrenceRecordConfig carries what a record adds to the default: Warning files it at Warning whatever the error, Extra adds context */
type occurrenceRecordConfig struct {
    Warning bool
    Extra   exceptioncontract.Context
}

func recordOccurrence(loggerInstance loggingcontract.Logger, request httpcontract.Request, err error, message string, recordConfig occurrenceRecordConfig) error {
    requestId := ""
    path := ""
    method := ""

    if false == internal.IsNilInterface(request) && nil != request.RequestContext() {
        requestId = request.RequestContext().RequestId()
    }

    if false == internal.IsNilInterface(request) && nil != request.HttpRequest() {
        method = internal.BoundDiagnosticText(request.HttpRequest().Method)
        if nil != request.HttpRequest().URL {
            path = internal.BoundDiagnosticText(request.HttpRequest().URL.Path)
        }
    }

    if true == exception.IsAlreadyLogged(err) {
        attachRequestContextToError(err, requestId, method, path)

        return err
    }

    occurrence := newLoggedOccurrence(err)
    attachRequestContextToError(occurrence, requestId, method, path)

    extra := exceptioncontract.Context{
        "requestId": requestId,
        "method":    method,
        "path":      path,
    }
    for key, value := range recordConfig.Extra {
        extra[key] = value
    }

    recordContext := exception.LogContext(
        occurrence,
        exception.LogContextConfig{
            Extra: extra,
        },
    )

    if true == recordConfig.Warning || true == isDeliberateClientFault(err) {
        loggerInstance.Warning(message, recordContext)
    } else {
        loggerInstance.Error(message, recordContext)
    }

    return occurrence
}

func exceptionListenerDebugMode(runtimeInstance runtimecontract.Runtime) bool {
    if true == internal.IsNilInterface(runtimeInstance) {
        return false
    }

    configuration, configurationErr := config.ConfigFromContainer(runtimeInstance.Context(), runtimeInstance.Container())
    if nil != configurationErr {
        return false
    }

    return config.EnvDevelopment == configuration.Kernel().Env()
}

type ExceptionRenderConfig struct {
    /* DebugMode adds the context and cause of the nearest melody error, and shows a plain error's own text. */
    DebugMode bool

    /* UnavailableRetryAfter is the Retry-After of the 503 a marked infrastructure failure earns: 5s when left out, 1s to 1h in whole seconds, unlimited refused, a refused value a panic naming the field. */
    UnavailableRetryAfter bound.Duration
}

/* RenderExceptionResponse renders the answer an error earns: an error exception.AnswersUnavailable reads as unavailable is 503 service unavailable with Retry-After, whatever status lies beneath the mark; otherwise the status and message of the http exception its chain carries, its client-facing code, key and parameters, its validation errors projected for the client, and in debug mode the context and cause of the nearest melody error; any other error is a 500. The built-in listener answers with it and the kernel's final step renders with it; an application listener that wants the framework body for some errors calls it. */
func RenderExceptionResponse(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, err error, renderConfig ExceptionRenderConfig) httpcontract.Response {
    debugMode := renderConfig.DebugMode

    /* resolved for every error, so a refused delay meets its caller at the first error rendered rather than at the first outage */
    unavailableRetryAfter, retryAfterErr := config.ResolveUnavailableRetryAfter(renderConfig.UnavailableRetryAfter, "ExceptionRenderConfig.UnavailableRetryAfter")
    if nil != retryAfterErr {
        exception.Panic(exception.FromError(retryAfterErr))
    }

    httpException := exception.AsHttpException(err)

    statusCode := nethttp.StatusInternalServerError
    message := "internal server error"
    retryAfter := httpException.RetryAfter()
    unavailable := exception.AnswersUnavailable(err)

    if true == unavailable {
        /* a status beneath the mark is a store's, never the caller's answer */
        httpException = nil
        statusCode = nethttp.StatusServiceUnavailable
        message = "service unavailable"
        retryAfter = unavailableRetryAfter
    } else if nil != httpException {
        statusCode = httpException.StatusCode()
        message = httpException.Message()
    } else if true == debugMode {
        message = debugErrorMessage(err)
    }

    payloadExtras := map[string]any{}

    if nil != httpException {
        if code := httpException.Code(); "" != code {
            payloadExtras[errorResponseKeyCode] = code
        }

        if key := httpException.Key(); "" != key {
            payloadExtras[errorResponseKeyKey] = key
        }

        if parameters := httpException.Parameters(); 0 < len(parameters) {
            payloadExtras[errorResponseKeyParameters] = parameters
        }
    }

    /* the validationErrors key is the public half of an http exception's context, projected here so an entry blaming the declaration does not hand its internals to the client; the record keeps them */
    if nil != httpException {
        if errorsValue, exists := httpException.Context()["validationErrors"]; true == exists {
            payloadExtras["validationErrors"] = clientVisibleValidationErrors(errorsValue)
        }
    }

    if true == debugMode {
        var melodyError *exception.Error
        melodyErrorFound := exception.ChainAs(err, &melodyError)
        if true == melodyErrorFound && nil != melodyError {
            payloadExtras[errorResponseKeyContext] = withOccurrenceCoordinates(melodyError.Context(), err)

            causeErr := melodyError.CauseErr()
            if nil != causeErr {
                payloadExtras[errorResponseKeyCause] = debugErrorMessage(causeErr)
            }
        } else if true == unavailable {
            payloadExtras[errorResponseKeyCause] = debugErrorMessage(err)
        }
    }

    response := renderErrorResponse(runtimeInstance, request, statusCode, message, payloadExtras)

    SetRetryAfter(response, retryAfter)

    return response
}

/* attachRequestContextToError carries the request coordinates onto an already-logged error: onto the occurrence the kernel marked when the chain holds one, so a value several requests share keeps no request's coordinates, and onto the nearest melody error otherwise, the error a lower layer filed itself. A key already held is kept, and an empty coordinate is not written. */
func attachRequestContextToError(err error, requestId string, method string, path string) {
    var occurrence *loggedOccurrence
    if true == exception.ChainAs(err, &occurrence) && nil != occurrence {
        for key, value := range map[string]string{
            "requestId": requestId,
            "method":    method,
            "path":      path,
        } {
            if "" == value {
                continue
            }

            if _, exists := occurrence.coordinates[key]; true == exists {
                continue
            }

            occurrence.coordinates[key] = value
        }

        return
    }

    var melodyError *exception.Error
    if false == exception.ChainAs(err, &melodyError) || nil == melodyError {
        return
    }

    existingContext := melodyError.Context()

    for key, value := range map[string]string{
        "requestId": requestId,
        "method":    method,
        "path":      path,
    } {
        if "" == value {
            continue
        }

        if _, exists := existingContext[key]; true == exists {
            continue
        }

        melodyError.SetContextValue(key, value)
    }
}

/* isDeliberateClientFault reports whether a failure is a 4xx the handler meant, which is recorded at warning: a 5xx, a non-http error, a 4xx whose validation errors blame the declaration and an error answered as unavailable, whatever store status lies beneath its mark, are failures recorded at error. The kernel's handler-error record and the exception listener's read the same answer. */
func isDeliberateClientFault(err error) bool {
    if true == exception.AnswersUnavailable(err) {
        return false
    }

    httpException := exception.AsHttpException(err)

    return nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() && false == carriesRuleWiringError(httpException)
}

/* carriesRuleWiringError reports whether the validation errors blame the rule declaration rather than the submitted value, which no client input can reach. */
func carriesRuleWiringError(httpException *exception.HttpException) bool {
    if nil == httpException {
        return false
    }

    errorsValue, exists := httpException.Context()["validationErrors"]
    if false == exists {
        return false
    }

    validationErrors, isValidationErrors := errorsValue.(validation.ValidationErrors)
    if false == isValidationErrors {
        return false
    }

    return validationErrors.HasRuleWiringError()
}

/* clientVisibleValidationErrors strips the internal context of the entries that blame the declaration and leaves every other entry untouched. A value that is not the framework's own collection is handed back as it came. */
func clientVisibleValidationErrors(errorsValue any) any {
    validationErrors, isValidationErrors := errorsValue.(validation.ValidationErrors)
    if false == isValidationErrors {
        return errorsValue
    }

    return validationErrors.WithoutRuleWiringContext()
}

/* withOccurrenceCoordinates answers the context of the debug payload: the error's own context, with the coordinates of the occurrence the kernel marked under the keys the context does not hold. The error's context is copied, never written. */
func withOccurrenceCoordinates(context exceptioncontract.Context, err error) exceptioncontract.Context {
    var occurrence *loggedOccurrence
    if false == exception.ChainAs(err, &occurrence) || nil == occurrence || 0 == len(occurrence.coordinates) {
        return context
    }

    merged := make(exceptioncontract.Context, len(context)+len(occurrence.coordinates))
    for key, value := range context {
        merged[key] = value
    }

    for key, value := range occurrence.coordinates {
        if _, exists := merged[key]; true == exists {
            continue
        }

        merged[key] = value
    }

    return merged
}
