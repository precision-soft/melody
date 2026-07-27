package http

import (
    "html"
    nethttp "net/http"
    "sort"
    "strings"
    "time"

    "github.com/precision-soft/melody/v2/config"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

type MethodPolicy struct {
    HeadFallbackToGet bool
    AutomaticOptions  bool
}

type KernelOptions struct {
    MethodPolicy           MethodPolicy
    ForwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy
    SessionCookiePolicy    httpcontract.SessionCookiePolicy
}

func DefaultKernelOptions() KernelOptions {
    return KernelOptions{
        MethodPolicy: MethodPolicy{
            HeadFallbackToGet: true,
            AutomaticOptions:  true,
        },
        ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{
            TrustForwardedHeaders: false,
            TrustedProxyList:      make([]string, 0),
        },
        SessionCookiePolicy: httpcontract.SessionCookiePolicy{
            Path:     "/",
            Domain:   "",
            SameSite: nethttp.SameSiteLaxMode,
        },
    }
}

func NewKernel(router httpcontract.Router) *Kernel {
    return &Kernel{
        router:          router,
        middlewares:     make([]httpcontract.Middleware, 0),
        notFoundHandler: nil,
        errorHandler:    nil,
        options:         DefaultKernelOptions(),
    }
}

type Kernel struct {
    router          httpcontract.Router
    middlewares     []httpcontract.Middleware
    notFoundHandler httpcontract.Handler
    errorHandler    httpcontract.ErrorHandler
    options         KernelOptions
}

func (instance *Kernel) Use(middlewares ...httpcontract.Middleware) {
    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.notFoundHandler = handler
}

func (instance *Kernel) SetErrorHandler(handler httpcontract.ErrorHandler) {
    instance.errorHandler = handler
}

func (instance *Kernel) SetForwardedHeadersPolicy(policy httpcontract.ForwardedHeadersPolicy) {
    instance.options.ForwardedHeadersPolicy = policy
}

func (instance *Kernel) SetSessionCookiePolicy(policy httpcontract.SessionCookiePolicy) {
    instance.options.SessionCookiePolicy = policy
}

func (instance *Kernel) ServeHttp(serviceContainer containercontract.Container) nethttp.Handler {
    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        /* @important close the scope before anything that can fail, so a panic during request-logger setup cannot leak it; the logger is captured by reference and nil-guarded for the pre-setup failure path.

        The report falls back to the emergency logger rather than being dropped: the request logger is read after the scope it was installed into has closed, which is safe only because it is an override and Close leaves overrides alone. A close failure is the one thing that must never go unreported, so the path that has no request logger to name still says what happened. */
        var requestLogger loggingcontract.Logger
        defer func() {
            scopeCloseErr := scope.Close()
            if nil == scopeCloseErr {
                return
            }

            if nil != requestLogger {
                requestLogger.Error("failed to close service container scope", exception.LogContext(scopeCloseErr))

                return
            }

            logging.EmergencyLogger().Error("failed to close service container scope", exception.LogContext(scopeCloseErr))
        }()

        requestLogger, requestId, requestIdLoggerErr := instance.requestIdLogger(serviceContainer, scope)
        if nil != requestIdLoggerErr {
            exception.Panic(
                exception.NewError("failed to create request logger", nil, requestIdLoggerErr),
            )
        }

        requestContext := NewRequestContext(requestId, time.Now())
        serviceRequestContextErr := scope.OverrideProtectedInstance(ServiceRequestContext, requestContext)
        if nil != serviceRequestContextErr {
            exception.Panic(
                exception.NewError("failed to override request context", nil, serviceRequestContextErr),
            )
        }

        writer.Header().Set(HeaderRequestId, requestId)

        runtimeInstance := runtime.New(
            request.Context(),
            scope,
            serviceContainer,
        )

        configuration := config.ConfigMustFromContainer(serviceContainer)
        defaultLocale := configuration.Http().DefaultLocale()
        debugMode := config.EnvDevelopment == configuration.Kernel().Env()

        maxBodyBytes := configuration.Http().MaxRequestBodyBytes()
        if 0 < maxBodyBytes && nil != request.Body {
            /* @important pass the raw writer, not the recording wrapper: net/http detects the server response through an unexported-method assertion with no Unwrap, so wrapping it would lose the requestTooLarge connection-close signal on oversized bodies */
            request.Body = nethttp.MaxBytesReader(rawWriter, request.Body, int64(maxBodyBytes))
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.options.ForwardedHeadersPolicy)

        matchResult, _ := instance.router.Match(
            request.Method,
            request.URL.Path,
            request.Host,
            scheme,
        )

        handler := matchResult.Handler
        params := matchResult.Params
        routeAttributes := matchResult.RouteAttributes

        if true == instance.options.MethodPolicy.HeadFallbackToGet && nethttp.MethodHead == request.Method && nil == handler {
            allowedMethodsValue, exists := routeAttributes[RouteAttributeMethods]
            if true == exists {
                allowedMethods, ok := allowedMethodsValue.([]string)
                if true == ok && 0 < len(allowedMethods) {
                    hasGet := false
                    for _, allowedMethod := range allowedMethods {
                        if nethttp.MethodGet == allowedMethod {
                            hasGet = true
                            break
                        }
                    }

                    if true == hasGet {
                        getMatchResult, _ := instance.router.Match(
                            nethttp.MethodGet,
                            request.URL.Path,
                            request.Host,
                            scheme,
                        )

                        if nil != getMatchResult {
                            handler = getMatchResult.Handler
                            params = getMatchResult.Params
                            routeAttributes = getMatchResult.RouteAttributes
                        }

                        if nil == params {
                            params = map[string]string{}
                        }
                        if nil == routeAttributes {
                            routeAttributes = map[string]any{}
                        }
                    }
                }
            }
        }

        melodyRequest := NewRequest(request, params, runtimeInstance, requestContext)

        for key, value := range routeAttributes {
            melodyRequest.Attributes().Set(key, value)
        }

        /* @important published after the route attributes so a route cannot replace what the kernel owns; the scheme is the one resolved through the configured forwarded-headers policy, which a listener has no access to — re-detecting without it reports http for every request a trusted proxy terminated as https */
        melodyRequest.Attributes().Set(RequestAttributeScheme, scheme)

        routeName := melodyRequest.RouteName()

        if nil != handler {
            requestLogger.Info(
                "route matched",
                loggingcontract.Context{
                    "method":    request.Method,
                    "path":      request.URL.Path,
                    "routeName": routeName,
                },
            )
        } else {
            allowedMethodsValue, exists := routeAttributes[RouteAttributeMethods]
            if true == exists {
                allowedMethods, ok := allowedMethodsValue.([]string)
                if true == ok && 0 < len(allowedMethods) {
                    requestLogger.Warning(
                        "method not allowed",
                        loggingcontract.Context{
                            "method":         request.Method,
                            "path":           request.URL.Path,
                            "query":          request.URL.RawQuery,
                            "scheme":         scheme,
                            "host":           request.Host,
                            "allowedMethods": allowedMethods,
                        },
                    )
                } else {
                    requestLogger.Warning(
                        "no route matched",
                        loggingcontract.Context{
                            "method": request.Method,
                            "path":   request.URL.Path,
                            "query":  request.URL.RawQuery,
                            "scheme": scheme,
                            "host":   request.Host,
                        },
                    )
                }
            } else {
                requestLogger.Warning(
                    "no route matched",
                    loggingcontract.Context{
                        "method": request.Method,
                        "path":   request.URL.Path,
                        "query":  request.URL.RawQuery,
                        "scheme": scheme,
                        "host":   request.Host,
                    },
                )
            }
        }

        finalResponse := (httpcontract.Response)(nil)

        eventDispatcher := event.EventDispatcherMustFromContainer(serviceContainer)

        var sessionManager sessioncontract.Manager
        var sessionInstance sessioncontract.Session

        defer func() {
            _, eventKernelTerminateErr := eventDispatcher.DispatchName(
                runtimeInstance,
                kernelcontract.EventKernelTerminate,
                NewKernelTerminateEvent(runtimeInstance, melodyRequest, finalResponse),
            )
            instance.logEventDispatchError(requestLogger, "kernel terminate error", eventKernelTerminateErr)
        }()

        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            /* @important net/http documents this sentinel as "abort the connection and suppress the log", and only a panic reaching its own serve loop closes the connection without a response; converting it into an error would answer an aborted upload with a 500 and an error line. The identity check matches net/http's own, so an application error merely wrapping the sentinel is unaffected. */
            if nethttp.ErrAbortHandler == recoveredValue {
                panic(recoveredValue)
            }

            recoveredErr := RecoverToError(recoveredValue)
            if nil == recoveredErr {
                return
            }

            /* the response that was in flight when the panic unwound; the error response replaces it below, and nothing else holds a reference to it */
            panickedResponse := finalResponse

            alreadyLogged := false
            exceptionErr, isExceptionErr := recoveredErr.(*exception.Error)
            if true == isExceptionErr {
                alreadyLogged = exceptionErr.AlreadyLogged()
            }

            if false == alreadyLogged {
                routeName := ""
                routeNameValue, exists := melodyRequest.Attributes().Get(RouteAttributeName)
                if true == exists {
                    if routeNameString, ok := routeNameValue.(string); true == ok {
                        routeName = routeNameString
                    }
                }

                durationMs := time.Since(requestContext.StartedAt()).Milliseconds()

                requestLogger.Error(
                    "unhandled http error",
                    exception.LogContext(
                        recoveredErr,
                        exceptioncontract.Context{
                            "method":     melodyRequest.HttpRequest().Method,
                            "path":       melodyRequest.HttpRequest().URL.Path,
                            "routeName":  routeName,
                            "durationMs": durationMs,
                        },
                    ),
                )

                _ = exception.MarkLogged(recoveredErr)
            }

            exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, recoveredErr)
            _, eventKernelExceptionErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
            instance.logEventDispatchError(requestLogger, "kernel exception error", eventKernelExceptionErr)

            if nil == exceptionEvent.Response() {
                if nil != instance.errorHandler {
                    customResponse := instance.errorHandler(runtimeInstance, writer, melodyRequest, recoveredErr)
                    if nil != customResponse {
                        exceptionEvent.SetResponse(customResponse)
                    }
                }
            }

            if nil == exceptionEvent.Response() {
                statusCode := nethttp.StatusInternalServerError
                message := "internal server error"
                if true == debugMode {
                    message = recoveredErr.Error()
                }

                if true == PrefersHtml(melodyRequest) {
                    exceptionEvent.SetResponse(HtmlResponse(
                        statusCode,
                        "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+html.EscapeString(message)+"</p></body></html>",
                    ))
                } else {
                    exceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
                }
            }

            /* @important the response built before the panic may own an open file (FileResponse/ServeReader); it is about to lose its only reference, so close it unless the exception handler chose to keep it */
            if nil != panickedResponse && panickedResponse != exceptionEvent.Response() {
                closeDiscardedResponseBody(panickedResponse, requestLogger)
            }

            finalResponse = exceptionEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelExceptionErr = eventDispatcher.DispatchName(
                runtimeInstance,
                kernelcontract.EventKernelResponse,
                kernelResponseEvent,
            )
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelExceptionErr)

            /* @important close the swapped-out response body so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            writeResponse(
                runtimeInstance,
                melodyRequest,
                writer,
                finalResponse,
                sessionManager,
                sessionInstance,
                instance.options.ForwardedHeadersPolicy,
                instance.options.SessionCookiePolicy,
            )
        }()

        /* @important the session is loaded HERE, after the recovery defer is installed, and must not be moved back up with the rest of the request setup: both Manager.Session and Manager.NewSession turn a storage outage into a panic, and above the guard that panic escapes ServeHttp — net/http closes the connection with no response, the terminate listener never fires and the access-log line is lost */
        sessionManager = session.SessionMustFromContainer(serviceContainer)

        cookie, _ := request.Cookie(session.SessionCookieName)
        if nil != cookie {
            sessionInstance = sessionManager.Session(cookie.Value)
        }
        if nil == sessionInstance {
            sessionInstance = sessionManager.NewSession()
        }

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        kernelRequestEvent := NewKernelRequestEvent(runtimeInstance, melodyRequest)
        _, eventKernelRequestErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, kernelRequestEvent)
        instance.logEventDispatchError(requestLogger, "kernel request error", eventKernelRequestErr)

        /* @important fail closed when the kernel.request dispatch aborted with an error and no listener produced a response: the dispatcher stops at the first failing listener, so listeners behind it (e.g. the access-control listener) never ran; proceeding to the handler would treat a partially-processed request as authorized */
        if nil != eventKernelRequestErr && nil == kernelRequestEvent.Response() {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = eventKernelRequestErr.Error()
            }

            if true == PrefersHtml(melodyRequest) {
                kernelRequestEvent.SetResponse(HtmlResponse(
                    statusCode,
                    "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+html.EscapeString(message)+"</p></body></html>",
                ))
            } else {
                kernelRequestEvent.SetResponse(JsonErrorResponse(statusCode, message))
            }
        }

        if nil != kernelRequestEvent.Response() {
            finalResponse = kernelRequestEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* @important close the swapped-out response body so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            writeResponse(
                runtimeInstance,
                melodyRequest,
                writer,
                finalResponse,
                sessionManager,
                sessionInstance,
                instance.options.ForwardedHeadersPolicy,
                instance.options.SessionCookiePolicy,
            )

            return
        }

        localeValue := ""
        if value, exists := params[RouteAttributeLocale]; true == exists {
            localeValue = value
        }
        if "" == localeValue {
            localeValue = defaultLocale
        }
        if "" != localeValue {
            melodyRequest.Attributes().Set(RouteAttributeLocale, localeValue)
        }

        baseHandler := handler

        if nil == baseHandler {
            baseHandler = func(
                runtimeInstance runtimecontract.Runtime,
                writer nethttp.ResponseWriter,
                request httpcontract.Request,
            ) (httpcontract.Response, error) {
                allowedMethodsValue, exists := request.Attributes().Get(RouteAttributeMethods)
                if true == exists {
                    allowedMethods, ok := allowedMethodsValue.([]string)
                    if true == ok && 0 < len(allowedMethods) {
                        allowedMethodsSet := make(map[string]struct{}, len(allowedMethods)+2)

                        hasGet := false
                        hasHead := false

                        for _, allowedMethod := range allowedMethods {
                            allowedMethodsSet[allowedMethod] = struct{}{}

                            if nethttp.MethodGet == allowedMethod {
                                hasGet = true
                            }
                            if nethttp.MethodHead == allowedMethod {
                                hasHead = true
                            }
                        }

                        /* @important only advertise in Allow the synthetic methods the kernel actually honors under the configured MethodPolicy: OPTIONS is answered automatically only when AutomaticOptions is set (otherwise an OPTIONS request falls through to this 405), and a HEAD is served by falling back to GET only when HeadFallbackToGet is set — so listing either under the opposite configuration promises a method that in fact returns 405. A method the route declares explicitly is already added from allowedMethods above. */
                        if true == instance.options.MethodPolicy.AutomaticOptions {
                            allowedMethodsSet[nethttp.MethodOptions] = struct{}{}
                        }

                        if true == instance.options.MethodPolicy.HeadFallbackToGet && true == hasGet && false == hasHead {
                            allowedMethodsSet[nethttp.MethodHead] = struct{}{}
                        }

                        normalizedAllowedMethods := make([]string, 0, len(allowedMethodsSet))
                        for allowedMethod := range allowedMethodsSet {
                            normalizedAllowedMethods = append(normalizedAllowedMethods, allowedMethod)
                        }
                        sort.Strings(normalizedAllowedMethods)

                        if nethttp.MethodOptions == request.HttpRequest().Method && true == instance.options.MethodPolicy.AutomaticOptions {
                            response := EmptyResponse(nethttp.StatusNoContent)
                            response.headers.Set("Allow", strings.Join(normalizedAllowedMethods, ", "))
                            return response, nil
                        }

                        response := JsonErrorResponse(nethttp.StatusMethodNotAllowed, "method not allowed")
                        response.headers.Set("Allow", strings.Join(normalizedAllowedMethods, ", "))
                        return response, nil
                    }
                }

                if nil != instance.notFoundHandler {
                    response, err := instance.notFoundHandler(runtimeInstance, writer, request)
                    if nil != err {
                        requestLogger.Error(
                            "not found handler error",
                            exception.LogContext(
                                err,
                                exceptioncontract.Context{
                                    "path": request.HttpRequest().URL.Path,
                                },
                            ),
                        )

                        kernelExceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, err)
                        instance.dispatchEventKernelException(kernelExceptionEvent, runtimeInstance, requestLogger, eventDispatcher)

                        if nil == kernelExceptionEvent.Response() {
                            if nil != instance.errorHandler {
                                customResponse := instance.errorHandler(runtimeInstance, writer, request, err)
                                if nil != customResponse {
                                    kernelExceptionEvent.SetResponse(customResponse)
                                }
                            }
                        }

                        if nil == kernelExceptionEvent.Response() {
                            statusCode := nethttp.StatusInternalServerError
                            message := "internal server error"
                            if true == debugMode {
                                message = err.Error()
                            }

                            if true == PrefersHtml(request) {
                                kernelExceptionEvent.SetResponse(HtmlResponse(
                                    statusCode,
                                    "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+html.EscapeString(message)+"</p></body></html>",
                                ))
                            } else {
                                kernelExceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
                            }
                        }

                        if nil != response && response != kernelExceptionEvent.Response() {
                            closeDiscardedResponseBody(response, requestLogger)
                        }

                        return kernelExceptionEvent.Response(), nil
                    }

                    return response, nil
                }

                return JsonErrorResponse(nethttp.StatusNotFound, "not found"), nil
            }
        }

        kernelControllerEvent := NewKernelControllerEvent(runtimeInstance, melodyRequest)
        _, eventKernelControllerErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelController, kernelControllerEvent)
        instance.logEventDispatchError(requestLogger, "kernel controller error", eventKernelControllerErr)

        /* @important fail closed when the kernel.controller dispatch aborted with an error and no listener produced a response, mirroring the kernel.request path: the dispatcher stops at the first failing listener, so a required listener behind it (marked through RequiredListenerRegistrar) never ran; proceeding to the handler would treat a partially-processed request as authorized */
        if nil != eventKernelControllerErr && nil == kernelControllerEvent.Response() {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = eventKernelControllerErr.Error()
            }

            if true == PrefersHtml(melodyRequest) {
                kernelControllerEvent.SetResponse(HtmlResponse(
                    statusCode,
                    "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+html.EscapeString(message)+"</p></body></html>",
                ))
            } else {
                kernelControllerEvent.SetResponse(JsonErrorResponse(statusCode, message))
            }
        }

        if nil != kernelControllerEvent.Response() {
            finalResponse = kernelControllerEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* @important close the swapped-out response body so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            writeResponse(
                runtimeInstance,
                melodyRequest,
                writer,
                finalResponse,
                sessionManager,
                sessionInstance,
                instance.options.ForwardedHeadersPolicy,
                instance.options.SessionCookiePolicy,
            )

            return
        }

        middlewaresSnapshot := append(
            []httpcontract.Middleware{},
            instance.middlewares...,
        )
        finalHandler := instance.buildHandler(baseHandler, middlewaresSnapshot)

        response, finalHandlerErr := finalHandler(runtimeInstance, writer, melodyRequest)
        if nil != finalHandlerErr {
            requestLogger.Error(
                "controller handler error",
                exception.LogContext(
                    finalHandlerErr,
                    exceptioncontract.Context{
                        "path": request.URL.Path,
                    },
                ),
            )

            kernelExceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, finalHandlerErr)
            instance.dispatchEventKernelException(kernelExceptionEvent, runtimeInstance, requestLogger, eventDispatcher)

            if nil == kernelExceptionEvent.Response() {
                if nil != instance.errorHandler {
                    customResponse := instance.errorHandler(runtimeInstance, writer, melodyRequest, finalHandlerErr)
                    if nil != customResponse {
                        kernelExceptionEvent.SetResponse(customResponse)
                    }
                }
            }

            if nil == kernelExceptionEvent.Response() {
                statusCode := nethttp.StatusInternalServerError
                message := "internal server error"
                if true == debugMode {
                    message = finalHandlerErr.Error()
                }

                if true == PrefersHtml(melodyRequest) {
                    kernelExceptionEvent.SetResponse(HtmlResponse(
                        statusCode,
                        "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+html.EscapeString(message)+"</p></body></html>",
                    ))
                } else {
                    kernelExceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
                }
            }

            if nil != response && response != kernelExceptionEvent.Response() {
                closeDiscardedResponseBody(response, requestLogger)
            }

            response = kernelExceptionEvent.Response()
        }

        /* a handler that returns no response is answered with an empty 204, and it is given one here rather than deep inside writeResponse, so that kernel.response is dispatched for it like for every other outcome. A listener is the only thing that decorates a response — cross-origin headers, cache directives, the access log's status code — and a response that never reaches one comes out visibly different from the identical response written explicitly: the browser drops a nil-returning cross-origin DELETE for want of the headers its explicit-204 twin carries, and the log records status 0. */
        if nil == response {
            response = EmptyResponse(nethttp.StatusNoContent)
        }

        finalResponse = response
        kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
        _, eventKernelResponseErr := eventDispatcher.DispatchName(
            runtimeInstance,
            kernelcontract.EventKernelResponse,
            kernelResponseEvent,
        )
        instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

        /* @important close the swapped-out response body so a file-backed body (FileResponse/ServeReader) is not leaked */
        if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
            closeDiscardedResponseBody(finalResponse, requestLogger)
        }

        finalResponse = kernelResponseEvent.Response()
        writeResponse(
            runtimeInstance,
            melodyRequest,
            writer,
            finalResponse,
            sessionManager,
            sessionInstance,
            instance.options.ForwardedHeadersPolicy,
            instance.options.SessionCookiePolicy,
        )
    })
}

func (instance *Kernel) dispatchEventKernelException(
    kernelExceptionEvent *KernelExceptionEvent,
    runtimeInstance runtimecontract.Runtime,
    requestLogger loggingcontract.Logger,
    eventDispatcher eventcontract.EventDispatcher,
) {
    _, eventKernelExceptionErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, kernelExceptionEvent)
    instance.logEventDispatchError(requestLogger, "kernel exception error", eventKernelExceptionErr)
}

func (instance *Kernel) requestIdLogger(
    serviceContainer containercontract.Container,
    scope containercontract.Scope,
) (loggingcontract.Logger, string, error) {
    requestId := logging.GenerateProcessId()

    baseLogger := logging.LoggerMustFromContainer(serviceContainer)
    if nil == baseLogger {
        return nil, requestId, exception.NewError("failed to get base logger", nil, nil)
    }

    requestLogger := logging.NewRequestLogger(baseLogger, requestId, "requestId")

    err := scope.OverrideProtectedInstance(logging.ServiceLogger, requestLogger)
    if nil != err {
        return nil, requestId, exception.NewError("failed to override request logger", nil, err)
    }

    return requestLogger, requestId, nil
}

func (instance *Kernel) logEventDispatchError(
    logger loggingcontract.Logger,
    message string,
    dispatchErr error,
) {
    if nil == dispatchErr {
        return
    }

    alreadyLogged := false
    exceptionErr, ok := dispatchErr.(*exception.Error)
    if true == ok && nil != exceptionErr {
        alreadyLogged = exceptionErr.AlreadyLogged()
    }

    if true == alreadyLogged {
        return
    }

    logger.Error(
        message,
        exception.LogContext(dispatchErr),
    )

    _ = exception.MarkLogged(dispatchErr)
}

func (instance *Kernel) buildHandler(handler httpcontract.Handler, middlewares []httpcontract.Middleware) httpcontract.Handler {
    return wrapWithMiddlewares(handler, middlewares)
}

var _ httpcontract.Kernel = (*Kernel)(nil)
