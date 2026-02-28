package http

import (
    "errors"
    "fmt"
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

                html := "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body>" +
                    "<h1>Error</h1>" +
                    "<p>Status: " + fmt.Sprintf("%d", statusCode) + "</p>" +
                    "<p>Message: " + message + "</p>" +
                    "<p>Request-Id: " + requestId + "</p>" +
                    "</body></html>"

                response = HtmlResponse(statusCode, html)
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
