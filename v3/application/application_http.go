package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "sync"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/cache"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

func (instance *Application) RegisterHttpRoute(
    method string,
    pattern string,
    handler httpcontract.Handler,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register http routes after boot", nil, nil))
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

func (instance *Application) bootHttp() {
    kernelInstance := instance.kernel

    for _, registrar := range instance.httpRouteRegistrars {
        registrar(kernelInstance)
    }
}

func (instance *Application) runHttp(
    ctx context.Context,
) error {
    eventDispatcher := instance.kernel.EventDispatcher()

    if true == instance.kernel.DebugMode() {
        http.RegisterKernelHttpProfilerListener(eventDispatcher)
    }

    http.RegisterKernelResponseNormalizerListener(eventDispatcher)
    http.RegisterKernelTerminateAccessLogListener(eventDispatcher)
    http.RegisterKernelExceptionListener(eventDispatcher, instance.kernel.DebugMode())

    configuration := instance.configuration

    httpKernel := instance.kernel.HttpKernel()
    httpKernel.Use(
        instance.httpMiddlewares.all(instance.kernel)...,
    )

    httpHandler := httpKernel.ServeHttp(instance.kernel.ServiceContainer())

    /* wrap last-to-first so the first registered decorator ends up outermost */
    for index := len(instance.httpHandlerDecorators) - 1; 0 <= index; index-- {
        httpHandler = instance.httpHandlerDecorators[index](httpHandler)
    }

    httpServer := &nethttp.Server{
        Addr:    configuration.Http().Address(),
        Handler: httpHandler,
    }

    applyHttpServerTimeouts(httpServer, configuration)

    logger := logging.LoggerMustFromContainer(instance.kernel.ServiceContainer())

    instance.warnOnUnboundedDefaultCacheBackend(logger)

    /* net/http runs these the moment Shutdown is called, on their own goroutines, so a streaming handler is released while the server drains the rest; wrapping each one recovers a panicking hook through the framework logger (a bare goroutine would otherwise crash the drain) and counts it into shutdownHooksDone so runHttp can join the hooks before returning */
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

    select {
    case <-ctx.Done():
        shutdownContext, cancel := context.WithTimeout(context.Background(), resolveHttpShutdownTimeout(configuration))
        defer cancel()

        shutdownErr := httpServer.Shutdown(shutdownContext)

        /* Shutdown starts the shutdown hooks on their own goroutines but never waits for them, and it reports quiescent at once when the only open connections are hijacked (websocket), so join the hooks — within the same shutdown budget — before returning, otherwise the process exits while a hook is still releasing those handlers */
        waitForHttpShutdownHooks(&shutdownHooksDone, shutdownContext)

        if nil != shutdownErr {
            logger.Error(
                "http server shutdown error",
                exception.LogContext(shutdownErr),
            )

            return shutdownErr
        }

        return nil

    case err := <-errorChannel:
        if nil != err && false == errors.Is(err, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(err),
            )

            return err
        }

        return nil
    }
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

/* wrapHttpShutdownHook adapts a shutdown hook for net/http's RegisterOnShutdown, which starts each hook on its own goroutine and never joins it. The wrapper counts the hook into hooksDone so runHttp can wait for it, and recovers a panicking hook through the framework logger so it is contained like every other extension point instead of hard-crashing the drain on a bare goroutine. */
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

/* recoverHttpShutdownHook contains a panicking shutdown hook by logging it and returning. It keeps its own shape rather than reusing logging.LogOnRecover because it must also strip the exit code an *exception.ExitError carries: a shutdown hook runs on its own goroutine the instant Shutdown begins, while the server is still draining, so nothing it panics with may be allowed to travel on and end the process there — that would cut the in-flight requests the drain exists to finish, skip the remaining hooks and skip Application.Close(). A hook asking for an exit code is therefore recorded as a plain error: the process is already on its way down, and the code a hook cannot deliver is not worth the requests it would cut. */
func recoverHttpShutdownHook(logger loggingcontract.Logger) {
    recoveredValue := recover()
    if nil == recoveredValue {
        return
    }

    logging.LogError(logger, httpShutdownHookError(recoveredValue))
}

/* the *exception.ExitError case is listed first because it also satisfies error, and it is unwrapped to the error it carries so the exit code is dropped while the diagnostic is kept */
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
        return exception.NewError(value.Error(), nil, value)

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

/* waitForHttpShutdownHooks blocks until every shutdown hook has returned or the shutdown budget is spent. net/http's Shutdown returns as soon as the tracked connections drain — which for hijacked (websocket) connections is immediately — so without this join runHttp would return and the process would exit while a hook is still releasing those handlers. */
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
