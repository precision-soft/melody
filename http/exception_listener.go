package http

import (
    "errors"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

            if nil == exceptionEvent.Err() {
                return nil
            }

            /* the search goes through the package's door, which refuses the typed nil the raw errors.As matches and reports as found, so a handler returning an unassigned *HttpException does not make StatusCode() dereference nil here. */
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

                    /* the mark is read the way the kernel's writers already read it before they log: an error recorded upstream is not filed a second time under a second message. What only this listener knows — the request coordinates — is attached to the error itself instead of being rewritten as a duplicate record. */
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

                        /* a deliberate 4xx a handler returned is a refusal, recorded at warning; a 5xx and every non-http error keep the error level. A 4xx whose validation errors blame the DECLARATION is an error: a struct tag naming a rule that does not exist refuses every request the route will ever serve. */
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

            /* the errors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and they reach the client here. Only the public projection is sent: an entry blaming the declaration carries the developer's typo and the constraint's parameters, which belong to the record, and the record is rendered from the exception's full context. */
            if nil != httpException {
                if errorsValue, exists := httpException.Context()["errors"]; true == exists {
                    payloadExtras["errors"] = clientVisibleValidationErrors(errorsValue)
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

/* attachRequestContextToError carries the request coordinates onto an already-logged error in place of a second record. A key the error already holds is kept — the writer closest to the failure said more — and an empty coordinate says nothing, so it is not written. */
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

/* carriesRuleWiringError reports whether the exception's validation errors blame the rule DECLARATION rather than the submitted value — a rule the registry does not know, a parameter set the constraint refused, a tag the parser could not read, a pattern that does not compile. None of them is reachable from any input a client sends, so the 4xx they produce is a program failure wearing a client-error status, and the record it earns is an error rather than the warning a deliberate refusal earns. */
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

/* clientVisibleValidationErrors projects the per-field errors onto what the client may see, stripping the internal context of the entries that blame the declaration and leaving every other entry — bounds, lengths, the material a client needs to correct its request — untouched. A value that is not the framework's own collection is handed back as it came: the key is the public half of the context by contract, and an application that put its own shape there owns it. */
func clientVisibleValidationErrors(errorsValue any) any {
    validationErrors, isValidationErrors := errorsValue.(validation.ValidationErrors)
    if false == isValidationErrors {
        return errorsValue
    }

    return validationErrors.WithoutRuleWiringContext()
}
