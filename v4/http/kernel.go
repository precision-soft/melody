package http

import (
    "context"
    nethttp "net/http"
    "runtime/debug"
    "slices"
    "sort"
    "strings"
    "sync/atomic"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type MethodPolicy = httpcontract.MethodPolicy

/* KernelConfig is what the http kernel is built with; the zero value is the default, and every field also has a boot-time setter. */
type KernelConfig struct {
    MethodPolicy MethodPolicy

    ForwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy

    SessionCookiePolicy httpcontract.SessionCookiePolicy

    SessionPolicy httpcontract.SessionPolicy

    Middlewares []httpcontract.Middleware

    NotFoundHandler httpcontract.Handler

    /* UnavailableRetryAfter is the Retry-After of an infrastructure-failure 503: 5s when left out, 1s to 1h in whole seconds, unlimited refused. */
    UnavailableRetryAfter bound.Duration
}

type kernelPolicies struct {
    methodPolicy           MethodPolicy
    forwardedHeadersPolicy httpcontract.ForwardedHeadersPolicy
    sessionCookiePolicy    httpcontract.SessionCookiePolicy
    sessionPolicy          httpcontract.SessionPolicy
}

func NewKernel(router httpcontract.Router, kernelConfig KernelConfig) *Kernel {
    kernel := &Kernel{
        router:      router,
        middlewares: make([]httpcontract.Middleware, 0),
    }

    kernel.SetMethodPolicy(kernelConfig.MethodPolicy)
    kernel.SetForwardedHeadersPolicy(kernelConfig.ForwardedHeadersPolicy)
    kernel.SetSessionCookiePolicy(kernelConfig.SessionCookiePolicy)
    kernel.SetSessionPolicy(kernelConfig.SessionPolicy)
    kernel.Use(kernelConfig.Middlewares)
    kernel.SetNotFoundHandler(kernelConfig.NotFoundHandler)
    kernel.SetUnavailableRetryAfter(kernelConfig.UnavailableRetryAfter)

    return kernel
}

type Kernel struct {
    router                httpcontract.Router
    middlewares           []httpcontract.Middleware
    notFoundHandler       httpcontract.Handler
    policies              kernelPolicies
    unavailableRetryAfter time.Duration
    /* the shutdown's only measure of a hijacked connection, which net/http's Shutdown does not wait for */
    openRequestScopes atomic.Int64
    /* atomic, since a mutator racing the first request is what it exists to catch */
    serving atomic.Bool
}

/* OpenRequestScopes reports how many request scopes are open, hijacked connections included. */
func (instance *Kernel) OpenRequestScopes() int64 {
    return instance.openRequestScopes.Load()
}

/* boot-only: request goroutines read these fields without synchronization */
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

/* Use appends middlewares around the matched handler only: a response a listener produced is written without them. */
func (instance *Kernel) Use(middlewares []httpcontract.Middleware) {
    instance.refuseMutationWhileServing("Use")

    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.refuseMutationWhileServing("SetNotFoundHandler")

    instance.notFoundHandler = handler
}

/* SetForwardedHeadersPolicy installs the forwarded-header policy; it is boot-only, refuses an unparsable trusted-proxy entry by name and copies the list. */
func (instance *Kernel) SetForwardedHeadersPolicy(policy httpcontract.ForwardedHeadersPolicy) {
    instance.refuseMutationWhileServing("SetForwardedHeadersPolicy")

    if validationErr := internal.ValidateTrustedProxyList(policy.TrustedProxyList); nil != validationErr {
        exception.Panic(validationErr)
    }

    policy.TrustedProxyList = copyStringList(policy.TrustedProxyList)
    instance.policies.forwardedHeadersPolicy = policy
}

/* SetSessionCookiePolicy installs the session cookie policy; it is boot-only and refuses SameSite=None with Secure never, which every current browser drops. */
func (instance *Kernel) SetSessionCookiePolicy(policy httpcontract.SessionCookiePolicy) {
    instance.refuseMutationWhileServing("SetSessionCookiePolicy")

    if nethttp.SameSiteNoneMode == resolveSessionCookieSameSite(policy) && httpcontract.SessionCookieSecureNever == policy.Secure {
        exception.Panic(
            exception.NewError(
                "the session cookie policy sets SameSite=None with Secure never, which every current browser drops, so no session sticks; write Secure always or auto, or another SameSite",
                exceptioncontract.Context{
                    "field":    "SessionCookiePolicy.Secure",
                    "sameSite": "None",
                },
                nil,
            ),
        )
    }

    instance.policies.sessionCookiePolicy = policy
}

/* SetMethodPolicy installs whether HEAD falls back to GET and whether an unrouted OPTIONS is answered; it is boot-only. */
func (instance *Kernel) SetMethodPolicy(policy httpcontract.MethodPolicy) {
    instance.refuseMutationWhileServing("SetMethodPolicy")

    instance.policies.methodPolicy = policy
}

/* SetSessionPolicy installs how the kernel serves a session its storage cannot answer; it is boot-only. */
func (instance *Kernel) SetSessionPolicy(policy httpcontract.SessionPolicy) {
    instance.refuseMutationWhileServing("SetSessionPolicy")

    instance.policies.sessionPolicy = policy
}

/* SetUnavailableRetryAfter installs the Retry-After of an infrastructure-failure 503; it is boot-only and refuses a delay outside 1s to 1h, not in whole seconds or unlimited. */
func (instance *Kernel) SetUnavailableRetryAfter(delay bound.Duration) {
    instance.refuseMutationWhileServing("SetUnavailableRetryAfter")

    resolved, resolveErr := config.ResolveUnavailableRetryAfter(delay, "KernelConfig.UnavailableRetryAfter")
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    instance.unavailableRetryAfter = resolved
}

/* a kernel built without NewKernel holds no delay, which reads as the default */
func (instance *Kernel) unavailableRetryAfterLimit() bound.Duration {
    if 0 == instance.unavailableRetryAfter {
        return bound.Duration{}
    }

    return bound.Of(instance.unavailableRetryAfter)
}

func copyStringList(values []string) []string {
    if nil == values {
        return nil
    }

    return append(make([]string, 0, len(values)), values...)
}

func (instance *Kernel) ServeHttp(serviceContainer containercontract.Container) nethttp.Handler {
    /* in the kernel rather than at boot, so a bare kernel and Boot's kernel under another server answer alike */
    installBuiltInExceptionListener(event.EventDispatcherMustFromResolver(context.Background(), serviceContainer))

    /* the freeze is not a lock: a mutator already past its check is not caught */
    instance.serving.Store(true)
    freezeRouterForServing(instance.router)

    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        instance.openRequestScopes.Add(1)

        /* closed before anything that can fail; the request logger is an override, which Close leaves alone */
        var requestLogger loggingcontract.Logger
        var requestId string
        defer func() {
            defer instance.openRequestScopes.Add(-1)

            scopeCloseErr := scope.Close()
            if nil == scopeCloseErr {
                return
            }

            if nil != requestLogger {
                requestLogger.Error("failed to close service container scope", exception.LogContext(scopeCloseErr, exception.LogContextConfig{}))

                return
            }

            logging.EmergencyLogger().Error("failed to close service container scope", exception.LogContext(scopeCloseErr, exception.LogContextConfig{}))
        }()

        /* the last guard, for the window between the scope opening and the main guard's installation, and for a panic inside the main guard */
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            /* the sentinel travels by identity through every guard */
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
                        exception.LogContextConfig{
                            Extra: exceptioncontract.Context{
                                /* named explicitly: the emergency logger does not inject it */
                                "requestId":  requestId,
                                "method":     internal.BoundDiagnosticText(request.Method),
                                "path":       internal.BoundDiagnosticText(request.URL.Path),
                                "panicStack": string(debug.Stack()),
                            },
                        },
                    ),
                )
            }

            if true == writer.HeadersWritten() {
                return
            }

            /* not writeResponse: the request never reached its session */
            _ = WriteToHttpResponseWriter(nil, nil, writer, JsonErrorResponse(nethttp.StatusInternalServerError, "internal server error"))
        }()

        /* before the first resolution, so a singleton the request path builds first reads KindRequest */
        runtimeInstance := runtime.NewRequest(
            request.Context(),
            scope,
        )

        requestLogger, requestId, requestIdLoggerErr := instance.requestIdLogger(runtimeInstance)
        if nil != requestIdLoggerErr {
            exception.Panic(
                exception.NewError("failed to create request logger", nil, requestIdLoggerErr),
            )
        }

        requestContext := NewRequestContext(requestId, time.Now())
        serviceRequestContextErr := scope.OverrideProtectedInstance(ServiceRequestContext, requestContext, containercontract.OverrideOptions{})
        if nil != serviceRequestContextErr {
            exception.Panic(
                exception.NewError("failed to override request context", nil, serviceRequestContextErr),
            )
        }

        writer.Header().Set(HeaderRequestId, requestId)

        configuration := config.ConfigMustFromRuntime(runtimeInstance)
        defaultLocale := configuration.Http().DefaultLocale()
        debugMode := config.EnvironmentDevelopment == configuration.Kernel().Environment()

        if nil != request.Body {
            /* the raw writer: net/http detects its own writer through an unexported-method assertion, so a wrapper loses the connection-close signal */
            request.Body = nethttp.MaxBytesReader(rawWriter, request.Body, int64(configuration.Http().MaxRequestBodyBytes().Value))
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.policies.forwardedHeadersPolicy)

        /* matched on the path as the client spelled it, so an encoded separator stays inside its segment */
        matchPath := internal.RequestPathAsSent(request.URL)

        /* a stale RawPath is not matched: the canonical guard below refuses it */
        rawPathIsStale := internal.RequestRawPathIsStale(request.URL)

        /* decided here so a refused path leaves its one record and no no-route record beside it */
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

        /* a nil result is a valid "no match" and, dereferenced above the recovery defer, would close the connection with no response */
        if nil == matchResult {
            matchResult = &httpcontract.MatchResult{}
        }

        handler := matchResult.Handler
        parameters := matchResult.Parameters
        routeAttributes := matchResult.RouteAttributes

        if false == instance.policies.methodPolicy.HeadFallbackDisabled && nethttp.MethodHead == request.Method && nil == handler {
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
                            parameters = getMatchResult.Parameters
                            routeAttributes = getMatchResult.RouteAttributes
                        }

                        if nil == parameters {
                            parameters = map[string]string{}
                        }
                        if nil == routeAttributes {
                            routeAttributes = map[string]any{}
                        }
                    }
                }
            }
        }

        melodyRequest := NewRequest(request, parameters, runtimeInstance, requestContext)

        for key, value := range routeAttributes {
            melodyRequest.Attributes().Set(key, value)
        }

        /* published after the route attributes, so a route cannot replace what the kernel owns */
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
        } else if _, definitionOnly := routeAttributes[RouteAttributeDefinitionOnly]; false == pathRefused && false == definitionOnly {
            allowedMethodsValue, exists := routeAttributes[RouteAttributeMethods]
            if true == exists {
                allowedMethods, ok := allowedMethodsValue.([]string)
                if true == ok && 0 < len(allowedMethods) {
                    /* an OPTIONS the method policy answers is served 204, so it is not a refused method */
                    if nethttp.MethodOptions == request.Method && false == instance.policies.methodPolicy.AutomaticOptionsDisabled {
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

        /* the recovery defer reads this before finalResponse is assigned */
        chainResponse := (httpcontract.Response)(nil)

        eventDispatcher := event.EventDispatcherMustFromRuntime(runtimeInstance)

        var sessionManager sessioncontract.Manager

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

            /* re-raised, matched by identity as net/http matches it; an error wrapping it is unaffected */
            if nethttp.ErrAbortHandler == recoveredValue {
                /* the abort suppresses the response, not its ownership, so both responses are closed first */
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

            /* a typed nil reads as unmarked */
            alreadyLogged := exception.IsAlreadyLogged(recoveredErr)

            /* marks a carrier of this occurrence: the recovered value may be shared by other requests */
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

                /* captured inside the recovering defer, while the panic frames are live */
                requestLogger.Error(
                    "unhandled http error",
                    exception.LogContext(
                        recoveredErr,
                        exception.LogContextConfig{
                            Extra: exceptioncontract.Context{
                                "method":     internal.BoundDiagnosticText(melodyRequest.HttpRequest().Method),
                                "path":       internal.BoundDiagnosticText(melodyRequest.HttpRequest().URL.Path),
                                "routeName":  routeName,
                                "durationMs": durationMs,
                                "panicStack": string(debug.Stack()),
                            },
                        },
                    ),
                )

                reportedErr = newLoggedOccurrence(recoveredErr)
            }

            exceptionResponse := instance.raiseException(runtimeInstance, melodyRequest, reportedErr, requestLogger, eventDispatcher, debugMode)

            /* handed to the write step, which closes it only once it knows what is written */

            /* a middleware that panicked after next() returned leaves the chain's response held only by the shim */
            if nil == panickedResponse && nil != chainResponse && chainResponse != exceptionResponse {
                closeDiscardedResponseBody(chainResponse, requestLogger)
            }

            finalResponse = exceptionResponse

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, panickedResponse, sessionManager, requestLogger, eventDispatcher, debugMode)
        }()

        /* resolved below the recovery defer, since a replaceable container may panic */
        sessionManager = session.SessionMustFromRuntime(runtimeInstance)

        cookieValue := ""
        cookie, _ := request.Cookie(session.SessionCookieName)
        if nil != cookie {
            cookieValue = cookie.Value
        }

        melodyRequest.Attributes().Set(requestAttributeSession, newRequestSession(runtimeInstance, sessionManager, melodyRequest, requestLogger, cookieValue, nil != cookie, instance.policies.sessionPolicy))

        /* refused after the match and before authorization, so the router, the firewall and the access control read one resource */
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

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)

            return
        }

        /* a failed urlencoded read never populated the form: 413 past the size limit, 400 otherwise */
        if nil != melodyRequest.bodyReadErr {
            requestLogger.Warning(
                "request body was refused before the handler",
                exception.LogContext(
                    melodyRequest.bodyReadErr,
                    exception.LogContextConfig{
                        Extra: exceptioncontract.Context{
                            "method": internal.BoundDiagnosticText(request.Method),
                            "path":   internal.BoundDiagnosticText(request.URL.Path),
                        },
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

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)

            return
        }

        kernelRequestEvent := NewKernelRequestEvent(runtimeInstance, melodyRequest)
        eventKernelRequestErr := instance.dispatchGuarded(runtimeInstance, eventDispatcher, requestLogger, kernelcontract.EventKernelRequest, kernelRequestEvent, &kernelRequestEvent.afterFailureDoor, "kernel request error", func() httpcontract.Response {
            return kernelRequestEvent.Response()
        })

        /* fails closed: a response set beside a failure is half made, and the listeners behind the failure, access control among them, did not run; a decision a door admitted after the failure stands unless a required listener did not run */
        _, requiredListenerSkipped := eventKernelRequestErr.(*event.RequiredListenerSkippedError)
        admittedDecision := true == kernelRequestEvent.hasAdmittedAfterFailureWrite() && true == kernelRequestEvent.hasDecision()

        requestVerdict := afterFailureVerdict{held: []httpcontract.Response{kernelRequestEvent.Response()}}
        switch {
        case true == kernelRequestEvent.decisionRefused:
            requestVerdict.refusedBy = afterFailureRefusedBySecondDecision
        case nil != eventKernelRequestErr && true == requiredListenerSkipped:
            requestVerdict.refusedBy = afterFailureRefusedByRequiredListenerSkipped
        case nil != eventKernelRequestErr && false == admittedDecision:
            requestVerdict.refusedBy = afterFailureRefusedByNoResponse
        }
        warnAfterFailureWrites(requestLogger, kernelRequestEvent.takeAfterFailureWrites(), requestVerdict)

        /* read on its own, since a listener that recovers its own panic leaves the dispatch clean */
        if true == kernelRequestEvent.decisionRefused || (nil != eventKernelRequestErr && (true == requiredListenerSkipped || (nil == kernelRequestEvent.Refusal() && false == admittedDecision))) {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode && nil != eventKernelRequestErr {
                message = debugKernelRequestFailureMessage(kernelRequestEvent.Refusal(), eventKernelRequestErr)
            }

            if nil == eventKernelRequestErr {
                requestLogger.Error("a kernel.request listener recovered the refusal of a second decision", nil)
            }

            /* replaced below and reaches no writer, so it is closed here */
            closeDiscardedResponseBody(kernelRequestEvent.Response(), requestLogger)

            kernelRequestEvent.failClosed(
                renderErrorResponse(runtimeInstance, melodyRequest, statusCode, message, nil),
            )
        }

        /* filed before the dispatch, so the record does not depend on which listener answers */
        if nil != kernelRequestEvent.Refusal() {
            reportedErr := recordExceptionOccurrence(requestLogger, melodyRequest, kernelRequestEvent.Refusal())

            finalResponse = instance.raiseException(runtimeInstance, melodyRequest, reportedErr, requestLogger, eventDispatcher, debugMode)

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)

            return
        }

        if nil != kernelRequestEvent.Response() {
            finalResponse = kernelRequestEvent.Response()

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)

            return
        }

        localeValue := ""
        if value, exists := parameters[RouteAttributeLocale]; true == exists {
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
                runtimeInstance containercontract.Runtime,
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

                        if false == instance.policies.methodPolicy.AutomaticOptionsDisabled {
                            allowedMethodsSet[nethttp.MethodOptions] = struct{}{}
                        }

                        if false == instance.policies.methodPolicy.HeadFallbackDisabled && true == hasGet && false == hasHead {
                            allowedMethodsSet[nethttp.MethodHead] = struct{}{}
                        }

                        normalizedAllowedMethods := make([]string, 0, len(allowedMethodsSet))
                        for allowedMethod := range allowedMethodsSet {
                            normalizedAllowedMethods = append(normalizedAllowedMethods, allowedMethod)
                        }
                        sort.Strings(normalizedAllowedMethods)

                        if nethttp.MethodOptions == request.HttpRequest().Method && false == instance.policies.methodPolicy.AutomaticOptionsDisabled {
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

                        exceptionResponse := instance.raiseException(runtimeInstance, request, reportedErr, requestLogger, eventDispatcher, debugMode)

                        if nil != response && response != exceptionResponse {
                            closeDiscardedResponseBody(response, requestLogger)
                        }

                        return exceptionResponse, nil
                    }

                    return response, nil
                }

                return renderErrorResponse(runtimeInstance, request, nethttp.StatusNotFound, "not found", nil), nil
            }
        }

        kernelControllerEvent := NewKernelControllerEvent(runtimeInstance, melodyRequest)
        eventKernelControllerErr := instance.dispatchGuarded(runtimeInstance, eventDispatcher, requestLogger, kernelcontract.EventKernelController, kernelControllerEvent, &kernelControllerEvent.afterFailureDoor, "kernel controller error", func() httpcontract.Response {
            return kernelControllerEvent.Response()
        })

        /* fails closed on any failure: a response set beside one is half made; a response a door admitted after the failure stands unless a required listener did not run */
        _, controllerRequiredListenerSkipped := eventKernelControllerErr.(*event.RequiredListenerSkippedError)
        admittedControllerResponse := true == kernelControllerEvent.hasAdmittedAfterFailureWrite() && nil != kernelControllerEvent.Response()

        controllerVerdict := afterFailureVerdict{held: []httpcontract.Response{kernelControllerEvent.Response()}}
        switch {
        case nil != eventKernelControllerErr && true == controllerRequiredListenerSkipped:
            controllerVerdict.refusedBy = afterFailureRefusedByRequiredListenerSkipped
        case nil != eventKernelControllerErr && false == admittedControllerResponse:
            controllerVerdict.refusedBy = afterFailureRefusedByNoResponse
        }
        warnAfterFailureWrites(requestLogger, kernelControllerEvent.takeAfterFailureWrites(), controllerVerdict)

        if nil != eventKernelControllerErr && (true == controllerRequiredListenerSkipped || false == admittedControllerResponse) {
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

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)

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

        /* published to the recovery defer first: everything until the assignment below can panic */
        finalResponse = response

        if nil != finalHandlerErr {
            reportedErr := logHandlerError(requestLogger, "controller handler error", finalHandlerErr, request)

            exceptionResponse := instance.raiseException(runtimeInstance, melodyRequest, reportedErr, requestLogger, eventDispatcher, debugMode)

            if nil != response && response != exceptionResponse {
                closeDiscardedResponseBody(response, requestLogger)
            }

            response = exceptionResponse
        }

        /* a nil response becomes an empty 204, so kernel.response listeners decorate it too */
        if true == internal.IsNilInterface(response) {
            response = EmptyResponse(nethttp.StatusNoContent)
        }

        finalResponse = response
        instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, requestLogger, eventDispatcher, debugMode)
    })
}

/* the response is written back through the pointer at every step, since the recovery reads the caller's variable when the write panics */
func (instance *Kernel) dispatchResponseAndWrite(
    runtimeInstance containercontract.Runtime,
    melodyRequest httpcontract.Request,
    writer nethttp.ResponseWriter,
    finalResponse *httpcontract.Response,
    discardCandidate httpcontract.Response,
    sessionManager sessioncontract.Manager,
    requestLogger loggingcontract.Logger,
    eventDispatcher eventcontract.EventDispatcher,
    debugMode bool,
) {
    kernelResponseEvent := NewKernelResponseEvent(melodyRequest, *finalResponse)

    /* the dispatcher re-raises a listener's exit error, so the defer closes the candidate on that path */
    defer func() {
        if nil == discardCandidate {
            return
        }

        closeDiscardedResponseBody(discardCandidate, requestLogger)
    }()

    /* an exit raised by a listener unwinds past the warnings below, so what the payload recorded is warned about and closed here too */
    replacementsWarned := false
    defer func() {
        if true == replacementsWarned {
            return
        }

        warnAfterFailureWrites(requestLogger, kernelResponseEvent.takeAfterFailureWrites(), afterFailureVerdict{nilResponseStatus: nethttp.StatusNoContent, standingServed: true, held: []httpcontract.Response{kernelResponseEvent.Response(), *finalResponse}})
    }()

    _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
    instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

    responseWrites := kernelResponseEvent.takeAfterFailureWrites()
    responseVerdict := afterFailureVerdict{nilResponseStatus: nethttp.StatusNoContent, standingServed: true}

    /* a replacement a door admitted does not waive a required listener that did not run: the response the failure left is served */
    if _, responseRequiredListenerSkipped := eventKernelResponseErr.(*event.RequiredListenerSkippedError); true == responseRequiredListenerSkipped {
        if firstAdmitted := firstAdmittedAfterFailureWrite(responseWrites); nil != firstAdmitted {
            kernelResponseEvent.response = firstAdmitted.standing
            responseVerdict.refusedBy = afterFailureRefusedByRequiredListenerSkipped
        }
    }

    publishedResponse := kernelResponseEvent.Response()

    replacementsWarned = true
    responseVerdict.held = []httpcontract.Response{publishedResponse, *finalResponse}
    warnAfterFailureWrites(requestLogger, responseWrites, responseVerdict)

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
        instance.policies.forwardedHeadersPolicy,
        instance.policies.sessionCookiePolicy,
        instance.unavailableRetryAfter,
        func(callbackErr error) httpcontract.Response {
            reportedErr := logHandlerError(requestLogger, "session persisted callback error", callbackErr, melodyRequest.HttpRequest())

            return instance.raiseException(runtimeInstance, melodyRequest, reportedErr, requestLogger, eventDispatcher, debugMode)
        },
    )
}

/* dispatchGuarded dispatches a payload guarding its writes after a failure and files the dispatch's failure; on a listener's exit, which no caller answers with the payload's response, the writes recorded so far are warned about as refused and every response they touched is closed once, the payload's abandoned one included, before the exit passes */
func (instance *Kernel) dispatchGuarded(
    runtimeInstance containercontract.Runtime,
    eventDispatcher eventcontract.EventDispatcher,
    requestLogger loggingcontract.Logger,
    eventName string,
    payload any,
    door *afterFailureDoor,
    failureMessage string,
    exitAbandoned func() httpcontract.Response,
) error {
    completed := false
    defer func() {
        if true == completed {
            return
        }

        closedResponses := warnAfterFailureWrites(requestLogger, door.takeAfterFailureWrites(), afterFailureVerdict{refusedBy: afterFailureRefusedByExit})

        abandoned := exitAbandoned()
        if false == internal.IsNilInterface(abandoned) && false == slices.Contains(closedResponses, abandoned) {
            closeDiscardedResponseBody(abandoned, requestLogger)
        }
    }()

    _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, eventName, payload)
    instance.logEventDispatchError(requestLogger, failureMessage, dispatchErr)

    completed = true

    return dispatchErr
}

/* a failed dispatch answers nothing: a response set by a listener that then panicked is half made */
func (instance *Kernel) raiseException(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    err error,
    requestLogger loggingcontract.Logger,
    eventDispatcher eventcontract.EventDispatcher,
    debugMode bool,
) httpcontract.Response {
    exceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, err, KernelExceptionEventConfig{UnavailableRetryAfter: instance.unavailableRetryAfterLimit()})

    dispatchErr := instance.dispatchGuarded(runtimeInstance, eventDispatcher, requestLogger, kernelcontract.EventKernelException, exceptionEvent, &exceptionEvent.afterFailureDoor, "kernel exception error", func() httpcontract.Response {
        return exceptionEvent.Response()
    })

    answered := exceptionEvent.Response()

    /* a response a door admitted after the failure stands unless a required listener did not run */
    _, exceptionRequiredListenerSkipped := dispatchErr.(*event.RequiredListenerSkippedError)
    admittedAnswer := nil != dispatchErr && false == exceptionRequiredListenerSkipped && true == exceptionEvent.hasAdmittedAfterFailureWrite() && nil != answered

    exceptionVerdict := afterFailureVerdict{held: []httpcontract.Response{answered}}
    switch {
    case nil != dispatchErr && true == exceptionRequiredListenerSkipped:
        exceptionVerdict.refusedBy = afterFailureRefusedByRequiredListenerSkipped
    case nil != dispatchErr && false == admittedAnswer:
        exceptionVerdict.refusedBy = afterFailureRefusedByNoResponse
    }
    warnAfterFailureWrites(requestLogger, exceptionEvent.takeAfterFailureWrites(), exceptionVerdict)

    if nil != answered && (nil == dispatchErr || true == admittedAnswer) {
        return answered
    }

    if nil != answered {
        requestLogger.Error(
            "the kernel.exception dispatch failed with a response set, which is dropped",
            loggingcontract.Context{
                "droppedResponseStatus":      answered.StatusCode(),
                "droppedResponseContentType": internal.BoundDiagnosticText(answered.Headers().Get("Content-Type")),
            },
        )

        closeDiscardedResponseBody(answered, requestLogger)
    }

    return renderExceptionSafely(runtimeInstance, request, exceptionEvent.Err(), requestLogger, ExceptionRenderConfig{DebugMode: debugMode, UnavailableRetryAfter: bound.Of(exceptionEvent.UnavailableRetryAfter())})
}

func renderExceptionSafely(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    err error,
    requestLogger loggingcontract.Logger,
    renderConfig ExceptionRenderConfig,
) (rendered httpcontract.Response) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        requestLogger.Error(
            "the exception renderer panicked",
            exception.LogContext(
                RecoverToError(recoveredValue),
                exception.LogContextConfig{
                    Extra: exceptioncontract.Context{
                        "panicStack": string(debug.Stack()),
                    },
                },
            ),
        )

        /* built without the negotiation, which is what may have panicked */
        rendered = JsonErrorResponse(nethttp.StatusInternalServerError, "internal server error")
    }()

    return RenderExceptionResponse(runtimeInstance, request, err, renderConfig)
}

/* resolved before the request logger is installed over it, so the scope still answers the container's */
func (instance *Kernel) requestIdLogger(
    runtimeInstance containercontract.Runtime,
) (loggingcontract.Logger, string, error) {
    requestId := logging.GenerateProcessId()
    scope := runtimeInstance.Scope()

    baseLogger := logging.LoggerMustFromRuntime(runtimeInstance)
    if nil == baseLogger {
        return nil, requestId, exception.NewError("failed to get base logger", nil, nil)
    }

    requestLogger := logging.NewRequestLogger(baseLogger, requestId, "requestId")

    err := scope.OverrideProtectedInstance(logging.ServiceLogger, requestLogger, containercontract.OverrideOptions{})
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
        exception.LogContext(dispatchErr, exception.LogContextConfig{}),
    )

    _ = exception.MarkLogged(dispatchErr)
}

/* marks a carrier of this occurrence, so the next request returning the same value files its own record */
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
        exception.LogContextConfig{
            Extra: exceptioncontract.Context{
                "method": internal.BoundDiagnosticText(method),
                "path":   internal.BoundDiagnosticText(path),
            },
        },
    )

    clientCancelled := true == exception.ChainIs(handlerErr, context.Canceled) &&
        nil != httpRequest && context.Canceled == context.Cause(httpRequest.Context())

    if true == clientCancelled {
        requestLogger.Warning("request cancelled by client", logContext)
    } else if true == isDeliberateClientFault(handlerErr) {
        requestLogger.Warning(message, logContext)
    } else {
        requestLogger.Error(message, logContext)
    }

    return newLoggedOccurrence(handlerErr)
}

/* loggedOccurrence marks one request's failure beside the error, since one error value is returned from many requests. */
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

/* the carrier exists only as the mark */
func (instance *loggedOccurrence) MarkAsLogged() {}

var _ exceptioncontract.AlreadyLogged = (*loggedOccurrence)(nil)

func normalizeBodyLimitError(handlerErr error) error {
    if nil == handlerErr {
        return handlerErr
    }

    var maxBytesError *nethttp.MaxBytesError
    if true == exception.ChainAs(handlerErr, &maxBytesError) {
        return exception.NewHttpError(nethttp.StatusRequestEntityTooLarge, exception.HttpErrorConfig{Message: "payload too large", Cause: handlerErr})
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

