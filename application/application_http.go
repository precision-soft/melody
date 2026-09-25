package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "time"

    "github.com/precision-soft/melody/cache"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/session"
)

/* RegisterHttpRoute queues one of the application's own routes. The queue drains before any module's RegisterHttpRoutes runs, so a root route wins the registration-order tie-break against a module route. */
func (instance *Application) RegisterHttpRoute(
    method string,
    pattern string,
    handler httpcontract.Handler,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http routes after boot", nil, nil))
    }

    /* the queue drains before the module phases, so a registrar queued from inside a module boot hook would never run; the door refuses during the boot window, and a module registers its routes through RegisterHttpRoutes */
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

/* errorHandlerReporter is the door through which the composition root asks a kernel whether the application installed its own error handler; a kernel without it keeps the framework exception listener. */
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

/* openRequestScopeReporter is the door through which the shutdown asks a kernel how many requests are still inside it; a kernel without it is not asked. */
type openRequestScopeReporter interface {
    OpenRequestScopes() int64
}

/* openRequestScopeCount answers the number of request scopes still open, and -1 for a kernel that cannot be asked, which is not zero: zero means everything drained. */
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

/* registerKernelHttpListeners wires the kernel's default listeners at the end of Boot in every process shape: they are inert in a console, whose dispatcher then shows the set the serving process runs. The exception listener's condition is not boot-final, so an http process decides it where serving begins. */
func (instance *Application) registerKernelHttpListeners() {
    eventDispatcher := instance.kernel.EventDispatcher()

    if true == instance.kernel.DebugMode() {
        http.RegisterKernelHttpProfilerListener(eventDispatcher)
    }

    http.RegisterKernelResponseNormalizerListener(eventDispatcher)
    http.RegisterKernelTerminateAccessLogListener(eventDispatcher)

    /* a console never reaches runHttp, so it decides here, for the dispatcher the introspection command reads */
    if config.ModeHttp != instance.runtimeFlags.Mode() {
        instance.registerKernelExceptionListener()
    }
}

/* registerKernelExceptionListener installs the framework's exception renderer unless the application installed an error handler, since a registered listener takes the handler's place entirely. An http process decides where serving begins, because SetErrorHandler stays open on the kernel after Boot, and a handler installed between Boot and Run must be the one consulted. */
func (instance *Application) registerKernelExceptionListener() {
    if true == kernelHasErrorHandler(instance.kernel.HttpKernel()) {
        return
    }

    http.RegisterKernelExceptionListener(instance.kernel.EventDispatcher(), instance.kernel.DebugMode())
}

func (instance *Application) runHttp(
    ctx context.Context,
) error {
    configuration := instance.configuration

    instance.registerKernelExceptionListener()

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

/* awaitHttpServerEnd waits for the cancelled context or the server's own failure, whichever ends the serving first. The serve error is read on the shutdown branch too, since a listen failing in the same instant may lose the select; Shutdown closes the listeners before it returns, so that receive is bounded. A shutdown that outlives its budget surfaces the deadline error and exits non-zero. Shutdown does not account for hijacked connections, so the open request scopes are drained under the same budget. */
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

        /* every phase of the wind-down runs and each failing one contributes its cause: the drain behind a failed Shutdown is the wait a hijacked connection needs */
        runErrs := make([]error, 0, 3)

        serveErr := <-errorChannel
        if nil != serveErr && false == errors.Is(serveErr, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(serveErr),
            )

            runErrs = append(runErrs, serveErr)
        }

        if nil != shutdownErr {
            logger.Error(
                "http server shutdown error",
                exception.LogContext(shutdownErr),
            )

            runErrs = append(runErrs, shutdownErr)
        }

        drainErr := awaitOpenRequestScopes(shutdownContext, httpKernel, logger)
        if nil != drainErr {
            runErrs = append(runErrs, drainErr)
        }

        return markHttpWindDownFailure(runErrs)

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

/* awaitOpenRequestScopesInterval is how often the drain re-reads the counter: short enough to add no perceptible delay, long enough not to spin. */
const awaitOpenRequestScopesInterval = 20 * time.Millisecond

/* awaitOpenRequestScopes holds the exit until every request scope the kernel opened has closed or the shutdown budget runs out, since Shutdown returns nil for a hijacked connection still being served. An expiry is an error, so the process exits non-zero; a kernel that answers -1 is not waited on. */
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

/* markHttpWindDownFailure answers the wind-down with the causes its phases reported: a lone cause as it stands, two or more joined, since each is independently actionable. */
func markHttpWindDownFailure(runErrs []error) error {
    if 0 == len(runErrs) {
        return nil
    }

    if 1 == len(runErrs) {
        return markHttpRunErrorLogged(runErrs[0])
    }

    return markHttpRunErrorLogged(errors.Join(runErrs...))
}

/* markHttpRunErrorLogged wraps a failure runHttp already wrote to the log and marks it so, so the exit handler renders it once. */
func markHttpRunErrorLogged(err error) error {
    wrappedErr := exception.FromError(err)

    _ = exception.MarkLogged(wrappedErr)

    return wrappedErr
}

/* warnOnUnboundedDefaultCacheBackend reports once at boot that the default cache carries no item ceiling: an entry cached without a ttl stays for the life of the process. The constructor's second argument is the sweep interval, not a lifetime. It is raised from the http path alone, since a command exits and takes its map with it. */
func (instance *Application) warnOnUnboundedDefaultCacheBackend(logger loggingcontract.Logger) {
    if false == instance.unboundedDefaultCacheBackend {
        return
    }

    logger.Warning(
        "the default in-memory cache backend carries no item ceiling, so a key cached without a ttl is kept until this process exits and nothing evicts it under memory pressure; register `"+cache.ServiceCacheBackend+"` with cache.NewInMemoryBackend(maxItems, cleanupInterval, clock) for a bounded one, or with a shared backend",
        nil,
    )
}

/* warnOnUnboundedDefaultSessionStorage reports once at boot the combination in which sessions grow without end: the in-process storage melody wired together with a zero lifetime. Melody mints a session for every request without a cookie, so one write on a public path, a csrf token or a flash message, lets an unauthenticated caller decide how many entries accumulate. It is raised from the http path alone. */
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
