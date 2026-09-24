package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "sync"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/cache"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/session"
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

/* RegisterHttpHandlerDecorator adds an outermost wrapper around the nethttp.Handler the http kernel produces. Decorators observe the full request lifecycle — including security denials and other kernel.request short-circuits that never reach the middlewares — which is where observability wrappers belong. The first registered decorator is the outermost. */
func (instance *Application) RegisterHttpHandlerDecorator(decorator applicationcontract.HttpHandlerDecorator) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http handler decorators after boot", nil, nil))
    }

    if nil == decorator {
        exception.Panic(exception.NewError("http handler decorator may not be nil", nil, nil))
    }

    instance.httpHandlerDecorators = append(instance.httpHandlerDecorators, decorator)
}

/* OnHttpShutdown registers a callback that runs as soon as the http server begins shutting down, before it waits for connections to drain. It is how an application unwinds handlers the server cannot: `http.Server.Shutdown` neither cancels the contexts of in-flight requests nor tracks hijacked connections, so a Server-Sent Events stream or a websocket blocks the whole shutdown timeout and is then cut mid-flight. Closing the hub those handlers select on (`ServerSentEventHub.Shutdown`) releases them at once. */
func (instance *Application) OnHttpShutdown(hook func()) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http shutdown hooks after boot", nil, nil))
    }

    if nil == hook {
        exception.Panic(exception.NewError("http shutdown hook may not be nil", nil, nil))
    }

    instance.httpShutdownHooks = append(instance.httpShutdownHooks, hook)
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

/* registerKernelExceptionListener installs the framework's exception renderer unless the application installed an error handler, since a registered listener takes the handler's place entirely. An http process decides where serving begins, because SetErrorHandler stays open until Kernel.ServeHttp builds the handler. */
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

    /* last-to-first, so the first registered decorator ends up outermost */
    for index := len(instance.httpHandlerDecorators) - 1; 0 <= index; index-- {
        httpHandler = instance.httpHandlerDecorators[index](httpHandler)
    }

    httpServer := &nethttp.Server{
        Addr:    configuration.Http().Address(),
        Handler: httpHandler,
    }

    applyHttpServerTimeouts(httpServer)

    logger := logging.LoggerMustFromContainer(instance.kernel.ServiceContainer())

    applyHttpServerErrorLog(httpServer, logger)

    instance.warnOnUnboundedDefaultCacheBackend(logger)

    instance.warnOnUnboundedDefaultSessionStorage(logger)

    /* net/http starts these on their own goroutines the moment Shutdown is called; each is wrapped to recover a panic through the framework logger and to be counted into shutdownHooksDone, which awaitHttpServerEnd joins */
    var shutdownHooksDone sync.WaitGroup
    for _, hook := range instance.httpShutdownHooks {
        httpServer.RegisterOnShutdown(
            wrapHttpShutdownHook(hook, &shutdownHooksDone, logger),
        )
    }

    logger.Info(
        "starting http server on `"+configuration.Http().Address()+"` with env `"+configuration.Kernel().Env()+"`",
        nil,
    )

    errorChannel := make(chan error, 1)

    go func() {
        listenAndServeErr := httpServer.ListenAndServe()
        errorChannel <- listenAndServeErr
    }()

    return awaitHttpServerEnd(
        ctx,
        httpServer,
        errorChannel,
        logger,
        configuration.Http().ShutdownTimeout(),
        httpKernel,
        &shutdownHooksDone,
    )
}

/* awaitHttpServerEnd waits for the cancelled context or the server's own failure, whichever ends the serving first. The serve error is read on the shutdown branch too, since a listen failing in the same instant may lose the select. A shutdown that outlives its budget surfaces the deadline error and exits non-zero. Shutdown does not account for hijacked connections, so the shutdown hooks and then the open request scopes are joined under the same budget. */
func awaitHttpServerEnd(
    ctx context.Context,
    httpServer *nethttp.Server,
    errorChannel chan error,
    logger loggingcontract.Logger,
    shutdownTimeout time.Duration,
    httpKernel httpcontract.Kernel,
    shutdownHooksDone *sync.WaitGroup,
) error {
    select {
    case <-ctx.Done():
        shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
        defer cancel()

        shutdownErr := httpServer.Shutdown(shutdownContext)

        /* Shutdown never waits for the hooks it starts and reports quiescent at once when only hijacked connections remain, so the hooks are joined within the same budget */
        waitForHttpShutdownHooks(shutdownHooksDone, shutdownContext)

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

/* markHttpRunErrorLogged hands the exit handler an error that already carries its record, so the http failure is rendered once. */
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

/* wrapHttpShutdownHook adapts a shutdown hook for net/http's RegisterOnShutdown, which never joins the goroutine it starts: the wrapper counts the hook into hooksDone and recovers a panic through the framework logger. */
func wrapHttpShutdownHook(
    hook func(),
    hooksDone *sync.WaitGroup,
    logger loggingcontract.Logger,
) func() {
    hooksDone.Add(1)

    return func() {
        defer hooksDone.Done()
        defer recoverHttpShutdownHook(logger)

        hook()
    }
}

/* recoverHttpShutdownHook logs a panicking shutdown hook and returns. Unlike logging.LogOnRecover it strips an *exception.ExitError's code: the hook runs while the server drains, and ending the process there would cut in-flight requests, skip the remaining hooks and skip Application.Close(). */
func recoverHttpShutdownHook(logger loggingcontract.Logger) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    logging.LogError(logger, httpShutdownHookError(recoveredValue))
}

/* the *exception.ExitError case comes first because it also satisfies error; it is unwrapped so the code is dropped and the diagnostic kept */
func httpShutdownHookError(recoveredValue any) *exception.Error {
    switch value := recoveredValue.(type) {
    case *exception.ExitError:
        if nil == value.ErrorValue() {
            return exception.NewError("http shutdown hook asked to exit", nil, nil)
        }

        return value.ErrorValue()

    case *exception.Error:
        return value

    case error:
        /* built through FromError, which renders the message under a recover: an Error() that panics here would be a second panic on a goroutine nothing above catches */
        if converted := exception.FromError(value); nil != converted {
            return converted
        }

        return exception.NewError("http shutdown hook panicked with a nil error", nil, nil)

    default:
        return exception.NewError(
            "panic in http shutdown hook",
            map[string]any{
                "value": value,
            },
            nil,
        )
    }
}

/* waitForHttpShutdownHooks blocks until every shutdown hook has returned or the budget is spent, since Shutdown returns at once for hijacked connections. */
func waitForHttpShutdownHooks(
    hooksDone *sync.WaitGroup,
    ctx context.Context,
) {
    done := make(chan struct{})

    go func() {
        hooksDone.Wait()

        close(done)
    }()

    select {
    case <-done:
    case <-ctx.Done():
    }
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
