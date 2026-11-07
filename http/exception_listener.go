package http

import (
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/validation"
)

const (
    KernelExceptionListenerPriority = -1000
)

func RegisterKernelExceptionListener(eventDispatcher eventcontract.EventDispatcher, debugMode bool) {
    eventDispatcher.AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
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

            /* an application error handler takes the listener's place: the kernel consults it once the event is left unanswered */
            if true == exceptionEvent.errorHandlerInstalled {
                return nil
            }

            if nil == exceptionEvent.Err() {
                return nil
            }

            httpException := exception.AsHttpException(exceptionEvent.Err())

            if nil != runtimeInstance {
                loggerInstance := logging.LoggerFromRuntime(runtimeInstance)
                if nil != loggerInstance {
                    requestId := ""
                    path := ""
                    method := ""

                    if false == internal.IsNilInterface(exceptionEvent.Request()) && nil != exceptionEvent.Request().RequestContext() {
                        requestId = exceptionEvent.Request().RequestContext().RequestId()
                    }

                    if false == internal.IsNilInterface(exceptionEvent.Request()) && nil != exceptionEvent.Request().HttpRequest() {
                        method = internal.BoundDiagnosticText(exceptionEvent.Request().HttpRequest().Method)
                        if nil != exceptionEvent.Request().HttpRequest().URL {
                            path = internal.BoundDiagnosticText(exceptionEvent.Request().HttpRequest().URL.Path)
                        }
                    }

                    /* an error this listener files is marked through the event's occurrence, never in the value, which requests can share */
                    if true == exception.IsAlreadyLogged(exceptionEvent.Err()) {
                        attachRequestContextToError(exceptionEvent.Err(), requestId, method, path)
                    } else {
                        exceptionEvent.err = newLoggedOccurrence(exceptionEvent.Err())
                        attachRequestContextToError(exceptionEvent.err, requestId, method, path)

                        recordContext := exception.LogContext(
                            exceptionEvent.Err(),
                            exceptioncontract.Context{
                                "requestId": requestId,
                                "method":    method,
                                "path":      path,
                            },
                        )

                        if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() && false == carriesRuleWiringError(httpException) {
                            loggerInstance.Warning("unhandled exception", recordContext)
                        } else {
                            loggerInstance.Error("unhandled exception", recordContext)
                        }
                    }
                }
            }

            exceptionEvent.SetResponse(
                exceptionResponseFor(runtimeInstance, exceptionEvent.Request(), exceptionEvent.Err(), debugMode),
            )

            return nil
        },
        KernelExceptionListenerPriority,
    )
}

func exceptionResponseFor(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, err error, debugMode bool) httpcontract.Response {
    httpException := exception.AsHttpException(err)

    statusCode := nethttp.StatusInternalServerError
    message := "internal server error"

    if nil != httpException {
        statusCode = httpException.StatusCode()
        message = httpException.Message()
    } else if true == debugMode {
        message = debugErrorMessage(err)
    }

    payloadExtras := map[string]any{}

    /* only the public projection is sent: an entry blaming the declaration carries the developer's typo and the parameters */
    if nil != httpException {
        if errorsValue, exists := httpException.Context()["errors"]; true == exists {
            payloadExtras["errors"] = clientVisibleValidationErrors(errorsValue)
        }
    }

    if true == debugMode {
        var melodyError *exception.Error
        melodyErrorFound := exception.ChainAs(err, &melodyError)
        if true == melodyErrorFound && nil != melodyError {
            payloadExtras["context"] = withOccurrenceCoordinates(melodyError.Context(), err)

            causeErr := melodyError.CauseErr()
            if nil != causeErr {
                payloadExtras["cause"] = debugErrorMessage(causeErr)
            }
        }
    }

    return renderErrorResponse(runtimeInstance, request, statusCode, message, payloadExtras)
}

/* coordinates land on the kernel's occurrence, which no request shares; with none, on the nearest melody error */
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

func carriesRuleWiringError(httpException *exception.HttpException) bool {
    if nil == httpException {
        return false
    }

    errorsValue, exists := httpException.Context()["errors"]
    if false == exists {
        return false
    }

    validationErrors, isValidationErrors := errorsValue.(validation.ValidationErrors)
    if false == isValidationErrors {
        return false
    }

    return validationErrors.HasRuleWiringError()
}

func clientVisibleValidationErrors(errorsValue any) any {
    validationErrors, isValidationErrors := errorsValue.(validation.ValidationErrors)
    if false == isValidationErrors {
        return errorsValue
    }

    return validationErrors.WithoutRuleWiringContext()
}

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
