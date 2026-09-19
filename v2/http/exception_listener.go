package http

import (
    "errors"
    nethttp "net/http"

    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/validation"
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

            /* the search goes through the door of the package, which refuses the typed nil the raw errors.As matches and reports as found: a handler returning an unassigned *HttpException as its error made StatusCode() dereference nil here, and the same value reaching the kernel's recovery instead panicked inside the handler that had already recovered. The two branches this replaces asked the same question twice, one of them without the guard. */
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

                        /* a deliberate 4xx a handler returned is a refusal, not an incident: it is recorded at warning, while a 5xx and every non-http error keep the error level. A 4xx whose validation errors blame the DECLARATION is the exception, because the deliberation is exactly what is missing: a struct tag naming a rule that does not exist refuses every request that route will ever serve, with a body naming the client's field, and at warning it sat in the dashboard among the users who mistyped their address while the route stayed permanently broken. */
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

            /* the errors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and without this the detail the validator computed reached neither the client nor, structured, anything else.

               Public is the operative word. An entry blaming the declaration rather than the value carries the developer's own typo, the parameters the constraint refused and its reason, and those belong to the operator reading the record, not to whoever sent the request. The projection is taken here and only here: the record is rendered from the exception's own context, which keeps everything — the marshaler the two renderings share is the same one, and it stays the one that says the same thing in both places. */
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
