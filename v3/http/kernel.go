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

/* MethodPolicy is the contract's type under this package's name: the policy travels through httpcontract.Kernel.SetMethodPolicy, where an application meets the kernel, and the alias keeps every caller and every composite literal written against melodyhttp.MethodPolicy compiling unchanged. */
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
    /* the number of request scopes opened and not yet closed, which is the only thing a shutdown can measure about a hijacked connection: Shutdown does not wait for one — the connection stopped being the server's the moment the handler took it — so a websocket still being served left the process reporting a clean stop it had not obtained. Atomic because it is written by every serving goroutine and read by the shutdown one. */
    openRequestScopes atomic.Int64
    /* raised when ServeHttp builds the handler, which is the moment the configuration stops being editable and starts being read by request goroutines. Atomic because the doors that read it may be called from any goroutine, and because a mutator racing the very first request is exactly what it exists to catch. */
    serving atomic.Bool
}

/* OpenRequestScopes reports how many request scopes are open right now: one per request being served, hijacked connections included, because the scope belongs to ServeHttp rather than to the connection. A shutdown reads it to tell a drained server from one that still has work inside it — net/http's own Shutdown cannot answer for a connection it has handed away. */
func (instance *Kernel) OpenRequestScopes() int64 {
    return instance.openRequestScopes.Load()
}

/* the kernel's configuration doors are BOOT-ONLY, and after the handler is built they refuse rather than corrupt. Every mutator below writes a field that each request goroutine reads without synchronization — the middleware chain, the forwarded-headers policy whose four words decide whether a session cookie is marked Secure, the method policy, the not-found and error handlers — so a call made while requests are in flight is a data race, and the route tree's maps are worse: a concurrent write there is an unrecoverable fatal error, not a torn read.

   Configuration is compiled once and then serves; it is not reconfigured under load. The refusal makes that contract enforceable rather than merely written, and it is raised on the calling goroutine, at the door, naming the door — where a race would instead surface as a corrupted read somewhere else entirely, or not at all. The application's own boot registrar refuses on the same principle once it has booted.

   The reading doors stay open: RouteDefinitions, RouteDefinition, RouteRegistry and the introspection surface are read from inside handlers — the openapi document is served that way — and freezing them would break the very route that publishes the table. */
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
/* Use appends middlewares to the chain the kernel builds around the matched handler. The chain decorates the handler path only: a response produced by an event listener — a security refusal, an error page — is dispatched through kernel.response and written before the chain is built, so cross-cutting response decoration belongs to kernel.response listeners; the cors package pairs its two doors that way. */
func (instance *Kernel) Use(middlewares ...httpcontract.Middleware) {
    instance.refuseMutationWhileServing("Use")

    instance.middlewares = append(instance.middlewares, middlewares...)
}

func (instance *Kernel) SetNotFoundHandler(handler httpcontract.Handler) {
    instance.refuseMutationWhileServing("SetNotFoundHandler")

    instance.notFoundHandler = handler
}

/* SetErrorHandler installs the application's own error rendering, and it is read at boot: the application registers the framework exception listener only when no handler is installed by then, because that listener answers every kernel.exception dispatch first and a handler behind it can never run. An installed handler therefore takes over what the listener did — negotiation, the request-id header, the validation errors payload — and when it returns nil the kernel's own default rendering answers instead. */
func (instance *Kernel) SetErrorHandler(handler httpcontract.ErrorHandler) {
    instance.refuseMutationWhileServing("SetErrorHandler")

    instance.errorHandler = handler
}

/* HasErrorHandler reports whether the application installed an error handler; the composition root reads it before deciding to register the framework exception listener. */
func (instance *Kernel) HasErrorHandler() bool {
    return nil != instance.errorHandler
}

/* SetForwardedHeadersPolicy installs the policy every forwarded-header reader consults, and it is boot-only like every door above it. A trusted-proxy entry that parses as neither a CIDR prefix nor an address is refused by name: both readers of the list skipped such an entry, so a typo narrowed the trust silently — the hop it named stopped being believed and every client behind it collapsed onto the direct peer, with no record anywhere. The list is copied rather than retained, because every request's proxy-trust decision reads it — whether X-Forwarded-Proto is believed, which is what sets the session cookie's Secure attribute — so a caller reusing its slice would rewrite that decision under the requests already being served. Every sibling configuration door copies its caller lists for the same reason. */
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

/* SetMethodPolicy installs the method policy the kernel reads on every request — whether HEAD falls back to the GET route, whether an unrouted OPTIONS is answered with the computed Allow header. Without this door the policy was a documented type with nowhere to hand it: DefaultKernelOptions built one, the kernel read one on every request, and no application could reach the field between them, so an api that had to answer 405 to OPTIONS wrote the configuration and watched it change nothing. */
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
    /* building the handler is the moment configuration stops and serving begins, so the freeze is raised here rather than at the first request: it is deterministic and it happens once. It refuses every mutator that BEGINS after the flag is stored; it is not a lock, so a mutator already past its own serving check when the store lands is not caught — but such a caller was already violating the single-threaded boot contract this flag makes enforceable, and was already racing the request goroutines. The freeze turns the common misuse into a refusal at the door rather than a corrupted read elsewhere; it does not serialize a caller that mutates configuration concurrently with serving. The router is frozen with it, through a package-private door — a router from outside this package cannot be reached that way and keeps only the written contract, which is all a foreign implementation can be held to. */
    instance.serving.Store(true)
    freezeRouterForServing(instance.router)

    return nethttp.HandlerFunc(func(rawWriter nethttp.ResponseWriter, request *nethttp.Request) {
        writer := newRecordingResponseWriter(rawWriter)

        scope := serviceContainer.NewScope()

        /* counted the instant the scope exists and released in the same defer that closes it, so the two can never disagree: what the counter reports is exactly the set of scopes a teardown would find open. It is the shutdown's only measure of a hijacked connection, which net/http stops accounting for the moment the handler takes it. */
        instance.openRequestScopes.Add(1)

        /* the scope is closed before anything that can fail, so a panic during request-logger setup cannot leak it; the logger is captured by reference and nil-guarded for the pre-setup failure path.

           The report falls back to the emergency logger rather than being dropped: the request logger is read after the scope it was installed into has closed, which is safe only because it is an override and Close leaves overrides alone. A close failure is the one thing that must never go unreported, so the path that has no request logger to name still says what happened. */
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

        /* the last guard, covering the window the main recovery defer cannot: everything between the scope opening and its own installation — the request logger, the request context override, the config and dispatcher resolutions, the routing — plus a panic raised inside the main guard itself, after it has already recovered once. Above it a panic escapes ServeHttp, net/http closes the connection with no response, the terminate listener never fires and the access-log line is lost; the comment on the session load below names the same failure mode as the reason that step was moved under the main guard.

           It is registered under the scope-close defer so the response is written while the scope is still open, and it is inert on every request the main guard already answered: recover returns nil there and nothing else is read. What it cannot restore is written where it is lost — no terminate dispatch, so no access-log line, and no kernel.exception dispatch, so no application error page. The record it files is that line's degraded substitute, which is why it carries the method and the path the main guard's twin also carries. */
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

        /* the route is matched on the path as the client SPELLED it, not on its decoded form: net/http decodes "%2F" into a separator, so "/admin%2Fusers" — one segment naming a resource called "admin/users" — became two segments and reached the "/admin/users" handler, while a proxy or WAF rule written against the raw request line matched neither spelling. splitRequestPath unescapes each segment after the split, so the value a parameter binds is still the decoded one. */
        matchPath := request.URL.EscapedPath()

        matchResult, _ := instance.router.Match(
            request.Method,
            matchPath,
            request.Host,
            scheme,
        )

        /* the contract returns a pointer and a flag, so an implementation is entitled to report "no match" as a nil result; the second call below already reads it that way. Read here without the check, a nil dereferences above the recovery defer installed further down, which leaves net/http closing the connection with no response, no terminate event and no access-log line. */
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
                /* the abort suppresses the response, not the ownership of what it holds: a response in flight may own an open file (FileResponse/ServeReader) and loses its only reference as this panic unwinds past the kernel, so both the assigned response and the one the chain shim still holds are closed before the sentinel is re-raised. Below, the same two closes run on every other panic path; invokeErrorHandlerSafely refuses to honour the sentinel at all for exactly this leak, and honouring it here without closing would be the leak that refusal names. */
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

            /* the mark is read through the door that writes it, at the depth it is written: the concrete assertion this replaced saw no mark on a marked HttpException — so the failure was rendered twice — and, lacking a nil check its sibling below already had, dereferenced a typed-nil *exception.Error on the very line that decides whether to report, inside the deferred handler that has already recovered once */
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

                /* the stack is captured here, inside the recovering defer, where the panic frames are still live; the error value alone names the symptom ("invalid memory address") but not the line that raised it, and net/http's own stack print never fires for a panic this recovery absorbs — every other recovery boundary in the framework records the same key */
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
        /* IsNilInterface and not `nil ==`: the manager is a replaceable service, and an implementation reporting "not found" with a nil pointer of its own session type hands back an interface that is not equal to nil. A bare comparison takes it for a live session, skips NewSession and publishes it; the later call on the response path dereferences it inside the recovery defer, where recover has already run, leaving ServeHttp with no response at all. */
        if true == internal.IsNilInterface(sessionInstance) {
            sessionInstance = sessionManager.NewSession()
        }

        melodyRequest.Attributes().Set(RequestAttributeSession, sessionInstance)

        /* a request path that folds to a different spelling is refused before the security dispatch below runs and before the handler: the router matched the path as sent while the access-control matcher folds it, so a request routed to a protected handler under one spelling could be authorized against the folded spelling's rule. Refused here — not routed, not authorized, not handled — the router, the firewall matchers and the access control never disagree about which resource this is. A trailing slash is not a fold and is not refused; requestPathIsCanonical states the boundary exactly. */
        if false == requestPathIsCanonical(request.URL.Path) {
            requestLogger.Warning(
                "request path refused before the handler",
                loggingcontract.Context{
                    "method": request.Method,
                    "path":   request.URL.Path,
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

        /* a urlencoded body whose read or parse failed never populated the form, so the handler would see a syntactically valid request whose form is simply empty — an oversized or malformed submission processed as an empty one and answered 200. It is refused the way the json binding path refuses the identical condition: 413 when the size limit stopped the read, 400 for a body the client broke. */
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

        /* a body-limit overflow a handler surfaced is answered 413 like every other oversized body, not 500. A handler that calls ParseMultipartForm on an upload past MaxRequestBodyBytes receives a *MaxBytesError — multipart is the one body path the kernel does not pre-read, so its refusal arrives here rather than at the pre-handler urlencoded/BindJson mapping. Left raw it is not an HttpException and renders 500 at error level; wrapped, the exception listener reads its 413 and logHandlerError files it at warning, symmetric with the other two paths. */
        finalHandlerErr = normalizeBodyLimitError(finalHandlerErr)

        /* the response the chain produced is published to the recovery defer here rather than after the error branch below: it may own an open file (FileResponse/ServeReader through the static middleware) and the defer closes only what it can see through finalResponse. Everything between this line and that assignment can panic — the exception dispatch re-raises a listener panic, and PrefersHtml reads the request — and the defer would then find the nil it closed over and leak the descriptor for the life of the process. */
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

        /* a handler that returns no response is answered with an empty 204, and it is given one here rather than deep inside writeResponse, so that kernel.response is dispatched for it like for every other outcome. A listener is the only thing that decorates a response — cross-origin headers, cache directives, the access log's status code — and a response that never reaches one comes out visibly different from the identical response written explicitly: the browser drops a nil-returning cross-origin DELETE for want of the headers its explicit-204 twin carries, and the log records status 0. */
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

/* invokeErrorHandlerSafely runs the application's error handler under the kernel's own recovery: it is called while the failed response's body is still open and held only by the caller, so a panic escaping it would unwind past every close the kernel performs and leak that body — permanently, for a body that is not an os.File. The panic is logged with the stack of its site and answered by the default error response instead; net/http's abort sentinel is treated the same way, because honoring an abort raised here would leak the same body. */
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

func (instance *Kernel) logEventDispatchError(
    logger loggingcontract.Logger,
    message string,
    dispatchErr error,
) {
    if nil == dispatchErr {
        return
    }

    /* the same reader the writer beside it uses: the concrete assertion this replaced saw no mark on a marked HttpException, nor on anything wrapping a marked error, and logged the dispatch failure a second time */
    if true == exception.IsAlreadyLogged(dispatchErr) {
        return
    }

    logger.Error(
        message,
        exception.LogContext(dispatchErr),
    )

    _ = exception.MarkLogged(dispatchErr)
}

/* logHandlerError files the one record for a handler-returned failure, under the discipline the panic recovery shares: an error something upstream already logged is not filed again, a deliberate 4xx a handler returned is a refusal recorded at warning, a client's own cancellation is named for what it is, and everything else keeps the error level.

   A handler that honours its context and returns the request context's own cancellation is reporting the client's disconnect, not a fault of its own: recorded at error it paged the operator for every abandoned slow request, in a record that read exactly like a genuine handler failure.

   The reported error is the value the caller must put on the exception event: a handler's plain errors.New carries no AlreadyLogged implementer for the mark to live on, so it comes back wrapped in a marked carrier keeping the original as its cause. The caller's own error is what still reaches the application's error handler. */
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

/* normalizeBodyLimitError maps a *MaxBytesError a handler surfaced onto a 413 HttpException so it renders as "payload too large" at warning, the way the pre-handler urlencoded and BindJson paths already answer an oversized body. Any other error is returned untouched. */
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
