package application

import (
    "context"
    "errors"
    "maps"
    nethttp "net/http"
    "slices"
    "sync"
    "sync/atomic"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/http/middleware"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/httpkernelseam"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* RegisterHttpRoute queues one of the application's own routes with what the options declare, as Router.Handle registers one, empty Methods answering every method; the lists and the maps are copied here, so the value can be reused before the boot registers it. The queue drains before any module's RegisterHttpRoutes runs, so a root route wins the registration-order tie-break against a module route. */
func (instance *Application) RegisterHttpRoute(
    pattern string,
    handler httpcontract.Handler,
    options httpcontract.RouteOptions,
) {
    if true == instance.registrationClosed() {
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

    options.Methods = slices.Clone(options.Methods)
    options.Schemes = slices.Clone(options.Schemes)
    options.Locales = slices.Clone(options.Locales)
    options.Requirements = maps.Clone(options.Requirements)
    options.Defaults = maps.Clone(options.Defaults)
    options.Attributes = maps.Clone(options.Attributes)
    options.Protection.SecurityHeadersDisabled = slices.Clone(options.Protection.SecurityHeadersDisabled)

    instance.httpRouteRegistrars = append(
        instance.httpRouteRegistrars,
        func(kernelInstance kernelcontract.Kernel) {
            kernelInstance.HttpRouter().Handle(pattern, handler, options)
        },
    )
}

func (instance *Application) RegisterHttpMiddlewares(middlewares []httpcontract.Middleware) {
    if true == instance.registrationClosed() {
        exception.Panic(exception.NewError("may not register http middlewares after boot", nil, nil))
    }

    instance.httpMiddlewares.forModule(internal.EnvironmentKeyOwnerApplication).Use(applicationcontract.MiddlewareRegistration{Middlewares: middlewares})
}

func (instance *Application) RegisterHttpMiddlewareFactories(factories []middlewarepipeline.MiddlewareFactory) {
    if true == instance.registrationClosed() {
        exception.Panic(exception.NewError("may not register http middleware factories after boot", nil, nil))
    }

    instance.httpMiddlewares.forModule(internal.EnvironmentKeyOwnerApplication).Use(applicationcontract.MiddlewareRegistration{Factories: factories})
}

/* RegisterHttpHandlerDecorator adds an outermost wrapper around the nethttp.Handler the http kernel produces. Decorators observe the full request lifecycle — including security denials and other kernel.request short-circuits that never reach the middlewares — which is where observability wrappers belong. The first registered decorator is the outermost. */
func (instance *Application) RegisterHttpHandlerDecorator(decorator applicationcontract.HttpHandlerDecorator) {
    if true == instance.registrationClosed() {
        exception.Panic(exception.NewError("may not register http handler decorators after boot", nil, nil))
    }

    if nil == decorator {
        exception.Panic(exception.NewError("http handler decorator may not be nil", nil, nil))
    }

    instance.httpHandlerDecorators = append(instance.httpHandlerDecorators, decorator)
}

/* decorateHttpHandler wraps the handler the http kernel built in the registered decorators, last to first, so the first registered decorator ends up outermost; a decorator answering nil, typed nil included, is refused before the next one wraps it, since a nil handler serves net/http's default mux */
func (instance *Application) decorateHttpHandler(httpHandler nethttp.Handler) (nethttp.Handler, error) {
    for index := len(instance.httpHandlerDecorators) - 1; 0 <= index; index-- {
        httpHandler = instance.httpHandlerDecorators[index](httpHandler)

        if true == internal.IsNilInterface(httpHandler) {
            return nil, exception.NewError(
                "http handler decorator answered nil",
                exceptioncontract.Context{"index": index},
                nil,
            )
        }
    }

    return httpHandler, nil
}

/* OnHttpShutdown registers a callback that runs as soon as the http server begins shutting down, before it waits for connections to drain, handed the shutdown's context, which carries the shutdown deadline and is done when its budget ends. It is how an application unwinds handlers the server cannot: `http.Server.Shutdown` neither cancels the contexts of in-flight requests nor tracks hijacked connections, so a Server-Sent Events stream or a websocket blocks the whole shutdown timeout and is then cut mid-flight. Closing the hub those handlers select on releases them at once. */
func (instance *Application) OnHttpShutdown(hook func(shutdownContext context.Context)) {
    if true == instance.registrationClosed() {
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

/* registerKernelHttpListeners wires the kernel's default listeners at the end of Boot in every process shape: they are inert in a console, whose dispatcher then shows the set the serving process runs. The built-in exception listener is not among them: the http kernel installs it when it builds its handler, and a console declares it among the serving-process listeners. */
func (instance *Application) registerKernelHttpListeners() {
    eventDispatcher := instance.kernel.EventDispatcher()

    if true == instance.kernel.DebugMode() {
        http.RegisterKernelHttpProfilerListener(eventDispatcher)
    }

    http.RegisterKernelResponseNormalizerListener(eventDispatcher)
    http.RegisterKernelTerminateAccessLogListener(eventDispatcher)

    if nil != instance.httpMiddlewares && nil != instance.httpMiddlewares.securityHeaders {
        middleware.RegisterSecurityHeadersResponseListener(eventDispatcher, instance.httpMiddlewares.securityHeaders)
    }
}

/* bootHttpPipeline judges the session manager's storage timeout and builds the middleware pipeline under the boot runtime, once */
func (instance *Application) bootHttpPipeline(bootRuntime containercontract.Runtime) {
    if refusedErr := refuseSessionStorageTimeout(bootRuntime); nil != refusedErr {
        exception.Panic(exception.FromError(refusedErr))
    }

    instance.installHttpMiddlewares()
}

/* installHttpMiddlewares builds the pipeline. A middleware factory runs here with the writable router, so the walk Boot made is made again over what it registered. */
func (instance *Application) installHttpMiddlewares() {
    instance.servedHttpKernel().InstallMiddlewares(httpkernelseam.NewToken(), instance.httpMiddlewares.all(instance.kernel))

    instance.recordFirewallDoorRouteCollisions()
    instance.panicOnBootCollisions()
}

/* servedHttpKernel answers the http kernel NewApplication built; an Application built by literal gets the zero-configuration one over its kernel's router */
func (instance *Application) servedHttpKernel() *http.Kernel {
    if nil == instance.httpKernel {
        instance.httpKernel = http.NewKernel(instance.kernel.HttpRouter(), http.KernelConfig{})
    }

    return instance.httpKernel
}

/* HttpHandler boots the application and answers the handler it serves under a server of the caller's, the one Run serves with: the pipeline, the decorators and the http kernel's configuration, with the configuration and the openapi registry marked serving and the boot warnings written. A .env Run refuses is refused here too. The handler, or the refusal, is built once and answered on every call. It is refused in cli mode and once Run started, and Run is refused once the handler is out; a refused build or a panicking decorator hands nothing out and leaves the application unclaimed. The server's owner calls BeginHttpShutdown as its server begins shutting down and CloseWithContext once it stopped. */
func (instance *Application) HttpHandler() (nethttp.Handler, error) {
    instance.httpHandlerMutex.Lock()
    defer instance.httpHandlerMutex.Unlock()

    if true == instance.httpHandlerBuilt {
        return instance.httpHandler, instance.httpHandlerErr
    }

    if nil != instance.runtimeFlags && config.ModeCli == instance.runtimeFlags.Mode() {
        return nil, exception.NewError(
            "the http handler is asked of a process in cli mode",
            exceptioncontract.Context{"mode": config.ModeCli},
            nil,
        )
    }

    if false == instance.servingClaim.CompareAndSwap(servingClaimNone, servingClaimHttpHandler) && servingClaimHttpHandler != instance.servingClaim.Load() {
        return nil, exception.NewError("the http handler is asked of an application that runs its own server", nil, nil)
    }

    /* nothing handed out leaves the application unclaimed: a refused build or a panicking decorator releases the claim, so Run is refused only once a handler is out */
    handedOut := false
    defer func() {
        if false == handedOut {
            instance.servingClaim.CompareAndSwap(servingClaimHttpHandler, servingClaimNone)
        }
    }()

    _ = instance.Boot()

    markConfigurationServing(instance.configuration)
    markOpenApiRegistryServing(instance.ctx, instance.kernel.ServiceContainer())

    /* only a finished build is memoized: a panic inside a decorator leaves the next call to build again, never a nil handler answered without an error */
    built, builtErr := instance.buildHttpHandler()
    if nil != builtErr {
        instance.httpHandlerBuilt = true
        instance.httpHandlerErr = builtErr

        return nil, builtErr
    }

    logger, loggerErr := logging.LoggerFromResolver(instance.ctx, instance.kernel.ServiceContainer())
    if nil != loggerErr {
        instance.httpHandlerBuilt = true
        instance.httpHandlerErr = loggerErr

        return nil, loggerErr
    }

    instance.writeBootWarnings(logger)

    instance.httpHandlerBuilt = true
    instance.httpHandler = built.handler
    instance.httpHandlerHandedOut.Store(&handedOutHttpHandler{logger: logger, shutdownTimeout: built.shutdownTimeout})
    handedOut = true

    return instance.httpHandler, nil
}

/* BeginHttpShutdown starts the OnHttpShutdown hooks of a handed-out handler, each handed shutdownContext, once, and returns without waiting for them: the server's owner calls it just before its server's Shutdown, which neither cancels an in-flight request nor drains a hijacked connection. An application that runs its own server, or handed nothing out, starts nothing. */
func (instance *Application) BeginHttpShutdown(shutdownContext context.Context) error {
    if true == internal.IsNilInterface(shutdownContext) {
        return exception.NewError("the shutdown context is nil", exceptioncontract.Context{"door": "Application.BeginHttpShutdown"}, nil)
    }

    handedOut := instance.handedOutHttpHandler()
    if nil == handedOut {
        return nil
    }

    instance.startHttpShutdownHooks(shutdownContext, handedOut.logger)

    return nil
}

type handedOutHttpHandler struct {
    logger          loggingcontract.Logger
    shutdownTimeout time.Duration
}

/* handedOutHttpHandler answers the logger and the shutdown timeout a handed-out handler was built with, nil when HttpHandler handed none out */
func (instance *Application) handedOutHttpHandler() *handedOutHttpHandler {
    return instance.httpHandlerHandedOut.Load()
}

/* startHttpShutdownHooks starts each shutdown hook on its own goroutine, as a server's Shutdown does; only the first call starts them */
func (instance *Application) startHttpShutdownHooks(shutdownContext context.Context, logger loggingcontract.Logger) {
    instance.httpShutdownHooksOnce.Do(func() {
        instance.httpShutdownHookSet.shutdownContext.Store(shutdownContext)

        for _, hook := range instance.httpShutdownHooks {
            go wrapHttpShutdownHook(hook, &instance.httpShutdownHookSet, logger)()
        }
    })
}

/* windDownHandedOutHttpHandler holds the close of a handed-out handler until its shutdown hooks returned and its request scopes closed, the wait Run makes after its own server's Shutdown, bounded by the http shutdown timeout as Run bounds its own, the close's context winning when it ends earlier; the hooks are started under that bound unless BeginHttpShutdown started them. */
func (instance *Application) windDownHandedOutHttpHandler(closeContext context.Context) error {
    handedOut := instance.handedOutHttpHandler()
    if nil == handedOut {
        return nil
    }

    windDownContext, cancelWindDown := context.WithTimeout(closeContext, handedOut.shutdownTimeout)
    defer cancelWindDown()

    instance.startHttpShutdownHooks(windDownContext, handedOut.logger)

    waitForHttpShutdownHooks(&instance.httpShutdownHookSet.done, windDownContext)

    return awaitOpenRequestScopes(windDownContext, instance.httpKernel, handedOut.logger)
}

type builtHttpHandler struct {
    handler         nethttp.Handler
    serverLimits    httpServerLimits
    shutdownTimeout time.Duration
}

/* buildHttpHandler judges the limits a server serves under and answers the decorated handler; Run and HttpHandler share it, so one .env serves, or is refused, the same under both */
func (instance *Application) buildHttpHandler() (builtHttpHandler, error) {
    configuration := instance.configuration

    serverLimits, serverLimitsErr := resolveHttpServerLimits(configuration, instance.applicationConfig)
    if nil != serverLimitsErr {
        return builtHttpHandler{}, serverLimitsErr
    }

    if refusedErr := refuseScopeTeardownOutlastingHttpShutdown(configuration, instance.applicationConfig); nil != refusedErr {
        return builtHttpHandler{}, refusedErr
    }

    httpShutdownTimeout, httpShutdownTimeoutErr := resolveHttpShutdownTimeout(configuration, instance.applicationConfig)
    if nil != httpShutdownTimeoutErr {
        return builtHttpHandler{}, httpShutdownTimeoutErr
    }

    handler, handlerErr := instance.decorateHttpHandler(instance.servedHttpKernel().ServeHttp(instance.kernel.ServiceContainer()))
    if nil != handlerErr {
        return builtHttpHandler{}, handlerErr
    }

    return builtHttpHandler{
        handler:         handler,
        serverLimits:    serverLimits,
        shutdownTimeout: httpShutdownTimeout.Value,
    }, nil
}

func (instance *Application) runHttp(
    ctx context.Context,
) error {
    configuration := instance.configuration

    built, builtErr := instance.buildHttpHandler()
    if nil != builtErr {
        return builtErr
    }

    httpServer := &nethttp.Server{
        Addr:    configuration.Http().Address(),
        Handler: built.handler,
    }

    applyHttpServerTimeouts(httpServer, built.serverLimits)

    logger := logging.LoggerMustFromResolver(ctx, instance.kernel.ServiceContainer())

    applyHttpServerErrorLog(httpServer, logger)

    instance.writeBootWarnings(logger)

    /* net/http starts these on their own goroutines the moment Shutdown is called; each is wrapped to recover a panic through the framework logger and to be counted into shutdownHooksDone, which awaitHttpServerEnd joins */
    shutdownHooks := &httpShutdownHookSet{}
    for _, hook := range instance.httpShutdownHooks {
        httpServer.RegisterOnShutdown(
            wrapHttpShutdownHook(hook, shutdownHooks, logger),
        )
    }

    logger.Info(
        "starting http server on `"+configuration.Http().Address()+"` with environment `"+configuration.Kernel().Environment()+"`",
        nil,
    )

    errorChannel := make(chan error, 1)

    go func() {
        errorChannel <- instance.serveHttpServer(httpServer)
    }()

    return awaitHttpServerEnd(
        ctx,
        httpServer,
        errorChannel,
        logger,
        built.shutdownTimeout,
        instance.servedHttpKernel(),
        shutdownHooks,
    )
}

func (instance *Application) serveHttpServer(httpServer *nethttp.Server) error {
    if nil != instance.httpListener {
        return httpServer.Serve(instance.httpListener)
    }

    return httpServer.ListenAndServe()
}

/* refuseSessionStorageTimeout judges the registered manager's bound once at boot, so a replaced manager declaring one the rule refuses fails the boot rather than every saving request. */
func refuseSessionStorageTimeout(bootRuntime containercontract.Runtime) error {
    if false == bootRuntime.Scope().Has(session.ServiceSessionManager) {
        return nil
    }

    sessionManager, sessionManagerErr := runtime.FromRuntime[sessioncontract.Manager](bootRuntime, session.ServiceSessionManager)
    if nil != sessionManagerErr {
        return sessionManagerErr
    }

    if _, timeoutErr := session.ResolveStorageTimeout(sessionManager); nil != timeoutErr {
        return exception.NewError(
            "the session manager declares a storage timeout the rule refuses",
            exceptioncontract.Context{
                "service": session.ServiceSessionManager,
                "field":   session.StorageTimeoutRule().Name,
            },
            timeoutErr,
        )
    }

    return nil
}

/* awaitHttpServerEnd waits for the cancelled context or the server's own failure, whichever ends the serving first. The serve error is read on the shutdown branch too, since a listen failing in the same instant may lose the select. A shutdown that outlives its budget surfaces the deadline error and exits non-zero. Shutdown does not account for hijacked connections, so the shutdown hooks and then the open request scopes are joined under the same budget. */
func awaitHttpServerEnd(
    ctx context.Context,
    httpServer *nethttp.Server,
    errorChannel chan error,
    logger loggingcontract.Logger,
    shutdownTimeout time.Duration,
    openRequestScopes openRequestScopeReporter,
    shutdownHooks *httpShutdownHookSet,
) error {
    select {
    case <-ctx.Done():
        shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
        defer cancel()

        /* stored before Shutdown, which starts the hooks */
        shutdownHooks.shutdownContext.Store(shutdownContext)

        shutdownErr := httpServer.Shutdown(shutdownContext)

        /* Shutdown never waits for the hooks it starts and reports quiescent at once when only hijacked connections remain, so the hooks are joined within the same budget */
        waitForHttpShutdownHooks(&shutdownHooks.done, shutdownContext)

        /* every phase of the wind-down runs and each failing one contributes its cause: the drain behind a failed Shutdown is the wait a hijacked connection needs */
        runErrs := make([]error, 0, 3)

        serveErr := <-errorChannel
        if nil != serveErr && false == errors.Is(serveErr, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(serveErr, exception.LogContextConfig{}),
            )

            runErrs = append(runErrs, serveErr)
        }

        if nil != shutdownErr {
            logger.Error(
                "http server shutdown error",
                exception.LogContext(shutdownErr, exception.LogContextConfig{}),
            )

            runErrs = append(runErrs, shutdownErr)
        }

        drainErr := awaitOpenRequestScopes(shutdownContext, openRequestScopes, logger)
        if nil != drainErr {
            runErrs = append(runErrs, drainErr)
        }

        return markHttpWindDownFailure(runErrs)

    case err := <-errorChannel:
        if nil != err && false == errors.Is(err, nethttp.ErrServerClosed) {
            logger.Error(
                "http server error",
                exception.LogContext(err, exception.LogContextConfig{}),
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

/* httpShutdownHookSet counts the shutdown hooks net/http starts and holds the shutdown's context they are handed, stored before Shutdown starts them. */
type httpShutdownHookSet struct {
    done            sync.WaitGroup
    shutdownContext atomic.Value
}

/* contextForHooks answers the shutdown's context, and a background context for a hook started by a Shutdown the application did not call */
func (instance *httpShutdownHookSet) contextForHooks() context.Context {
    if shutdownContext, isContext := instance.shutdownContext.Load().(context.Context); true == isContext {
        return shutdownContext
    }

    return context.Background()
}

/* wrapHttpShutdownHook adapts a shutdown hook for net/http's RegisterOnShutdown, which never joins the goroutine it starts: the wrapper counts the hook into the set, hands it the shutdown's context and recovers a panic through the framework logger. */
func wrapHttpShutdownHook(
    hook func(shutdownContext context.Context),
    hooks *httpShutdownHookSet,
    logger loggingcontract.Logger,
) func() {
    hooks.done.Add(1)

    return func() {
        defer hooks.done.Done()
        defer recoverHttpShutdownHook(logger)

        hook(hooks.contextForHooks())
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

/* openRequestScopeReporter is the door through which the shutdown asks a kernel how many requests are still inside it; a nil one is not asked. */
type openRequestScopeReporter interface {
    OpenRequestScopes() int64
}

/* openRequestScopeCount answers the number of request scopes still open, and -1 when there is no kernel to ask, which is not zero: zero means everything drained. */
func openRequestScopeCount(reporter openRequestScopeReporter) int64 {
    if true == internal.IsNilInterface(reporter) {
        return -1
    }

    return reporter.OpenRequestScopes()
}

/* awaitOpenRequestScopesInterval is how often the drain re-reads the counter: short enough to add no perceptible delay, long enough not to spin. */
const awaitOpenRequestScopesInterval = 20 * time.Millisecond

/* awaitOpenRequestScopes holds the exit until every request scope the kernel opened has closed or the shutdown budget runs out, since Shutdown returns nil for a hijacked connection still being served. An expiry is an error, so the process exits non-zero; no kernel to ask is not waited on. */
func awaitOpenRequestScopes(
    ctx context.Context,
    reporter openRequestScopeReporter,
    logger loggingcontract.Logger,
) error {
    if 0 > openRequestScopeCount(reporter) {
        return nil
    }

    ticker := time.NewTicker(awaitOpenRequestScopesInterval)
    defer ticker.Stop()

    for {
        openScopes := openRequestScopeCount(reporter)
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

            logger.Error("http server shutdown error", exception.LogContext(drainErr, exception.LogContextConfig{}))

            return drainErr

        case <-ticker.C:
        }
    }
}

/* melody mints a session for every cookie-less request that reads the session, so an unlimited ttl leaves only the count to bound the fallback storage */
func (instance *Application) checkDefaultSessionStorageBound() {
    if config.ModeHttp != instance.runtimeFlags.Mode() || false == instance.defaultInMemorySessionStorage {
        return
    }

    httpConfiguration := instance.configuration.Http()
    if false == httpConfiguration.SessionTtl().Unlimited {
        return
    }

    if true == httpConfiguration.SessionMaxSessions().Unlimited {
        exception.Panic(
            exception.NewError(
                "the default in-memory session storage is paired with an unbounded session ttl and an unbounded session count, so every request that arrives without a session cookie and reads the session can leave an entry nothing removes until this process exits; bound `"+config.HttpSessionTtlKey+"` or `"+config.HttpSessionMaxSessionsKey+"`, or register `"+session.ServiceSessionStorage+"` with a shared storage an operator can expire",
                exceptioncontract.Context{
                    "environmentKeys": []string{config.HttpSessionTtlKey, config.HttpSessionMaxSessionsKey},
                },
                nil,
            ),
        )
    }

    instance.bootWarnings = append(instance.bootWarnings, internal.BootWarning{
        Name:    internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage,
        Message: "the default in-memory session storage is paired with an unbounded session ttl, so every request that arrives without a session cookie and reads the session can leave an entry no expiry removes, and once the storage holds `" + config.HttpSessionMaxSessionsKey + "` entries a new session is refused; set `" + config.HttpSessionTtlKey + "` to the lifetime this deployment wants, or register `" + session.ServiceSessionStorage + "` with a shared storage an operator can expire",
        Context: loggingcontract.Context{
            "configurationKey": config.HttpSessionTtlKey,
            "maxSessions":      httpConfiguration.SessionMaxSessions().Value,
        },
    })
}
