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

    instance.warnOnUnboundedDefaultSessionStorage(logger)

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

        drainErr := awaitOpenRequestScopes(shutdownContext, httpKernel, logger)
        if nil != drainErr {
            return drainErr
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

/* openRequestScopeReporter is the door through which the shutdown asks a kernel how many requests are
still inside it. It sits beside the contract rather than in it: a replacement kernel that cannot answer
is not interrogated, and the shutdown reports exactly what it did before. */
type openRequestScopeReporter interface {
    OpenRequestScopes() int64
}

/* openRequestScopeCount answers the number of request scopes still open, and -1 for a kernel that cannot
be asked — which is not zero: zero is the answer "everything drained", and handing that back for a kernel
that never counted would report a drain nobody measured. */
func openRequestScopeCount(httpKernel httpcontract.Kernel) int64 {
    reporter, ok := httpKernel.(openRequestScopeReporter)
    if false == ok {
        return -1
    }

    return reporter.OpenRequestScopes()
}

/* awaitOpenRequestScopesInterval is how often the drain re-reads the counter. It is short enough that an ordinary drain adds no perceptible delay to the exit and long enough that the wait is not a spin: the loop exists to bound a wait, not to time it precisely. */
const awaitOpenRequestScopesInterval = 20 * time.Millisecond

/* awaitOpenRequestScopes holds the exit until every request scope the kernel opened has closed, or until the shutdown budget the caller already opened runs out. It is what makes the stop melody reports the stop it obtained: Shutdown drains the connections the server owns and returns nil for a hijacked one, so a websocket still being served — its request scope, its session, everything it holds — used to sit under a container that was closing while the process announced a clean stop and exited zero. The shutdown hooks release the streaming handlers; this waits for the scopes those handlers still hold to actually close.

An expiry is an error rather than a warning: a drain that did not finish is the operator's signal, and the process exiting non-zero is how they receive it. A kernel that cannot be asked is not waited on at all — the answer -1 means "no measurement", and waiting on a number nobody maintains would hang every shutdown of a replacement kernel. */
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
