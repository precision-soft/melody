package http

import (
    "context"
    nethttp "net/http"
    "runtime/debug"
    "sort"
    "strings"
    "sync/atomic"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v3/config"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

type MethodPolicy = httpcontract.MethodPolicy

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
    /* the shutdown's only measure of a hijacked connection, which net/http's Shutdown does not wait for */
    openRequestScopes atomic.Int64
    serving           atomic.Bool
}

func sessionCookieBootWarnings(policy httpcontract.SessionCookiePolicy) []internal.BootWarning {
    if nethttp.SameSiteNoneMode != resolveSessionCookieSameSite(policy) || httpcontract.SessionCookieSecureNever != policy.Secure {
        return nil
    }

    return []internal.BootWarning{
        {
            Name:    "http.sessionCookie.sameSiteNoneNeverSecure",
            Message: "the session cookie policy sets SameSite=None with Secure never: every current browser drops such a cookie, so no session sticks",
            Context: loggingcontract.Context{"configurationKey": "SessionCookiePolicy"},
        },
    }
}

func (instance *Kernel) OpenRequestScopes() int64 {
    return instance.openRequestScopes.Load()
}

/* the configuration doors write fields request goroutines read unsynchronized, so they refuse once serving */
func (instance *Kernel) refuseMutationWhileServing(door string) {
    if false == instance.serving.Load() {
        return
    }

    exception.Panic(
        exception.NewError(
            "may not configure the http kernel after it started serving",
            map[string]any{
                "door": door,
            },
            nil,
        ),
    )
}

/* Use decorates the handler path only: a response a listener produced is written without the chain */
func (instance *Kernel) Use(middlewares ...httpcontract.Middleware) {
    instance.refuseMutationWhileServing("Use")

    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.refuseMutationWhileServing("SetNotFoundHandler")

    instance.notFoundHandler = handler
}

/* SetErrorHandler renders the errors the kernel raises; a nil answer takes the default, listener refusals bypass it */
func (instance *Kernel) SetErrorHandler(handler httpcontract.ErrorHandler) {
    instance.refuseMutationWhileServing("SetErrorHandler")

    instance.errorHandler = handler
}

func (instance *Kernel) HasErrorHandler() bool {
    return nil != instance.errorHandler
}

/* SetForwardedHeadersPolicy is boot-only, refuses an unparsable trusted-proxy entry by name and copies the list */
func (instance *Kernel) SetForwardedHeadersPolicy(policy httpcontract.ForwardedHeadersPolicy) {
    instance.refuseMutationWhileServing("SetForwardedHeadersPolicy")

    if validationErr := internal.ValidateTrustedProxyList(policy.TrustedProxyList); nil != validationErr {
        exception.Panic(validationErr)
    }

    policy.TrustedProxyList = copyStringList(policy.TrustedProxyList)
    instance.options.ForwardedHeadersPolicy = policy
}

func (instance *Kernel) SetSessionCookiePolicy(policy httpcontract.SessionCookiePolicy) {
    instance.refuseMutationWhileServing("SetSessionCookiePolicy")

    instance.options.SessionCookiePolicy = policy
}

func (instance *Kernel) SetMethodPolicy(policy httpcontract.MethodPolicy) {
    instance.refuseMutationWhileServing("SetMethodPolicy")

    instance.options.MethodPolicy = policy
}

func copyStringList(values []string) []string {
    if nil == values {
        return nil
    }

    return append(make([]string, 0, len(values)), values...)
}

func (instance *Kernel) ServeHttp(serviceContainer containercontract.Container) nethttp.Handler {
    /* the freeze is not a lock: a mutator already past its check is not caught */
    instance.serving.Store(true)
    freezeRouterForServing(instance.router)

    bootWarnings := internal.NewBootWarningsOnce(sessionCookieBootWarnings(instance.options.SessionCookiePolicy))

    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        instance.openRequestScopes.Add(1)

        /* reading the request logger after its scope closed is safe: it is an override, which Close leaves alone */
        var requestLogger loggingcontract.Logger
        var requestId string
        defer func() {
            defer instance.openRequestScopes.Add(-1)

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

        /* last guard for the window the main recovery cannot cover; registered under the scope-close defer so the scope is open */
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            if nethttp.ErrAbortHandler == recoveredValue {
                panic(recoveredValue)
            }

            recoveredErr := RecoverToError(recoveredValue)
            if nil == recoveredErr {
                return
            }

            logger := requestLogger
            if nil == logger {
                logger = logging.EmergencyLogger()
            }

            if false == exception.IsAlreadyLogged(recoveredErr) {
                logger.Error(
                    "unhandled http error before the kernel recovery guard",
                    exception.LogContext(
                        recoveredErr,
                        exceptioncontract.Context{
                            /* named explicitly: the emergency logger this falls back to does not inject it */
                            "requestId":  requestId,
                            "method":     internal.BoundDiagnosticText(request.Method),
                            "path":       internal.BoundDiagnosticText(request.URL.Path),
                            "panicStack": string(debug.Stack()),
                        },
                    ),
                )
            }

            if true == writer.HeadersWritten() {
                return
            }

            /* written directly: writeResponse resolves the logger from a runtime absent here and persists an unreached session */
            _ = WriteToHttpResponseWriter(nil, nil, writer, JsonErrorResponse(nethttp.StatusInternalServerError, "internal server error"))
        }()

        requestLogger, requestId, requestIdLoggerErr := instance.requestIdLogger(serviceContainer, scope)
        if nil != requestIdLoggerErr {
            exception.Panic(
                exception.NewError("failed to create request logger", nil, requestIdLoggerErr),
            )
        }

        bootWarnings.Write(requestLogger)

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
            /* the raw writer is passed: net/http detects its own writer by an unexported-method assertion a wrapper loses */
            request.Body = nethttp.MaxBytesReader(rawWriter, request.Body, int64(maxBodyBytes))
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.options.ForwardedHeadersPolicy)

        /* the route is matched on the path as the client spelled it, so an encoded separator stays inside its segment */
        matchPath := internal.RequestPathAsSent(request.URL)

        /* a stale RawPath is not matched: a route selected on the re-escaped decoded path is one the client never named */
        rawPathIsStale := internal.RequestRawPathIsStale(request.URL)

        pathRefused := false == requestPathIsCanonical(RequestPathAsRouted(internal.RequestPathAsSent(request.URL))) || ("" != request.URL.Path && strings.TrimLeftFunc(request.URL.Path, unicode.IsSpace) != request.URL.Path) || true == rawPathIsStale
        var matchResult *httpcontract.MatchResult
        if false == rawPathIsStale {
            matchResult, _ = instance.router.Match(
                request.Method,
                matchPath,
                request.Host,
                scheme,
            )
        }

        /* a nil result is a valid no match; a dereference here panics above the main recovery, into the last guard */
        if nil == matchResult {
            matchResult = &httpcontract.MatchResult{}
        }

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
                            matchPath,
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

        /* published after the route attributes so a route cannot replace what the kernel owns */
        melodyRequest.Attributes().Set(RequestAttributeScheme, scheme)

        routeName := melodyRequest.RouteName()

        if nil != handler {
            requestLogger.Info(
                "route matched",
                loggingcontract.Context{
                    "method":    internal.BoundDiagnosticText(request.Method),
                    "path":      internal.BoundDiagnosticText(request.URL.Path),
                    "routeName": routeName,
                },
            )
        } else if false == pathRefused {
            allowedMethodsValue, exists := routeAttributes[RouteAttributeMethods]
            if true == exists {
                allowedMethods, ok := allowedMethodsValue.([]string)
                if true == ok && 0 < len(allowedMethods) {
                    if nethttp.MethodOptions == request.Method && true == instance.options.MethodPolicy.AutomaticOptions {
                        requestLogger.Debug(
                            "automatic options answered",
                            loggingcontract.Context{
                                "method":         internal.BoundDiagnosticText(request.Method),
                                "path":           internal.BoundDiagnosticText(request.URL.Path),
                                "query":          internal.BoundDiagnosticText(internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery)),
                                "scheme":         scheme,
                                "host":           internal.BoundDiagnosticText(request.Host),
                                "allowedMethods": allowedMethods,
                            },
                        )
                    } else {
                        requestLogger.Warning(
                            "method not allowed",
                            loggingcontract.Context{
                                "method":         internal.BoundDiagnosticText(request.Method),
                                "path":           internal.BoundDiagnosticText(request.URL.Path),
                                "query":          internal.BoundDiagnosticText(internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery)),
                                "scheme":         scheme,
                                "host":           internal.BoundDiagnosticText(request.Host),
                                "allowedMethods": allowedMethods,
                            },
                        )
                    }
                } else {
                    requestLogger.Warning(
                        "no route matched",
                        loggingcontract.Context{
                            "method": internal.BoundDiagnosticText(request.Method),
                            "path":   internal.BoundDiagnosticText(request.URL.Path),
                            "query":  internal.BoundDiagnosticText(internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery)),
                            "scheme": scheme,
                            "host":   internal.BoundDiagnosticText(request.Host),
                        },
                    )
                }
            } else {
                requestLogger.Warning(
                    "no route matched",
                    loggingcontract.Context{
                        "method": internal.BoundDiagnosticText(request.Method),
                        "path":   internal.BoundDiagnosticText(request.URL.Path),
                        "query":  internal.BoundDiagnosticText(internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery)),
                        "scheme": scheme,
                        "host":   internal.BoundDiagnosticText(request.Host),
                    },
                )
            }
        }

        finalResponse := (httpcontract.Response)(nil)

        chainResponse := (httpcontract.Response)(nil)

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

            /* net/http's abort sentinel is re-raised, matched by identity as net/http matches it; a wrapping error is unaffected */
            if nethttp.ErrAbortHandler == recoveredValue {
                /* the abort suppresses the response, not its ownership: both held responses are closed before the re-raise */
                closeDiscardedResponseBody(finalResponse, requestLogger)

                if chainResponse != finalResponse {
                    closeDiscardedResponseBody(chainResponse, requestLogger)
                }

                panic(recoveredValue)
            }

            recoveredErr := RecoverToError(recoveredValue)
            if nil == recoveredErr {
                return
            }

            panickedResponse := finalResponse

            alreadyLogged := exception.IsAlreadyLogged(recoveredErr)

            /* the report hands on a marked carrier, never a mark in the recovered value, which other requests can share */
            reportedErr := recoveredErr

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
                            "method":     internal.BoundDiagnosticText(melodyRequest.HttpRequest().Method),
                            "path":       internal.BoundDiagnosticText(melodyRequest.HttpRequest().URL.Path),
                            "routeName":  routeName,
                            "durationMs": durationMs,
                            "panicStack": string(debug.Stack()),
                        },
                    ),
                )

                reportedErr = newLoggedOccurrence(recoveredErr)
            }

            exceptionEvent := instance.newKernelExceptionEvent(runtimeInstance, melodyRequest, reportedErr)
            _, eventKernelExceptionErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
            instance.logEventDispatchError(requestLogger, "kernel exception error", eventKernelExceptionErr)

            if nil == exceptionEvent.Response() {
                if nil != instance.errorHandler {
                    customResponse := instance.invokeErrorHandlerSafely(runtimeInstance, writer, melodyRequest, recoveredErr, requestLogger)
                    if nil != customResponse {
                        exceptionEvent.SetResponse(customResponse)
                    }
                }
            }

            if nil == exceptionEvent.Response() {
                exceptionEvent.SetResponse(
                    exceptionResponseFor(runtimeInstance, melodyRequest, exceptionEvent.Err(), debugMode),
                )
            }

            /* with finalResponse unassigned only the recording shim holds the chain's response; else the write step owns the close */
            if nil == panickedResponse && nil != chainResponse && chainResponse != exceptionEvent.Response() {
                closeDiscardedResponseBody(chainResponse, requestLogger)
            }

            finalResponse = exceptionEvent.Response()

            /* the response built before the panic may own an open file; the write step closes it once it knows what is written */
            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, panickedResponse, sessionManager, sessionInstance, requestLogger, eventDispatcher)
        }()

        /* the session loads below the recovery defer: Session and NewSession panic on a storage outage */
        sessionManager = session.SessionMustFromContainer(serviceContainer)

        cookie, _ := request.Cookie(session.SessionCookieName)
        if nil != cookie {
            sessionInstance = sessionManager.Session(cookie.Value)
        }
        /* a replaceable manager may answer not found with a typed nil, which a bare comparison takes for a live session */
        if true == internal.IsNilInterface(sessionInstance) {
            sessionInstance = sessionManager.NewSession()
        }

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        /* refused after the route is matched and before it is authorized: router, firewall and access control must agree */
        if true == pathRefused {
            requestLogger.Warning(
                "request path refused before the handler",
                loggingcontract.Context{
                    "method":  internal.BoundDiagnosticText(request.Method),
                    "path":    internal.BoundDiagnosticText(request.URL.Path),
                    "rawPath": internal.BoundDiagnosticText(request.URL.RawPath),
                },
            )

            finalResponse = renderErrorResponse(runtimeInstance, melodyRequest, nethttp.StatusBadRequest, "bad request", nil)

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)

            return
        }

        if nil != melodyRequest.bodyReadErr {
            requestLogger.Warning(
                "request body was refused before the handler",
                exception.LogContext(
                    melodyRequest.bodyReadErr,
                    exceptioncontract.Context{
                        "method": internal.BoundDiagnosticText(request.Method),
                        "path":   internal.BoundDiagnosticText(request.URL.Path),
                    },
                ),
            )

            statusCode := nethttp.StatusBadRequest
            message := "bad request"

            var maxBytesError *nethttp.MaxBytesError
            if true == exception.ChainAs(melodyRequest.bodyReadErr, &maxBytesError) {
                statusCode = nethttp.StatusRequestEntityTooLarge
                message = "payload too large"
            }

            finalResponse = renderErrorResponse(runtimeInstance, melodyRequest, statusCode, message, nil)

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)

            return
        }

        kernelRequestEvent := NewKernelRequestEvent(runtimeInstance, melodyRequest)
        _, eventKernelRequestErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, kernelRequestEvent)
        instance.logEventDispatchError(requestLogger, "kernel request error", eventKernelRequestErr)

        /* the dispatch fails closed: listeners behind the failing one never ran; the error is judged on itself, not its chain */
        _, requiredListenerSkipped := eventKernelRequestErr.(*event.RequiredListenerSkippedError)

        if nil != eventKernelRequestErr && (true == requiredListenerSkipped || nil == kernelRequestEvent.Response()) {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = debugErrorMessage(eventKernelRequestErr)
            }

            closeDiscardedResponseBody(kernelRequestEvent.Response(), requestLogger)

            kernelRequestEvent.SetResponse(
                renderErrorResponse(runtimeInstance, melodyRequest, statusCode, message, nil),
            )
        }

        if nil != kernelRequestEvent.Response() {
            finalResponse = kernelRequestEvent.Response()

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)

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

                        response := renderErrorResponse(runtimeInstance, request, nethttp.StatusMethodNotAllowed, "method not allowed", nil)
                        setResponseHeader(response, "Allow", strings.Join(normalizedAllowedMethods, ", "))
                        return response, nil
                    }
                }

                if nil != instance.notFoundHandler {
                    response, err := instance.notFoundHandler(runtimeInstance, writer, request)
                    if nil != err {
                        reportedErr := logHandlerError(requestLogger, "not found handler error", err, request.HttpRequest())

                        kernelExceptionEvent := instance.newKernelExceptionEvent(runtimeInstance, request, reportedErr)
                        instance.dispatchEventKernelException(kernelExceptionEvent, runtimeInstance, requestLogger, eventDispatcher)

                        if nil == kernelExceptionEvent.Response() {
                            if nil != instance.errorHandler {
                                customResponse := instance.invokeErrorHandlerSafely(runtimeInstance, writer, request, err, requestLogger)
                                if nil != customResponse {
                                    kernelExceptionEvent.SetResponse(customResponse)
                                }
                            }
                        }

                        if nil == kernelExceptionEvent.Response() {
                            kernelExceptionEvent.SetResponse(
                                exceptionResponseFor(runtimeInstance, request, kernelExceptionEvent.Err(), debugMode),
                            )
                        }

                        if nil != response && response != kernelExceptionEvent.Response() {
                            closeDiscardedResponseBody(response, requestLogger)
                        }

                        return kernelExceptionEvent.Response(), nil
                    }

                    return response, nil
                }

                return renderErrorResponse(runtimeInstance, request, nethttp.StatusNotFound, "not found", nil), nil
            }
        }

        kernelControllerEvent := NewKernelControllerEvent(runtimeInstance, melodyRequest)
        _, eventKernelControllerErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelController, kernelControllerEvent)
        instance.logEventDispatchError(requestLogger, "kernel controller error", eventKernelControllerErr)

        /* kernel.controller fails closed as kernel.request does: a required listener behind a failing one never ran */
        _, controllerRequiredListenerSkipped := eventKernelControllerErr.(*event.RequiredListenerSkippedError)

        if nil != eventKernelControllerErr && (true == controllerRequiredListenerSkipped || nil == kernelControllerEvent.Response()) {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = debugErrorMessage(eventKernelControllerErr)
            }

            closeDiscardedResponseBody(kernelControllerEvent.Response(), requestLogger)

            kernelControllerEvent.SetResponse(
                renderErrorResponse(runtimeInstance, melodyRequest, statusCode, message, nil),
            )
        }

        if nil != kernelControllerEvent.Response() {
            finalResponse = kernelControllerEvent.Response()

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)

            return
        }

        middlewaresSnapshot := append(
            []httpcontract.Middleware{},
            instance.middlewares...,
        )
        finalHandler := instance.buildHandler(baseHandler, middlewaresSnapshot, func(response httpcontract.Response) {
            chainResponse = response
        })

        response, finalHandlerErr := finalHandler(runtimeInstance, writer, melodyRequest)

        finalHandlerErr = normalizeBodyLimitError(finalHandlerErr)

        /* published before the error branch: everything up to the assignment can panic and the defer closes finalResponse */
        finalResponse = response

        if nil != finalHandlerErr {
            reportedErr := logHandlerError(requestLogger, "controller handler error", finalHandlerErr, request)

            kernelExceptionEvent := instance.newKernelExceptionEvent(runtimeInstance, melodyRequest, reportedErr)
            instance.dispatchEventKernelException(kernelExceptionEvent, runtimeInstance, requestLogger, eventDispatcher)

            if nil == kernelExceptionEvent.Response() {
                if nil != instance.errorHandler {
                    customResponse := instance.invokeErrorHandlerSafely(runtimeInstance, writer, melodyRequest, finalHandlerErr, requestLogger)
                    if nil != customResponse {
                        kernelExceptionEvent.SetResponse(customResponse)
                    }
                }
            }

            if nil == kernelExceptionEvent.Response() {
                kernelExceptionEvent.SetResponse(
                    exceptionResponseFor(runtimeInstance, melodyRequest, kernelExceptionEvent.Err(), debugMode),
                )
            }

            if nil != response && response != kernelExceptionEvent.Response() {
                closeDiscardedResponseBody(response, requestLogger)
            }

            response = kernelExceptionEvent.Response()
        }

        if true == internal.IsNilInterface(response) {
            response = EmptyResponse(nethttp.StatusNoContent)
        }

        finalResponse = response
        instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)
    })
}

func (instance *Kernel) invokeErrorHandlerSafely(
    runtimeInstance runtimecontract.Runtime,
    writer nethttp.ResponseWriter,
    request httpcontract.Request,
    handlerErr error,
    requestLogger loggingcontract.Logger,
) (errorHandlerResponse httpcontract.Response) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        errorHandlerResponse = nil

        requestLogger.Error(
            "error handler panicked",
            exception.LogContext(
                RecoverToError(recoveredValue),
                exceptioncontract.Context{
                    "panicStack": string(debug.Stack()),
                },
            ),
        )
    }()

    return instance.errorHandler(runtimeInstance, writer, request, handlerErr)
}

/* dispatchResponseAndWrite writes the response back through the pointer at every step: the recovery reads it */
func (instance *Kernel) dispatchResponseAndWrite(
    runtimeInstance runtimecontract.Runtime,
    melodyRequest httpcontract.Request,
    writer nethttp.ResponseWriter,
    finalResponse *httpcontract.Response,
    discardCandidate httpcontract.Response,
    sessionManager sessioncontract.Manager,
    sessionInstance sessioncontract.Session,
    requestLogger loggingcontract.Logger,
    eventDispatcher eventcontract.EventDispatcher,
) {
    kernelResponseEvent := NewKernelResponseEvent(melodyRequest, *finalResponse)

    defer func() {
        if nil == discardCandidate {
            return
        }

        closeDiscardedResponseBody(discardCandidate, requestLogger)
    }()

    _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
    instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

    publishedResponse := kernelResponseEvent.Response()

    if nil != *finalResponse && *finalResponse != publishedResponse {
        closeDiscardedResponseBody(*finalResponse, requestLogger)
    }

    if nil != discardCandidate && discardCandidate != publishedResponse && discardCandidate != *finalResponse {
        closeDiscardedResponseBody(discardCandidate, requestLogger)
    }
    discardCandidate = nil

    *finalResponse = publishedResponse
    *finalResponse = writeResponse(
        runtimeInstance,
        melodyRequest,
        writer,
        *finalResponse,
        sessionManager,
        sessionInstance,
        instance.options.ForwardedHeadersPolicy,
        instance.options.SessionCookiePolicy,
    )
}

func (instance *Kernel) newKernelExceptionEvent(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    err error,
) *KernelExceptionEvent {
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, err)
    exceptionEvent.errorHandlerInstalled = nil != instance.errorHandler

    return exceptionEvent
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

    if true == exception.IsAlreadyLogged(dispatchErr) {
        return
    }

    logger.Error(
        message,
        exception.LogContext(dispatchErr),
    )

    _ = exception.MarkLogged(dispatchErr)
}

/* logHandlerError returns a marked carrier: the handler's value is never marked, so its next request files a record */
func logHandlerError(requestLogger loggingcontract.Logger, message string, handlerErr error, httpRequest *nethttp.Request) error {
    if true == exception.IsAlreadyLogged(handlerErr) {
        return handlerErr
    }

    path := ""
    method := ""
    if nil != httpRequest {
        method = httpRequest.Method

        if nil != httpRequest.URL {
            path = httpRequest.URL.Path
        }
    }

    logContext := exception.LogContext(
        handlerErr,
        exceptioncontract.Context{
            "method": internal.BoundDiagnosticText(method),
            "path":   internal.BoundDiagnosticText(path),
        },
    )

    clientCancelled := true == exception.ChainIs(handlerErr, context.Canceled) &&
        nil != httpRequest && context.Canceled == context.Cause(httpRequest.Context())

    httpException := exception.AsHttpException(handlerErr)

    if true == clientCancelled {
        requestLogger.Warning("request cancelled by client", logContext)
    } else if true == isDeliberateClientFault(httpException) {
        requestLogger.Warning(message, logContext)
    } else {
        requestLogger.Error(message, logContext)
    }

    return newLoggedOccurrence(handlerErr)
}

/* loggedOccurrence carries the mark beside the error: one error value serves many requests, so nothing is written into it */
type loggedOccurrence struct {
    err         error
    coordinates map[string]string
}

func newLoggedOccurrence(err error) error {
    if true == internal.IsNilInterface(err) {
        return err
    }

    return &loggedOccurrence{err: err, coordinates: make(map[string]string)}
}

func (instance *loggedOccurrence) Error() string {
    return instance.err.Error()
}

func (instance *loggedOccurrence) Unwrap() error {
    return instance.err
}

func (instance *loggedOccurrence) AlreadyLogged() bool {
    return true
}

func (instance *loggedOccurrence) MarkAsLogged() {}

var _ exceptioncontract.AlreadyLogged = (*loggedOccurrence)(nil)

func normalizeBodyLimitError(handlerErr error) error {
    if nil == handlerErr {
        return handlerErr
    }

    var maxBytesError *nethttp.MaxBytesError
    if true == exception.ChainAs(handlerErr, &maxBytesError) {
        return exception.NewHttpExceptionWithCause(nethttp.StatusRequestEntityTooLarge, "payload too large", handlerErr)
    }

    return handlerErr
}

func (instance *Kernel) buildHandler(
    handler httpcontract.Handler,
    middlewares []httpcontract.Middleware,
    recordChainResponse func(httpcontract.Response),
) httpcontract.Handler {
    return wrapWithMiddlewaresRecording(handler, middlewares, recordChainResponse)
}

var _ httpcontract.Kernel = (*Kernel)(nil)

var _ httpcontract.MethodPolicySetter = (*Kernel)(nil)
