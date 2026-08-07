package http

import (
    "errors"
    "fmt"
    "html"
    nethttp "net/http"
    "time"

    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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

                    if nil != exceptionEvent.Request() && nil != exceptionEvent.Request().RequestContext() {
                        requestId = exceptionEvent.Request().RequestContext().RequestId()
                    }

                    if nil != exceptionEvent.Request() && nil != exceptionEvent.Request().HttpRequest() {
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

                        /* a deliberate 4xx a handler returned is a refusal, not an incident: it is recorded at warning, while a 5xx and every non-http error keep the error level */
                        if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() {
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
                message = exceptionEvent.Err().Error()
            }

            response := (httpcontract.Response)(nil)

            if true == PrefersHtml(exceptionEvent.Request()) {
                requestId := ""
                if nil != exceptionEvent.Request() && nil != exceptionEvent.Request().RequestContext() {
                    requestId = exceptionEvent.Request().RequestContext().RequestId()
                }

                htmlBody := "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body>" +
                    "<h1>Error</h1>" +
                    "<p>Status: " + fmt.Sprintf("%d", statusCode) + "</p>" +
                    "<p>Message: " + html.EscapeString(message) + "</p>" +
                    "<p>Request-Id: " + html.EscapeString(requestId) + "</p>" +
                    "</body></html>"

                response = HtmlResponse(statusCode, htmlBody)
            } else {
                payload := map[string]any{
                    "error": message,
                    "time":  time.Now().Format(time.RFC3339),
                }

                /* the errors context key is the public half of an http exception's context: BindJsonAndValidate attaches the per-field validation errors under it, and without this the detail the validator computed reached neither the client nor, structured, anything else */
                if contextHttpException := exception.AsHttpException(exceptionEvent.Err()); nil != contextHttpException {
                    if errorsValue, exists := contextHttpException.Context()["errors"]; true == exists {
                        payload["errors"] = errorsValue
                    }
                }

                if true == debugMode {
                    var melodyError *exception.Error
                    melodyErrorFound := errors.As(exceptionEvent.Err(), &melodyError)
                    if true == melodyErrorFound && nil != melodyError {
                        payload["context"] = melodyError.Context()

                        causeErr := melodyError.CauseErr()
                        if nil != causeErr {
                            payload["cause"] = causeErr.Error()
                        }
                    }
                }

                jsonResponse, jsonErr := JsonResponse(statusCode, payload)
                if nil == jsonErr {
                    response = jsonResponse
                } else {
                    response = JsonErrorResponse(statusCode, message)
                }
            }

            if nil != exceptionEvent.Request() && nil != exceptionEvent.Request().RequestContext() {
                requestId := exceptionEvent.Request().RequestContext().RequestId()
                if "" != requestId && "" == response.Headers().Get(HeaderRequestId) {
                    response.Headers().Set(HeaderRequestId, requestId)
                }
            }

            exceptionEvent.SetResponse(response)

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
