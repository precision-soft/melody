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

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

/* MethodPolicy is the contract's type under this package's name, so callers written against melodyhttp.MethodPolicy keep compiling; the policy travels through httpcontract.Kernel.SetMethodPolicy. */
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
    /* the request scopes opened and not yet closed, the only thing a shutdown can measure about a hijacked connection, which net/http's Shutdown does not wait for. Atomic: every serving goroutine writes it and the shutdown reads it. */
    openRequestScopes atomic.Int64
}

/* OpenRequestScopes reports how many request scopes are open right now: one per request being served, hijacked connections included, because the scope belongs to ServeHttp rather than to the connection. A shutdown reads it to tell a drained server from one that still has work inside it — net/http's own Shutdown cannot answer for a connection it has handed away. */
func (instance *Kernel) OpenRequestScopes() int64 {
    return instance.openRequestScopes.Load()
}

/* Use appends middlewares to the chain the kernel builds around the matched handler. The chain decorates the handler path only: a response produced by an event listener — a security refusal, an error page — is dispatched through kernel.response and written before the chain is built, so cross-cutting response decoration belongs to kernel.response listeners; the cors package pairs its two doors that way. */
func (instance *Kernel) Use(middlewares ...httpcontract.Middleware) {
    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.notFoundHandler = handler
}

/* SetErrorHandler installs the application's own error rendering, and it is read at boot: the application registers the framework exception listener only when no handler is installed by then, because that listener answers every kernel.exception dispatch first and a handler behind it can never run. An installed handler therefore takes over what the listener did — negotiation, the request-id header, the validation errors payload — and when it returns nil the kernel's own default rendering answers instead. */
func (instance *Kernel) SetErrorHandler(handler httpcontract.ErrorHandler) {
    instance.errorHandler = handler
}

/* HasErrorHandler reports whether the application installed an error handler; the composition root reads it before deciding to register the framework exception listener. */
func (instance *Kernel) HasErrorHandler() bool {
    return nil != instance.errorHandler
}

/* SetForwardedHeadersPolicy copies the trusted proxy list instead of retaining the caller's slice: every request's proxy-trust decision reads it, and a caller reusing its slice would rewrite that decision mid-serving as a data race. */
func (instance *Kernel) SetForwardedHeadersPolicy(policy httpcontract.ForwardedHeadersPolicy) {
    policy.TrustedProxyList = copyStringList(policy.TrustedProxyList)
    instance.options.ForwardedHeadersPolicy = policy
}

func (instance *Kernel) SetSessionCookiePolicy(policy httpcontract.SessionCookiePolicy) {
    instance.options.SessionCookiePolicy = policy
}

/* SetMethodPolicy installs the method policy the kernel reads on every request: whether HEAD falls back to the GET route and whether an unrouted OPTIONS is answered with the computed Allow header. */
func (instance *Kernel) SetMethodPolicy(policy httpcontract.MethodPolicy) {
    instance.options.MethodPolicy = policy
}

func copyStringList(values []string) []string {
    if nil == values {
        return nil
    }

    return append(make([]string, 0, len(values)), values...)
}

func (instance *Kernel) ServeHttp(serviceContainer containercontract.Container) nethttp.Handler {
    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        /* counted the instant the scope exists and released in the defer that closes it, so the counter reports exactly the scopes a teardown would find open. */
        instance.openRequestScopes.Add(1)

        /* the scope is closed before anything that can fail, so a panic during request-logger setup cannot leak it; the logger is captured by reference and nil-guarded for the pre-setup failure path. A close failure falls back to the emergency logger rather than being dropped; reading the request logger after the scope closed is safe because it is an override and Close leaves overrides alone. */
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

        /* the last guard, covering what the main recovery defer cannot: the window between the scope opening and the main guard's installation, and a panic raised inside the main guard after it recovered once. Above it a panic escapes ServeHttp and net/http closes the connection with no response. It is registered under the scope-close defer so the response is written while the scope is open, it is inert when the main guard already answered, and its record carries the method and path because no terminate dispatch or access-log line follows. */
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
                            /* named explicitly: the emergency logger this falls back to when the request logger is what failed does not inject it */
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

            /* written directly rather than through writeResponse: that path resolves the logger from the runtime, which does not exist in this window and files an emergency record of its own for the nil, and it persists the session, which has nothing to do with a request that never got that far */
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
            /* the raw writer is passed rather than the recording wrapper: net/http detects the server response through an unexported-method assertion with no Unwrap, so a wrapper loses the requestTooLarge connection-close signal on oversized bodies */
            request.Body = nethttp.MaxBytesReader(rawWriter, request.Body, int64(maxBodyBytes))
        }

        scheme := detectSchemeWithForwardedHeadersPolicy(request, instance.options.ForwardedHeadersPolicy)

        matchResult, _ := instance.router.Match(
            request.Method,
            request.URL.Path,
            request.Host,
            scheme,
        )

        /* the contract returns a pointer and a flag, so an implementation may report no match as a nil result; read without the check, a nil would dereference above the recovery defer and net/http would close the connection with no response. */
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

        /* published after the route attributes so a route cannot replace what the kernel owns. The scheme is the one resolved through the configured forwarded-headers policy, which a listener has no access to: re-detecting without it reports http for every request a trusted proxy terminated as https. */
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

        /* the last response the middleware chain had in flight, published by the recording shim under every layer; the recovery defer reads it for the window in which the chain has answered but finalResponse has not been assigned yet */
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

            /* net/http documents this sentinel as "abort the connection and suppress the log", and only a panic reaching its own serve loop closes the connection without a response; converted into an error it would answer an aborted upload with a 500 and an error line. The identity check matches net/http's own, so an application error merely wrapping the sentinel is unaffected. */
            if nethttp.ErrAbortHandler == recoveredValue {
                /* the abort suppresses the response, not the ownership of what it holds: a response in flight may own an open file and loses its only reference as this panic unwinds, so the assigned response and the one the chain shim holds are both closed before the sentinel is re-raised, as on every other panic path. */
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

            /* the response that was in flight when the panic unwound; the error response replaces it below, and nothing else holds a reference to it */
            panickedResponse := finalResponse

            /* the mark is read through the door that writes it, at the depth it is written, so a marked HttpException is not rendered twice and a typed-nil *exception.Error is not dereferenced inside a handler that has already recovered once */
            alreadyLogged := exception.IsAlreadyLogged(recoveredErr)

            /* the value the exception dispatch carries: a runtime panic recovers to a runtime.Error, which has nowhere for the mark to live, so the report hands back a marked carrier keeping it as its cause. The error handler and the debug message below keep the recovered value itself. */
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

                /* the stack is captured here, inside the recovering defer, where the panic frames are still live; net/http's own stack print never fires for a panic this recovery absorbs, and every other recovery boundary in the framework records the same key */
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

            /* the response built before the panic may own an open file (FileResponse/ServeReader) and is about to lose its only reference, so it is closed unless the exception handler chose to keep it */
            if nil != panickedResponse && panickedResponse != exceptionEvent.Response() {
                closeDiscardedResponseBody(panickedResponse, requestLogger)
            }

            /* an outer middleware that panicked after its next() returned unwound the stack before finalResponse was assigned, so the response the chain produced is held only by the recording shim; it is closed here. On the panic paths where finalResponse was already assigned the discard above owns the close — closing the recorded response there too would close a body a wrapping middleware may share with the response being served. */
            if nil == panickedResponse && nil != chainResponse && chainResponse != exceptionEvent.Response() {
                closeDiscardedResponseBody(chainResponse, requestLogger)
            }

            finalResponse = exceptionEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelExceptionErr = eventDispatcher.DispatchName(
                runtimeInstance,
                kernelcontract.EventKernelResponse,
                kernelResponseEvent,
            )
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelExceptionErr)

            /* the swapped-out response body is closed so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            finalResponse = writeResponse(
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

        /* the session is loaded here, after the recovery defer is installed, and must not move back up with the rest of the request setup: both Manager.Session and Manager.NewSession turn a storage outage into a panic, and above the guard that panic escapes ServeHttp — net/http closes the connection with no response, the terminate listener never fires and the access-log line is lost */
        sessionManager = session.SessionMustFromContainer(serviceContainer)

        cookie, _ := request.Cookie(session.SessionCookieName)
        if nil != cookie {
            sessionInstance = sessionManager.Session(cookie.Value)
        }
        /* IsNilInterface and not `nil ==`: the manager is a replaceable service, and an implementation reporting not found with a nil pointer of its own session type hands back an interface that is not nil. A bare comparison would publish it, and the response path would dereference it inside the recovery defer. */
        if true == internal.IsNilInterface(sessionInstance) {
            sessionInstance = sessionManager.NewSession()
        }

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        /* a request path that folds to a different spelling is refused after the route is matched and before the security dispatch and the handler: the router matches the path as sent while the access-control matcher folds it, so the two could disagree about which resource this is. A trailing slash is not a fold (requestPathIsCanonical states the boundary), and a separator the client encoded is refused too (requestPathCarriesEncodedSeparator). */
        if false == requestPathIsCanonical(request.URL.Path) || true == requestPathCarriesEncodedSeparator(request.URL.RawPath) {
            requestLogger.Warning(
                "request path refused before the handler",
                loggingcontract.Context{
                    "method":  request.Method,
                    "path":    request.URL.Path,
                    "rawPath": request.URL.RawPath,
                },
            )

            finalResponse = renderErrorResponse(runtimeInstance, melodyRequest, nethttp.StatusBadRequest, "bad request", nil)

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* the swapped-out response body is closed so a file-backed body is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            finalResponse = writeResponse(
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

        /* a urlencoded body whose read or parse failed never populated the form, and an empty form must not pass for the submission: it is refused as the json binding path refuses it, 413 when the size limit stopped the read and 400 for a body the client broke. */
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

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* the swapped-out response body is closed so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            finalResponse = writeResponse(
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

        kernelRequestEvent := NewKernelRequestEvent(runtimeInstance, melodyRequest)
        _, eventKernelRequestErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelRequest, kernelRequestEvent)
        instance.logEventDispatchError(requestLogger, "kernel request error", eventKernelRequestErr)

        /* the kernel.request dispatch fails closed when it aborted with an error and no listener produced a response: the dispatcher stops at the first failing listener, so listeners behind it — the access-control listener among them — never ran, and proceeding to the handler would treat a partially-processed request as authorized. A dispatch that skipped a listener marked required is refused too: a listener stopping propagation is entitled to answer the request, but not with access control never consulted, so the response it produced is dropped for the error page. The error is judged on itself rather than through its cause chain, so an application event dispatched by a listener that skips a required listener of its own stays an ordinary listener failure. */
        _, requiredListenerSkipped := eventKernelRequestErr.(*event.RequiredListenerSkippedError)

        if nil != eventKernelRequestErr && (true == requiredListenerSkipped || nil == kernelRequestEvent.Response()) {
            statusCode := nethttp.StatusInternalServerError
            message := "internal server error"
            if true == debugMode {
                message = debugErrorMessage(eventKernelRequestErr)
            }

            /* the response the stopping listener produced is replaced below and reaches no writer, so a file-backed body would be leaked */
            closeDiscardedResponseBody(kernelRequestEvent.Response(), requestLogger)

            kernelRequestEvent.SetResponse(
                renderErrorResponse(runtimeInstance, melodyRequest, statusCode, message, nil),
            )
        }

        if nil != kernelRequestEvent.Response() {
            finalResponse = kernelRequestEvent.Response()

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* the swapped-out response body is closed so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            finalResponse = writeResponse(
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

                        /* Allow advertises only the synthetic methods the kernel honors under the configured MethodPolicy: OPTIONS is answered automatically only when AutomaticOptions is set, and HEAD falls back to GET only when HeadFallbackToGet is set, so listing either under the opposite configuration promises a method that in fact returns 405. A method the route declares explicitly is already added from allowedMethods above. */
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

        /* the kernel.controller dispatch fails closed on the same terms as the kernel.request path: the dispatcher stops at the first failing listener, so a required listener behind it — marked through RequiredListenerRegistrar — never ran, and proceeding to the handler would treat a partially-processed request as authorized */
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

            kernelResponseEvent := NewKernelResponseEvent(melodyRequest, finalResponse)
            _, eventKernelResponseErr := eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelResponse, kernelResponseEvent)
            instance.logEventDispatchError(requestLogger, "kernel response error", eventKernelResponseErr)

            /* the swapped-out response body is closed so a file-backed body (FileResponse/ServeReader) is not leaked */
            if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
                closeDiscardedResponseBody(finalResponse, requestLogger)
            }

            finalResponse = kernelResponseEvent.Response()
            finalResponse = writeResponse(
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
        finalHandler := instance.buildHandler(baseHandler, middlewaresSnapshot, func(response httpcontract.Response) {
            chainResponse = response
        })

        response, finalHandlerErr := finalHandler(runtimeInstance, writer, melodyRequest)

        /* a body-limit overflow a handler surfaced is answered 413 like every other oversized body. Multipart is the one body path the kernel does not pre-read, so a ParseMultipartForm past MaxRequestBodyBytes returns a *MaxBytesError here; wrapped, the exception listener reads its 413 and logHandlerError files it at warning. */
        finalHandlerErr = normalizeBodyLimitError(finalHandlerErr)

        /* the response the chain produced is published to the recovery defer here, before the error branch below: it may own an open file, the defer closes only what it sees through finalResponse, and the exception dispatch and PrefersHtml below can panic. */
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

        /* a handler that returns no response is given an empty 204 here rather than inside writeResponse, so kernel.response is dispatched for it like for every other outcome: listeners are what add the cross-origin headers, the cache directives and the access log's status code. */
        if true == internal.IsNilInterface(response) {
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

        /* the swapped-out response body is closed so a file-backed body (FileResponse/ServeReader) is not leaked */
        if nil != finalResponse && finalResponse != kernelResponseEvent.Response() {
            closeDiscardedResponseBody(finalResponse, requestLogger)
        }

        finalResponse = kernelResponseEvent.Response()
        finalResponse = writeResponse(
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

/* invokeErrorHandlerSafely runs the application's error handler under the kernel's own recovery: the failed response's body is still open and held only by the caller, so a panic escaping it would leak that body. The panic is logged with its stack and answered by the default error response; net/http's abort sentinel is treated the same way, because honouring it here would leak the same body. */
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

/* normalizeBodyLimitError maps a *MaxBytesError a handler surfaced onto a 413 HttpException, rendered as "payload too large" at warning like the pre-handler body paths; any other error is returned untouched. */
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

/* logHandlerError files the one record for a handler-returned failure: an error already logged is not filed again, a deliberate 4xx is a warning, the request context's own cancellation is named as the client's, and everything else is an error, a 4xx whose validation errors blame the declaration included. The returned error is the one the caller puts on the exception event, wrapped in a marked carrier when the original has nowhere for the mark to live, so the exception listener attaches its request coordinates instead of filing a second record. */
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
    } else if nil != httpException && nethttp.StatusInternalServerError > httpException.StatusCode() && false == carriesRuleWiringError(httpException) {
        requestLogger.Warning(message, logContext)
    } else {
        requestLogger.Error(message, logContext)
    }

    return exception.Logged(handlerErr)
}

func (instance *Kernel) logEventDispatchError(
    logger loggingcontract.Logger,
    message string,
    dispatchErr error,
) {
    if nil == dispatchErr {
        return
    }

    /* the same reader the writer beside it uses, so a marked HttpException, or anything wrapping a marked error, is not logged a second time */
    if true == exception.IsAlreadyLogged(dispatchErr) {
        return
    }

    logger.Error(
        message,
        exception.LogContext(dispatchErr),
    )

    _ = exception.MarkLogged(dispatchErr)
}

func (instance *Kernel) buildHandler(
    handler httpcontract.Handler,
    middlewares []httpcontract.Middleware,
    recordChainResponse func(httpcontract.Response),
) httpcontract.Handler {
    return wrapWithMiddlewaresRecording(handler, middlewares, recordChainResponse)
}

var _ httpcontract.Kernel = (*Kernel)(nil)
