package http

import (
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
    return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        scope := serviceContainer.NewScope()

        requestLogger, requestId, requestIdLoggerErr := instance.requestIdLogger(serviceContainer, scope)
        if nil != requestIdLoggerErr {
            exception.Panic(
                exception.NewError("failed to create request logger", nil, requestIdLoggerErr),
            )
        }

        defer func() {
            scopeCloseErr := scope.Close()
            if nil != scopeCloseErr {
                requestLogger.Error("failed to close service container scope", exception.LogContext(scopeCloseErr))
            }
        }()

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

        sessionManager := session.SessionMustFromContainer(serviceContainer)
        cookie, _ := request.Cookie(session.SessionCookieName)

        var sessionInstance sessioncontract.Session
        if nil != cookie {
            sessionInstance = sessionManager.Session(cookie.Value)
        }
        if nil == sessionInstance {
            sessionInstance = sessionManager.NewSession()
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.options.ForwardedHeadersPolicy)

        matchResult, _ := instance.router.Match(
            request.Method,
            request.URL.Path,
            request.Host,
            scheme,
        )

        if nil == matchResult {
            matchResult = &httpcontract.MatchResult{}
        }

        handler := matchResult.Handler
        params := matchResult.Params
        routeAttributes := matchResult.RouteAttributes

        if nil == params {
            params = map[string]string{}
        }
        if nil == routeAttributes {
            routeAttributes = map[string]any{}
        }

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

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        for key, value := range routeAttributes {
            melodyRequest.Attributes().Set(key, value)
        }

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

            recoveredErr := RecoverToError(recoveredValue)
            if nil == recoveredErr {
                return
            }

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
                debugMode := config.EnvDevelopment == configuration.Kernel().Env()

                statusCode := nethttp.StatusInternalServerError
                message := "internal server error"
                if true == debugMode {
                    message = recoveredErr.Error()
                }

                if true == PrefersHtml(melodyRequest) {
                    exceptionEvent.SetResponse(HtmlResponse(
                        statusCode,
                        "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+message+"</p></body></html>",
                    ))
                } else {
                    exceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
                }
            }

            finalResponse = exceptionEvent.Response()

            _, eventKernelExceptionErr = eventDispatcher.DispatchName(
                runtimeInstance,
                kernelcontract.EventKernelResponse,
                NewKernelResponseEvent(melodyRequest, finalResponse),
            )
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelExceptionErr)

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

        kernelRequestEvent := NewKernelRequestEvent(runtimeInstance, melodyRequest)
        _, eventKernelRequestErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, kernelRequestEvent)
        instance.logEventDispatchError(requestLogger, "kernel request error", eventKernelRequestErr)

        if nil != kernelRequestEvent.Response() {
            finalResponse = kernelRequestEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

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

                        allowedMethodsSet[nethttp.MethodOptions] = struct{}{}

                        if true == hasGet && false == hasHead {
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
                            debugMode := config.EnvDevelopment == configuration.Kernel().Env()

                            statusCode := nethttp.StatusInternalServerError
                            message := "internal server error"
                            if true == debugMode {
                                message = err.Error()
                            }

                            kernelExceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
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

        if nil != kernelControllerEvent.Response() {
            finalResponse = kernelControllerEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            if nil != eventKernelResponseErr {
                requestLogger.Error(
                    "kernel response error",
                    exception.LogContext(eventKernelResponseErr),
                )
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
                debugMode := config.EnvDevelopment == configuration.Kernel().Env()

                statusCode := nethttp.StatusInternalServerError
                message := "internal server error"
                if true == debugMode {
                    message = finalHandlerErr.Error()
                }

                if true == PrefersHtml(melodyRequest) {
                    kernelExceptionEvent.SetResponse(HtmlResponse(
                        statusCode,
                        "<!doctype html><html><head><meta charset=\"utf-8\"><title>Melody Error</title></head><body><h1>Error</h1><p>"+message+"</p></body></html>",
                    ))
                } else {
                    kernelExceptionEvent.SetResponse(JsonErrorResponse(statusCode, message))
                }
            }

            response = kernelExceptionEvent.Response()
        }

        if nil != response {
            finalResponse = response
            _, eventKernelResponseErr := eventDispatcher.DispatchName(
                runtimeInstance,
                kernelcontract.EventKernelResponse,
                NewKernelResponseEvent(melodyRequest, finalResponse),
            )
            if nil != eventKernelResponseErr {
                requestLogger.Error(
                    "kernel response error",
                    exception.LogContext(eventKernelResponseErr),
                )
            }

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
