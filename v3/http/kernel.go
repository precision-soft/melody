package http

import (
    "context"
    "errors"
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

/* MethodPolicy is the contract's type under this package's name, the one httpcontract.Kernel.SetMethodPolicy takes. */
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
    /* request scopes opened and not yet closed: the shutdown's only measure of a hijacked connection, which net/http's Shutdown does not wait for. Written by every serving goroutine, read by the shutdown one. */
    openRequestScopes atomic.Int64
    /* raised when ServeHttp builds the handler; atomic, since a mutator racing the first request is what it exists to catch */
    serving atomic.Bool
}

/* OpenRequestScopes reports how many request scopes are open, one per request being served, hijacked connections included. A shutdown reads it to tell a drained server from one that still has work inside it. */
func (instance *Kernel) OpenRequestScopes() int64 {
    return instance.openRequestScopes.Load()
}

/* the configuration doors are boot-only: every mutator writes a field request goroutines read without synchronization, and a concurrent write to the route tree's maps is a fatal error, so after the handler is built each one refuses on the calling goroutine, naming the door. The reading doors stay open, since the openapi document is served from inside a handler. */
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
/* Use appends middlewares to the chain around the matched handler. The chain decorates the handler path only: a response a listener produced is written without it, so response decoration that must reach every response belongs to kernel.response listeners. */
func (instance *Kernel) Use(middlewares ...httpcontract.Middleware) {
    instance.refuseMutationWhileServing("Use")

    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.refuseMutationWhileServing("SetNotFoundHandler")

    instance.notFoundHandler = handler
}

/* SetErrorHandler installs the application's own error rendering, read at boot: the framework exception listener is registered only when no handler is installed, so the handler takes over negotiation, the request-id header and the validation payload. When it returns nil, the kernel's default rendering answers. */
func (instance *Kernel) SetErrorHandler(handler httpcontract.ErrorHandler) {
    instance.refuseMutationWhileServing("SetErrorHandler")

    instance.errorHandler = handler
}

/* HasErrorHandler reports whether the application installed an error handler. */
func (instance *Kernel) HasErrorHandler() bool {
    return nil != instance.errorHandler
}

/* SetForwardedHeadersPolicy installs the policy every forwarded-header reader consults; it is boot-only. A trusted-proxy entry that parses as neither a CIDR prefix nor an address is refused by name, and the list is copied, since it decides on every request whether X-Forwarded-Proto is believed. */
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

/* SetMethodPolicy installs the method policy the kernel reads on every request: whether HEAD falls back to the GET route and whether an unrouted OPTIONS is answered with the computed Allow header. It is boot-only. */
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
    /* the freeze is raised when the handler is built, and refuses every mutator that begins after it; it is not a lock, so a mutator already past its check is not caught. The router is frozen through a package-private door; a router from outside this package keeps only the written contract. */
    instance.serving.Store(true)
    freezeRouterForServing(instance.router)

    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        /* counted when the scope exists and released in the defer that closes it, so the counter is exactly the set of scopes a teardown would find open */
        instance.openRequestScopes.Add(1)

        /* the scope is closed before anything that can fail, so a panic during request-logger setup cannot leak it. A close failure falls back to the emergency logger; reading the request logger after its scope closed is safe because it is an override, which Close leaves alone. */
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

        /* the last guard, for the window the main recovery cannot cover: from the scope opening to the main guard's installation, plus a panic raised inside the main guard itself. Registered under the scope-close defer so the response is written while the scope is open; it is inert on every request the main guard answered. It dispatches neither kernel.terminate nor kernel.exception, so its record carries the method and the path. */
        defer func() {
            recoveredValue := recover()
            if nil == recoveredValue {
                return
            }

            /* the sentinel is the caller's own instruction to drop the connection without a response, and it travels by identity through every guard */
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
                            "method":     request.Method,
                            "path":       request.URL.Path,
                            "panicStack": string(debug.Stack()),
                        },
                    ),
                )

                _ = exception.Logged(recoveredErr)
            }

            if true == writer.HeadersWritten() {
                return
            }

            /* written directly rather than through writeResponse, which resolves the logger from a runtime that does not exist in this window and persists a session this request never reached */
            _ = WriteToHttpResponseWriter(nil, nil, writer, JsonErrorResponse(nethttp.StatusInternalServerError, "internal server error"))
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
            /* the raw writer is passed, not the recording wrapper: net/http detects its own response writer through an unexported-method assertion, so a wrapper loses the connection-close signal on an oversized body */
            request.Body = nethttp.MaxBytesReader(rawWriter, request.Body, int64(maxBodyBytes))
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.options.ForwardedHeadersPolicy)

        /* the route is matched on the path as the client spelled it, so an encoded separator stays inside its segment; splitRequestPath unescapes each segment after the split, so a parameter binds the decoded value */
        matchPath := internal.RequestPathAsSent(request.URL)

        /* a stale RawPath is not matched: the canonical guard below refuses it, and a route selected on the re-escaped decoded path would be one the client never named, read by every kernel.response and kernel.terminate listener */
        var matchResult *httpcontract.MatchResult
        if false == internal.RequestRawPathIsStale(request.URL) {
            matchResult, _ = instance.router.Match(
                request.Method,
                matchPath,
                request.Host,
                scheme,
            )
        }

        /* a nil result is a valid "no match" under the contract, and dereferenced here, above the recovery defer, it would close the connection with no response */
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

        /* published after the route attributes so a route cannot replace what the kernel owns; the scheme is the one resolved through the forwarded-headers policy, which a listener cannot reach */
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
                            "query":          internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery),
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
                            "query":  internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery),
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
                        "query":  internal.RedactQueryValuesForDiagnostics(request.URL.RawQuery),
                        "scheme": scheme,
                        "host":   request.Host,
                    },
                )
            }
        }

        finalResponse := (httpcontract.Response)(nil)

        /* the last response the middleware chain had in flight, published by the recording shim; the recovery defer reads it before finalResponse is assigned */
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

            /* net/http's abort sentinel drops the connection without a response and suppresses the log, so it is re-raised, matched by identity as net/http matches it; an error wrapping it is unaffected */
            if nethttp.ErrAbortHandler == recoveredValue {
                /* the abort suppresses the response, not the ownership of what it holds: both the assigned response and the one the chain shim holds are closed before the sentinel is re-raised */
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

            /* the response in flight when the panic unwound; the error response replaces it below, and nothing else holds it */
            panickedResponse := finalResponse

            /* the mark is read through the door that writes it, at the depth it is written, and a typed nil reads as unmarked */
            alreadyLogged := exception.IsAlreadyLogged(recoveredErr)

            /* a runtime panic recovers to a runtime.Error, which has nowhere for the mark to live, so the report hands back a marked carrier keeping it as its cause; the error handler and the debug message keep the recovered value itself */
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

                /* the stack is captured inside the recovering defer, while the panic frames are live; every recovery boundary in the framework records the same key */
                requestLogger.Error(
                    "unhandled http error",
                    exception.LogContext(
                        recoveredErr,
                        exceptioncontract.Context{
                            "method":     melodyRequest.HttpRequest().Method,
                            "path":       melodyRequest.HttpRequest().URL.Path,
                            "routeName":  routeName,
                            "durationMs": durationMs,
                            "panicStack": string(debug.Stack()),
                        },
                    ),
                )

                reportedErr = exception.Logged(recoveredErr)
            }

            exceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, reportedErr)
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
                message := "internal server error"
                if true == debugMode {
                    message = debugErrorMessage(recoveredErr)
                }

                exceptionEvent.SetResponse(
                    renderErrorResponse(runtimeInstance, melodyRequest, nethttp.StatusInternalServerError, message, nil),
                )
            }

            /* the response built before the panic may own an open file, so it is handed to the write step, which closes it only once it knows what is written: a listener of the re-published kernel.response may answer with that same response */

            /* a middleware that panicked after its next() returned leaves the chain's response held only by the recording shim, so it is closed here; where finalResponse was assigned the write step owns the close, since a wrapping middleware may share the body with the response served */
            if nil == panickedResponse && nil != chainResponse && chainResponse != exceptionEvent.Response() {
                closeDiscardedResponseBody(chainResponse, requestLogger)
            }

            finalResponse = exceptionEvent.Response()

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, panickedResponse, sessionManager, sessionInstance, requestLogger, eventDispatcher)
        }()

        /* the session is loaded after the recovery defer is installed and must stay below it: Session and NewSession turn a storage outage into a panic, which above the guard escapes ServeHttp with no response */
        sessionManager = session.SessionMustFromContainer(serviceContainer)

        cookie, _ := request.Cookie(session.SessionCookieName)
        if nil != cookie {
            sessionInstance = sessionManager.Session(cookie.Value)
        }
        /* IsNilInterface, since a replaceable manager may answer "not found" with a typed nil, which a bare comparison takes for a live session */
        if true == internal.IsNilInterface(sessionInstance) {
            sessionInstance = sessionManager.NewSession()
        }

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        /* a path that folds to a different spelling is refused after the route is matched and before it is authorized or handled, so the router, the firewall matchers and the access control never disagree about the resource. It is asked of RequestPathAsRouted, the spelling the access-control matcher reads too; requestPathIsCanonical states the boundary. */
        /* the leading form of the padded path is asked of the decoded path as well: " /public" routes as "%20/public", a target the guard would otherwise leave to the router */
        /* a stale RawPath, left by a handler in front that rewrote Path alone, does not carry the spelling the client sent, so an encoded separator would be read as a separator: it is refused, as the first and second majors refuse on the raw path */
        if false == requestPathIsCanonical(RequestPathAsRouted(internal.RequestPathAsSent(request.URL))) || ("" != request.URL.Path && strings.TrimLeftFunc(request.URL.Path, unicode.IsSpace) != request.URL.Path) || true == internal.RequestRawPathIsStale(request.URL) {
            requestLogger.Warning(
                "request path refused before the handler",
                loggingcontract.Context{
                    "method":  request.Method,
                    "path":    request.URL.Path,
                    "rawPath": request.URL.RawPath,
                },
            )

            finalResponse = renderErrorResponse(runtimeInstance, melodyRequest, nethttp.StatusBadRequest, "bad request", nil)

            instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)

            return
        }

        /* a urlencoded body whose read or parse failed never populated the form, so it is refused as the json binding refuses it: 413 when the size limit stopped the read, 400 otherwise */
        if nil != melodyRequest.bodyReadErr {
            requestLogger.Warning(
                "request body was refused before the handler",
                exception.LogContext(
                    melodyRequest.bodyReadErr,
                    exceptioncontract.Context{
                        "method": request.Method,
                        "path":   request.URL.Path,
                    },
                ),
            )

            statusCode := nethttp.StatusBadRequest
            message := "bad request"

            var maxBytesError *nethttp.MaxBytesError
            if true == errors.As(melodyRequest.bodyReadErr, &maxBytesError) {
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

        /* the kernel.request dispatch fails closed: an error with no response means listeners behind the failing one, access control among them, never ran; a dispatch that skipped a required listener is refused too, and its response dropped for the error page. The error is judged on itself, not its cause chain. */
        _, requiredListenerSkipped := eventKernelRequestErr.(*event.RequiredListenerSkippedError)

        if nil != eventKernelRequestErr && (true == requiredListenerSkipped || nil == kernelRequestEvent.Response()) {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = debugErrorMessage(eventKernelRequestErr)
            }

            /* the stopping listener's response is replaced below and reaches no writer, so it is closed here */
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

                        /* Allow lists OPTIONS and HEAD only when the method policy honors them; a method the route declares is already added above */
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
                        reportedErr := logHandlerError(requestLogger, "not found handler error", err, request.HttpRequest())

                        kernelExceptionEvent := NewKernelExceptionEvent(runtimeInstance, request, reportedErr)
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
                            message := "internal server error"
                            if true == debugMode {
                                message = debugErrorMessage(err)
                            }

                            kernelExceptionEvent.SetResponse(
                                renderErrorResponse(runtimeInstance, request, nethttp.StatusInternalServerError, message, nil),
                            )
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

        /* the kernel.controller dispatch fails closed on the same terms as kernel.request: a required listener behind a failing one never ran */
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

        /* a body-limit overflow a handler surfaced, from ParseMultipartForm, is answered 413 at warning like the pre-handler body paths */
        finalHandlerErr = normalizeBodyLimitError(finalHandlerErr)

        /* published to the recovery defer before the error branch, since everything between here and the assignment below can panic and the defer closes only what finalResponse names */
        finalResponse = response

        if nil != finalHandlerErr {
            reportedErr := logHandlerError(requestLogger, "controller handler error", finalHandlerErr, request)

            kernelExceptionEvent := NewKernelExceptionEvent(runtimeInstance, melodyRequest, reportedErr)
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
                message := "internal server error"
                if true == debugMode {
                    message = debugErrorMessage(finalHandlerErr)
                }

                kernelExceptionEvent.SetResponse(
                    renderErrorResponse(runtimeInstance, melodyRequest, nethttp.StatusInternalServerError, message, nil),
                )
            }

            if nil != response && response != kernelExceptionEvent.Response() {
                closeDiscardedResponseBody(response, requestLogger)
            }

            response = kernelExceptionEvent.Response()
        }

        /* a nil response becomes an empty 204 here, so kernel.response is dispatched for it and its listeners decorate it like any other response */
        if true == internal.IsNilInterface(response) {
            response = EmptyResponse(nethttp.StatusNoContent)
        }

        finalResponse = response
        instance.dispatchResponseAndWrite(runtimeInstance, melodyRequest, writer, &finalResponse, nil, sessionManager, sessionInstance, requestLogger, eventDispatcher)
    })
}

/* invokeErrorHandlerSafely runs the application's error handler under the kernel's own recovery, since the failed response's body is still open and held only by the caller. A panic, net/http's abort sentinel included, is logged with its stack and answered by the default error response. */
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

/* dispatchResponseAndWrite is the one exit of every request path through ServeHttp: it publishes kernel.response, writes the response the listeners answered with, and closes the body of the one they swapped out. The response is written back through the pointer at every step, since the caller's variable is what the recovery reads when the write panics. */
/* discardCandidate is a response the caller is about to lose and which must be closed unless this step writes it; it is closed after the publish, never before, since a listener may answer with it. */
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

    /* the dispatcher re-raises an exit error from a listener, and then nothing below runs, so the defer closes the candidate on that path; the ordinary path clears it */
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

    /* the same mark reader the writer beside it uses */
    if true == exception.IsAlreadyLogged(dispatchErr) {
        return
    }

    logger.Error(
        message,
        exception.LogContext(dispatchErr),
    )

    _ = exception.MarkLogged(dispatchErr)
}

/* logHandlerError files the one record for a handler-returned failure: an error already logged is not filed again, a deliberate 4xx is a warning, the request context's own cancellation is named as the client's, and everything else is an error. The returned error is the one the caller puts on the exception event, wrapped in a marked carrier when the original has nowhere for the mark to live. */
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
            "method": method,
            "path":   path,
        },
    )

    clientCancelled := true == errors.Is(handlerErr, context.Canceled) &&
        nil != httpRequest && nil != httpRequest.Context().Err()

    httpException := exception.AsHttpException(handlerErr)

    if true == clientCancelled {
        requestLogger.Warning("request cancelled by client", logContext)
    } else if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() {
        requestLogger.Warning(message, logContext)
    } else {
        requestLogger.Error(message, logContext)
    }

    return exception.Logged(handlerErr)
}

/* normalizeBodyLimitError maps a *MaxBytesError onto a 413 HttpException; any other error is returned untouched. */
func normalizeBodyLimitError(handlerErr error) error {
    if nil == handlerErr {
        return handlerErr
    }

    var maxBytesError *nethttp.MaxBytesError
    if true == errors.As(handlerErr, &maxBytesError) {
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
