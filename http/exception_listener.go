package http

import (
    "errors"
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

            /* an application error handler takes the listener's place: the kernel consults it once the listener leaves the event unanswered, whenever the handler was installed and whatever serves the kernel */
            if true == exceptionEvent.errorHandlerInstalled {
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
                            path = internal.BoundDiagnosticText(exceptionEvent.Request().HttpRequest().URL.Path)
                        }
                    }

                    /* the mark is read the way the kernel's writers already read it before they log: an error recorded upstream is not filed a second time under a second message. What only this listener knows — the request coordinates — is attached to the occurrence instead of being rewritten as a duplicate record; an error this listener files is marked through the event's own occurrence, never in the error value, which another request can share. */
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

                        /* a deliberate 4xx a handler returned is a refusal, recorded at warning; a 5xx and every non-http error keep the error level. A 4xx whose validation errors blame the DECLARATION is an error: a struct tag naming a rule that does not exist refuses every request the route will ever serve. */
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

/* exceptionResponseFor renders the answer an error earns when no listener and no error handler answered it: the status and message of the http exception its chain carries, its validation errors projected for the client, and in debug mode the context and cause of the nearest melody error; any other error is a 500. The exception listener answers with it, and so does the kernel when an installed error handler declines. */
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

    /* the errors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and they reach the client here. Only the public projection is sent: an entry blaming the declaration carries the developer's typo and the constraint's parameters, which belong to the record, and the record is rendered from the exception's full context. */
    if nil != httpException {
        if errorsValue, exists := httpException.Context()["errors"]; true == exists {
            payloadExtras["errors"] = clientVisibleValidationErrors(errorsValue)
        }
    }

    if true == debugMode {
        var melodyError *exception.Error
        melodyErrorFound := errors.As(err, &melodyError)
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

/* attachRequestContextToError carries the request coordinates onto an already-logged error in place of a second record: onto the occurrence the kernel marked when the chain holds one, so a value several requests share keeps no request's coordinates, and onto the nearest melody error otherwise, the error a lower layer filed itself. A key already held is kept — the writer closest to the failure said more — and an empty coordinate says nothing, so it is not written. */
func attachRequestContextToError(err error, requestId string, method string, path string) {
    var occurrence *loggedOccurrence
    if true == errors.As(err, &occurrence) && nil != occurrence {
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

/* withOccurrenceCoordinates answers the context of the debug payload: the error's own context, with the coordinates of the occurrence the kernel marked under the keys the context does not hold. The error's context is copied, never written. */
func withOccurrenceCoordinates(context exceptioncontract.Context, err error) exceptioncontract.Context {
    var occurrence *loggedOccurrence
    if false == errors.As(err, &occurrence) || nil == occurrence || 0 == len(occurrence.coordinates) {
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
