package http

import (
    "errors"
    "fmt"
    "html"
    nethttp "net/http"
    "time"

    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

                        loggerInstance.Error(
                            "unhandled exception",
                            exception.LogContext(
                                exceptionEvent.Err(),
                                exceptioncontract.Context{
                                    "requestId": requestId,
                                    "method":    method,
                                    "path":      path,
                                },
                            ),
                        )
                    }
                }
            }

            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"

            var httpException *exception.HttpException
            ok = errors.As(exceptionEvent.Err(), &httpException)
            if true == ok {
                statusCode = httpException.StatusCode()
                message = httpException.Message()
            } else {
                exceptionHttpException := exception.AsHttpException(exceptionEvent.Err())
                if nil != exceptionHttpException {
                    statusCode = exceptionHttpException.StatusCode()
                    message = exceptionHttpException.Message()
                } else if true == debugMode {
                    message = exceptionEvent.Err().Error()
                }
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

                if true == debugMode {
                    var melodyError *exception.Error
                    ok = errors.As(exceptionEvent.Err(), &melodyError)
                    if true == ok && nil != melodyError {
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
