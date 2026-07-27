package application

import (
    "context"
    "errors"
    nethttp "net/http"

    "github.com/precision-soft/melody/v2/cache"
    "github.com/precision-soft/melody/v2/config"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/session"
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

    httpServer := &nethttp.Server{
        Addr:    configuration.Http().Address(),
        Handler: httpHandler,
    }

    applyHttpServerTimeouts(httpServer, configuration)

    logger := logging.LoggerMustFromContainer(instance.kernel.ServiceContainer())

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

    select {
    case <-ctx.Done():
        shutdownContext, cancel := context.WithTimeout(context.Background(), resolveHttpShutdownTimeout(configuration))
        defer cancel()

        shutdownErr := httpServer.Shutdown(shutdownContext)
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
