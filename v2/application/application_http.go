package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/v2/cache"
    "github.com/precision-soft/melody/v2/config"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/session"
)

/* RegisterHttpRoute queues one of the application's own routes. The queue drains before any module's RegisterHttpRoutes runs, so where a root route and a module route meet at dispatch, the root route wins the registration-order tie-break: the composition root wrote its route against the application, not against whichever module boots beside it. */
func (instance *Application) RegisterHttpRoute(
    method string,
    pattern string,
    handler httpcontract.Handler,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http routes after boot", nil, nil))
    }

    /* the queue drains early in the boot, before the module phases: a registrar queued from inside a module boot hook would never run — a route silently absent — so the door refuses during the boot window; a module registers its routes through RegisterHttpRoutes, on the hook made for them */
    if true == instance.booting {
        exception.Panic(
            exception.NewError(
                "may not register http routes from inside a module boot hook; a module registers routes through RegisterHttpRoutes",
                nil,
                nil,
            ),
        )
    }

    instance.httpRouteRegistrars = append(
        instance.httpRouteRegistrars,
        func(kernelInstance kernelcontract.Kernel) {
            kernelInstance.HttpRouter().Handle(method, pattern, handler)
        },
    )
}

func (instance *Application) RegisterHttpMiddlewares(middlewares ...httpcontract.Middleware) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http middlewares after boot", nil, nil))
    }

    instance.httpMiddlewares.Use(middlewares...)
}

func (instance *Application) RegisterHttpMiddlewareFactories(
    factories ...MiddlewareFactory,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http middlewares after boot", nil, nil))
    }

    instance.httpMiddlewares.UseFactories(factories...)
}

/* errorHandlerReporter is the door through which the composition root asks a kernel whether the application installed its own error handler — a Has door like the logger's, so a replacement kernel that does not implement it keeps the framework exception listener unconditionally, exactly the behavior it has today. */
type errorHandlerReporter interface {
    HasErrorHandler() bool
}

func kernelHasErrorHandler(httpKernel httpcontract.Kernel) bool {
    reporter, ok := httpKernel.(errorHandlerReporter)
    if false == ok {
        return false
    }

    return reporter.HasErrorHandler()
}

/* openRequestScopeReporter is the door through which the shutdown asks a kernel how many requests are still inside it. It sits beside the contract rather than in it, like the error-handler door above: a replacement kernel that cannot answer is not interrogated, and the shutdown reports exactly what it did before. */
type openRequestScopeReporter interface {
    OpenRequestScopes() int64
}

/* openRequestScopeCount answers the number of request scopes still open, and -1 for a kernel that cannot be asked — which is not zero: zero is the answer "everything drained", and handing that back for a kernel that never counted would report a drain nobody measured. */
func openRequestScopeCount(httpKernel httpcontract.Kernel) int64 {
    reporter, ok := httpKernel.(openRequestScopeReporter)
    if false == ok {
        return -1
    }

    return reporter.OpenRequestScopes()
}

func (instance *Application) bootHttp() {
    kernelInstance := instance.kernel

    for _, registrar := range instance.httpRouteRegistrars {
        registrar(kernelInstance)
    }
}

/* registerKernelHttpListeners wires the kernel's default listeners at the end of Boot, in every process shape: the listeners are inert in a console that never dispatches a kernel event, and a dispatcher inspected there answers with the same set the serving process runs — the introspection command used to report an empty dispatcher for a correctly wired application. The conditions are boot-final by contract: an error handler is installed by boot, and debug mode is configuration. */
func (instance *Application) registerKernelHttpListeners() {
    eventDispatcher := instance.kernel.EventDispatcher()

    if true == instance.kernel.DebugMode() {
        http.RegisterKernelHttpProfilerListener(eventDispatcher)
    }

    http.RegisterKernelResponseNormalizerListener(eventDispatcher)
    http.RegisterKernelTerminateAccessLogListener(eventDispatcher)

    /* the framework exception listener answers every kernel.exception dispatch, so with it registered an error handler the application installed at boot could never run — the kernel consults the handler only when the dispatch produced no response. A handler installed by boot therefore takes the listener's place; without one the listener renders exactly as before. */
    if false == kernelHasErrorHandler(instance.kernel.HttpKernel()) {
        http.RegisterKernelExceptionListener(eventDispatcher, instance.kernel.DebugMode())
    }
}

func (instance *Application) runHttp(
    ctx context.Context,
) error {
    configuration := instance.configuration

    httpKernel := instance.kernel.HttpKernel()

    httpKernel.Use(
        instance.httpMiddlewares.all(instance.kernel)...,
    )

    httpHandler := httpKernel.ServeHttp(instance.kernel.ServiceContainer())

    httpServer := &nethttp.Server{
        Addr:    configuration.Http().Address(),
        Handler: httpHandler,
    }

    applyHttpServerTimeouts(httpServer)

    logger := logging.LoggerMustFromContainer(instance.kernel.ServiceContainer())

    applyHttpServerErrorLog(httpServer, logger)

    instance.warnOnUnboundedDefaultCacheBackend(logger)

    instance.warnOnUnboundedDefaultSessionStorage(logger)

    logger.Info(
        "starting http server on `"+configuration.Http().Address()+"` with env `"+configuration.Kernel().Env()+"`",
        nil,
    )

    errorChannel := make(chan error, 1)

    go func() {
        listenAndServeErr := httpServer.ListenAndServe()
        errorChannel <- listenAndServeErr
    }()

    return awaitHttpServerEnd(ctx, httpServer, errorChannel, logger, configuration.Http().ShutdownTimeout(), httpKernel)
}

/* awaitHttpServerEnd waits for whichever ends the serving first: the cancelled context or the server's own failure. The serve error is read even on the shutdown branch — when the listen fails in the same instant the context is cancelled, the select's choice of branch is arbitrary, and taking the shutdown branch used to discard the real failure, so a process that never served a byte reported a clean shutdown. Shutdown closes the listeners before it returns, so the serve goroutine has already been released and the receive is bounded. A shutdown that outlives its configured budget surfaces the deadline error and the process exits non-zero on purpose: the overrun is the operator's signal that draining hung, not a success to smooth over.

   Shutdown answers for the connections the server still owns, and only for those: a handler that hijacked its connection took it out of the server's accounting, so Shutdown returns immediately and reports success while that handler runs on. The request scopes the kernel opened are what remains measurable about them, and they are drained under the same budget for the same reason the budget exists. */
func awaitHttpServerEnd(
    ctx context.Context,
    httpServer *nethttp.Server,
    errorChannel chan error,
    logger loggingcontract.Logger,
    shutdownTimeout time.Duration,
    httpKernel httpcontract.Kernel,
) error {
    select {
    case <-ctx.Done():
        shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
        defer cancel()

        shutdownErr := httpServer.Shutdown(shutdownContext)

        serveErr := <-errorChannel
        if nil != serveErr && false == errors.Is(serveErr, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(serveErr),
            )

            return markHttpRunErrorLogged(serveErr)
        }

        if nil != shutdownErr {
            logger.Error(
                "http server shutdown error",
                exception.LogContext(shutdownErr),
            )

            return markHttpRunErrorLogged(shutdownErr)
        }

        drainErr := awaitOpenRequestScopes(shutdownContext, httpKernel, logger)
        if nil != drainErr {
            return markHttpRunErrorLogged(drainErr)
        }

        return nil

    case err := <-errorChannel:
        if nil != err && false == errors.Is(err, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(err),
            )

            return markHttpRunErrorLogged(err)
        }

        return nil
    }
}

/* awaitOpenRequestScopesInterval is how often the drain re-reads the counter. It is short enough that an ordinary drain adds no perceptible delay to the exit and long enough that the wait is not a spin: the loop exists to bound a wait, not to time it precisely. */
const awaitOpenRequestScopesInterval = 20 * time.Millisecond

/* awaitOpenRequestScopes holds the exit until every request scope the kernel opened has closed, or until the shutdown budget the caller already opened runs out. It is what makes the stop melody reports the stop it obtained: Shutdown drains the connections the server owns and returns nil for a hijacked one, so a websocket still being served — its request scope, its session, everything it holds — used to sit under a container that was closing while the process announced a clean stop and exited zero.

   An expiry is an error rather than a warning, for the reason the budget overrun above is one: a drain that did not finish is the operator's signal, and the process exiting non-zero is how they receive it. A kernel that cannot be asked is not waited on at all — the answer -1 means "no measurement", and waiting on a number nobody maintains would hang every shutdown of a replacement kernel. */
func awaitOpenRequestScopes(
    ctx context.Context,
    httpKernel httpcontract.Kernel,
    logger loggingcontract.Logger,
) error {
    if 0 > openRequestScopeCount(httpKernel) {
        return nil
    }

    ticker := time.NewTicker(awaitOpenRequestScopesInterval)
    defer ticker.Stop()

    for {
        openScopes := openRequestScopeCount(httpKernel)
        if 0 >= openScopes {
            return nil
        }

        select {
        case <-ctx.Done():
            drainErr := exception.NewError(
                "http shutdown left request scopes open",
                exceptioncontract.Context{
                    "openRequestScopes": openScopes,
                    "reason":            "a hijacked connection is not drained by the http server's own shutdown, so its handler is still running",
                },
                ctx.Err(),
            )

            logger.Error("http server shutdown error", exception.LogContext(drainErr))

            return drainErr

        case <-ticker.C:
        }
    }
}

/* markHttpRunErrorLogged wraps a failure runHttp already wrote to the log and marks it so: the caller turns it into the process-ending panic, and without the mark the exit handler would render the same failure a second time. */
func markHttpRunErrorLogged(err error) error {
    wrappedErr := exception.FromError(err)

    _ = exception.MarkLogged(wrappedErr)

    return wrappedErr
}

/* warnOnUnboundedDefaultCacheBackend reports, once at boot, that the cache melody wired by default carries no item ceiling. Whether an entry ever leaves the map is then decided entirely by the caller: a key cached with a positive ttl is reclaimed by the sweep, and one cached without stays for as long as the process lives, with nothing to evict it under memory pressure. The constructor's second argument sets how often that sweep runs, not how long an entry lives. The warning is raised from the http path alone on purpose: a command runs and exits, taking its map with it, so there is genuinely nothing to warn a cli invocation about, and a warning it cannot act on would only teach it to ignore the ones it can. */
func (instance *Application) warnOnUnboundedDefaultCacheBackend(logger loggingcontract.Logger) {
    if false == instance.unboundedDefaultCacheBackend {
        return
    }

    logger.Warning(
        "the default in-memory cache backend carries no item ceiling, so a key cached without a ttl is kept until this process exits and nothing evicts it under memory pressure; register `"+cache.ServiceCacheBackend+"` with cache.NewInMemoryBackend(maxItems, cleanupInterval, clock) for a bounded one, or with a shared backend",
        nil,
    )
}

/* warnOnUnboundedDefaultSessionStorage reports, once at boot, the one combination in which sessions grow without anything ever reclaiming them: the storage melody wired itself, which lives in this process and which nothing outside it can expire, together with a lifetime of zero, which asks it to keep every entry forever.

   Either half alone is a deliberate and reasonable choice. A shared storage with no expiry is the operator's to prune, and the in-memory one with a lifetime set reclaims on its own. Together they are neither, and the growth does not come from anything the application wrote: melody mints a session for every request that arrives without a session cookie, so a single write on a public path — a csrf token, a flash message, a locale — turns every such request into a permanent entry, and an unauthenticated caller decides how many arrive.

   It is raised from the http path alone, the way the cache warning is: a command builds its map, runs and takes it away with it. */
func (instance *Application) warnOnUnboundedDefaultSessionStorage(logger loggingcontract.Logger) {
    if false == instance.defaultInMemorySessionStorage {
        return
    }

    if 0 != instance.configuration.Http().SessionTtl() {
        return
    }

    logger.Warning(
        "the default in-memory session storage is paired with an unbounded session ttl, so every request that arrives without a session cookie can leave an entry that is kept until this process exits; set `"+config.HttpSessionTtlKey+"` to the lifetime this deployment wants, or register `"+session.ServiceSessionStorage+"` with a shared storage an operator can expire",
        nil,
    )
}
