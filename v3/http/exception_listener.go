package http

import (
    "errors"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/validation"
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

            if nil == exceptionEvent.Err() {
                return nil
            }

            /* the search goes through the exception package's door, which refuses a typed nil errors.As would match */
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
                        method = exceptionEvent.Request().HttpRequest().Method
                        if nil != exceptionEvent.Request().HttpRequest().URL {
                            path = exceptionEvent.Request().HttpRequest().URL.Path
                        }
                    }

                    /* an error already logged is not filed again: the request coordinates are attached to it instead. Through the kernel every producer logs before it dispatches; this branch journals the dispatches made by hand, such as the rate-limit listener's. */
                    if true == exception.IsAlreadyLogged(exceptionEvent.Err()) {
                        attachRequestContextToError(exceptionEvent.Err(), requestId, method, path)
                    } else {
                        _ = exception.MarkLogged(exceptionEvent.Err())

                        recordContext := exception.LogContext(
                            exceptionEvent.Err(),
                            exceptioncontract.Context{
                                "requestId": requestId,
                                "method":    method,
                                "path":      path,
                            },
                        )

                        /* a deliberate 4xx is recorded at warning, a 5xx and any non-http error at error; a 4xx whose validation errors blame the declaration is a program failure and recorded at error */
                        if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() && false == carriesRuleWiringError(httpException) {
                            loggerInstance.Warning("unhandled exception", recordContext)
                        } else {
                            loggerInstance.Error("unhandled exception", recordContext)
                        }
                    }
                }
            }

            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"

            if nil != httpException {
                statusCode = httpException.StatusCode()
                message = httpException.Message()
            } else if true == debugMode {
                message = debugErrorMessage(exceptionEvent.Err())
            }

            payloadExtras := map[string]any{}

            /* the validationErrors key is the public half of an http exception's context, projected here so an entry blaming the declaration does not hand its internals to the client; the record keeps them */
            if nil != httpException {
                if errorsValue, exists := httpException.Context()["validationErrors"]; true == exists {
                    payloadExtras["validationErrors"] = clientVisibleValidationErrors(errorsValue)
                }
            }

            if true == debugMode {
                var melodyError *exception.Error
                melodyErrorFound := errors.As(exceptionEvent.Err(), &melodyError)
                if true == melodyErrorFound && nil != melodyError {
                    payloadExtras["context"] = melodyError.Context()

                    causeErr := melodyError.CauseErr()
                    if nil != causeErr {
                        payloadExtras["cause"] = debugErrorMessage(causeErr)
                    }
                }
            }

            exceptionEvent.SetResponse(
                renderErrorResponse(runtimeInstance, exceptionEvent.Request(), statusCode, message, payloadExtras),
            )

            return nil
        },
        KernelExceptionListenerPriority,
    )
}

/* attachRequestContextToError carries the request coordinates onto an already-logged error. A key the error already holds is kept, and an empty coordinate is not written. */
func attachRequestContextToError(err error, requestId string, method string, path string) {
    var melodyError *exception.Error
    if false == errors.As(err, &melodyError) || nil == melodyError {
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
