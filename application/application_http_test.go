package application

import (
    "context"
    "errors"
    "net"
    nethttp "net/http"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/config"
    configcontract "github.com/precision-soft/melody/config/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/event"
    eventcontract "github.com/precision-soft/melody/event/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    "github.com/precision-soft/melody/session"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func TestApplicationRegisterHttpRoute_AppendsRegistrarBeforeBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterHttpRoute(
        nethttp.MethodGet,
        "/test",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    if 1 != len(applicationInstance.httpRouteRegistrars) {
        t.Fatalf("expected 1 registrar, got %d", len(applicationInstance.httpRouteRegistrars))
    }
}

/* the queue this door feeds drains before the module phases run: a registrar queued from inside a module boot hook would never execute — a route silently absent — so the door refuses for the boot window and points at the hook made for module routes */
func TestApplicationRegisterHttpRoute_RefusesDuringTheBootWindow(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        (&Application{booting: true}).RegisterHttpRoute(
            nethttp.MethodGet,
            "/late",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
        )
    }, "may not register http routes from inside a module boot hook")
}

func TestApplicationRegisterHttpRoute_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpRoute(
            nethttp.MethodGet,
            "/test",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
        )
    }, "may not register http routes after boot")
}

func TestApplicationRegisterHttpMiddlewares_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpMiddlewares(func(next httpcontract.Handler) httpcontract.Handler {
            return next
        })
    }, "may not register http middlewares after boot")
}

func TestApplicationRegisterHttpMiddlewareFactories_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpMiddlewareFactories(
            func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }
            },
        )
    }, "may not register http middlewares after boot")
}

func TestBootHttp_HandsEveryDeclaredRouteToTheRouter(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterHttpRoute(
        nethttp.MethodGet,
        "/boot-http-probe",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    applicationInstance.bootHttp()

    found := false
    for _, route := range applicationInstance.kernel.HttpRouter().RouteDefinitions() {
        if "/boot-http-probe" == route.Pattern() {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the declared route to reach the router when the boot ran the registrars")
    }
}

func TestApplicationRegisterHttpMiddlewares_HandsThemToThePipeline(t *testing.T) {
    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    before := len(applicationInstance.httpMiddlewares.definitions)

    applicationInstance.RegisterHttpMiddlewares(func(next httpcontract.Handler) httpcontract.Handler {
        return next
    })

    applicationInstance.RegisterHttpMiddlewareFactories(
        func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware {
            return func(next httpcontract.Handler) httpcontract.Handler {
                return next
            }
        },
    )

    if before+2 != len(applicationInstance.httpMiddlewares.definitions) {
        t.Fatalf("expected both registrations to reach the pipeline, got %d definitions over %d", len(applicationInstance.httpMiddlewares.definitions), before)
    }
}

type warningRecordingLogger struct {
    mutex    sync.Mutex
    warnings []string
}

func (instance *warningRecordingLogger) record(level loggingcontract.Level, message string) {
    if loggingcontract.LevelWarning != level {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.warnings = append(instance.warnings, message)
}

func (instance *warningRecordingLogger) warningsContaining(fragment string) []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    matches := make([]string, 0, len(instance.warnings))

    for _, warning := range instance.warnings {
        if true == strings.Contains(warning, fragment) {
            matches = append(matches, warning)
        }
    }

    return matches
}

func (instance *warningRecordingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record(level, message)
}

func (instance *warningRecordingLogger) Debug(message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Info(message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Warning(message string, context loggingcontract.Context) {
    instance.record(loggingcontract.LevelWarning, message)
}

func (instance *warningRecordingLogger) Error(message string, context loggingcontract.Context) {
}

func (instance *warningRecordingLogger) Emergency(message string, context loggingcontract.Context) {
}

var _ loggingcontract.Logger = (*warningRecordingLogger)(nil)

/* the fragment is the part of the cache warning that identifies it whatever else the sentence grows */
const unboundedCacheWarningFragment = "default in-memory cache backend"

func newCacheWarningTestApplication(t *testing.T, mode string, logger loggingcontract.Logger) *Application {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey: "127.0.0.1:34517",
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(mode),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerOptions(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    applicationInstance.registerCache()

    return applicationInstance
}

func TestRunHttp_WarnsAboutTheUnboundedDefaultCacheBackend(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedCacheWarningFragment)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly one cache warning on the http path, got %d: %v", len(warnings), warnings)
    }
}

func TestRunHttp_StaysSilentWhenTheApplicationBoundedItsCacheBackend(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logger)
    applicationInstance.unboundedDefaultCacheBackend = false

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedCacheWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no cache warning, got %v", warnings)
    }
}

const unboundedSessionWarningFragment = "default in-memory session storage"

func newSessionWarningTestApplication(t *testing.T, sessionTtl string, logger loggingcontract.Logger) *Application {
    t.Helper()

    values := map[string]string{
        config.HttpAddressKey: "127.0.0.1:34519",
    }
    if "" != sessionTtl {
        values[config.HttpSessionTtlKey] = sessionTtl
    }

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(config.ModeHttp),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerOptions(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    applicationInstance.registerHttpSession()

    return applicationInstance
}

func TestRunHttp_WarnsAboutTheDefaultSessionStorageWithAnUnboundedTtl(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newSessionWarningTestApplication(t, "", logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedSessionWarningFragment)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly one session warning on the http path, got %d: %v", len(warnings), warnings)
    }
}

func TestRunHttp_StaysSilentWhenTheSessionTtlIsBounded(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newSessionWarningTestApplication(t, "30m", logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedSessionWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no session warning when the ttl is bounded, got %v", warnings)
    }
}

func TestRunHttp_StaysSilentWhenTheApplicationRegisteredItsSessionStorage(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newSessionWarningTestApplication(t, "", logger)
    applicationInstance.defaultInMemorySessionStorage = false

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedSessionWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no session warning when the application supplied the storage, got %v", warnings)
    }
}

func TestMarkHttpRunErrorLogged_WrapsAndMarks(t *testing.T) {
    original := errors.New("bind refused")

    marked := markHttpRunErrorLogged(original)

    exceptionErr, isExceptionErr := marked.(*exception.Error)
    if false == isExceptionErr {
        t.Fatalf("expected an exception error, got %T", marked)
    }

    if false == exceptionErr.AlreadyLogged() {
        t.Fatalf("expected the wrapped failure to be marked logged")
    }

    if false == errors.Is(marked, original) {
        t.Fatalf("expected the original failure to stay reachable in the chain")
    }
}

func TestAwaitHttpServerEnd_ReportsAServeErrorWhicheverBranchWins(t *testing.T) {
    serveErr := errors.New("listen tcp: bind refused by the probe")

    for iteration := 0; iteration < 20; iteration++ {
        errorChannel := make(chan error, 1)
        errorChannel <- serveErr

        cancelledContext, cancel := context.WithCancel(context.Background())
        cancel()

        endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter()))
        if nil == endErr {
            t.Fatalf("expected the serve error to be reported instead of a clean shutdown (iteration %d)", iteration)
        }

        if false == errors.Is(endErr, serveErr) {
            t.Fatalf("expected the serve error in the chain, got: %v", endErr)
        }
    }
}

/* the configured budget must travel from the environment key through the configuration into the shutdown, so the proof drives runHttp itself: a connection that sent half a request stays active through the whole shutdown, and only the 50ms the environment named — not the 5s default — explains a shutdown that gives up this fast. */
func TestRunHttp_CutsTheShutdownAtTheBudgetTheEnvironmentConfigured(t *testing.T) {
    logger := &warningRecordingLogger{}

    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey:         "127.0.0.1:34519",
                config.HttpShutdownTimeoutKey: "50ms",
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(config.ModeHttp),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerOptions(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    applicationInstance.registerCache()

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    var connection net.Conn
    var dialErr error
    for attempt := 0; attempt < 200; attempt++ {
        connection, dialErr = net.Dial("tcp", "127.0.0.1:34519")
        if nil == dialErr {
            break
        }

        time.Sleep(10 * time.Millisecond)
    }
    if nil != dialErr {
        t.Fatalf("the server never came up: %v", dialErr)
    }
    defer func() {
        _ = connection.Close()
    }()

    /* half a request keeps the connection active for the whole shutdown: the header never completes, so the server cannot idle it */
    _, writeErr := connection.Write([]byte("GET / HTTP/1.1\r\n"))
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    shutdownRequestedAt := time.Now()
    cancelRun()

    var runErr error
    select {
    case runErr = <-runResult:

    case <-time.After(4 * time.Second):
        t.Fatalf("expected runHttp to return within the configured budget; it is still waiting")
    }

    elapsed := time.Since(shutdownRequestedAt)

    if nil == runErr {
        t.Fatalf("expected the overrun shutdown budget to be reported as a failure")
    }

    if false == strings.Contains(runErr.Error(), "context deadline exceeded") {
        t.Fatalf("expected the budget overrun in the failure, got %q", runErr.Error())
    }

    if elapsed > 2*time.Second {
        t.Fatalf("expected the shutdown to be cut at the configured 50ms, took %v", elapsed)
    }
}

/* the budget is the parameter, not a constant: a request held open past the configured wait must be cut when the budget says so, and well before the default would. The handler is released only after the assertion, so the shutdown can never finish on its own first. */
func TestAwaitHttpServerEnd_CutsTheShutdownAtTheConfiguredBudget(t *testing.T) {
    handlerStarted := make(chan struct{})
    releaseHandler := make(chan struct{})

    serveMux := nethttp.NewServeMux()
    serveMux.HandleFunc("/", func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        close(handlerStarted)
        <-releaseHandler
    })

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("unexpected listen error: %v", listenErr)
    }

    httpServer := &nethttp.Server{Handler: serveMux}

    errorChannel := make(chan error, 1)
    go func() {
        errorChannel <- httpServer.Serve(listener)
    }()

    go func() {
        response, requestErr := nethttp.Get("http://" + listener.Addr().String() + "/")
        if nil == requestErr {
            _ = response.Body.Close()
        }
    }()

    <-handlerStarted

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    shutdownStartedAt := time.Now()
    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, http.NewKernel(http.NewRouter()))
    elapsed := time.Since(shutdownStartedAt)

    close(releaseHandler)

    if nil == endErr {
        t.Fatalf("expected the overrun shutdown budget to be reported as a failure")
    }

    if false == strings.Contains(endErr.Error(), "context deadline exceeded") {
        t.Fatalf("expected the budget overrun in the failure, got %q", endErr.Error())
    }

    if elapsed > 2*time.Second {
        t.Fatalf("expected the shutdown to be cut at the 50ms budget, took %v", elapsed)
    }
}

func TestAwaitHttpServerEnd_TreatsServerClosedAsACleanShutdown(t *testing.T) {
    errorChannel := make(chan error, 1)
    errorChannel <- nethttp.ErrServerClosed

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter()))
    if nil != endErr {
        t.Fatalf("expected a clean shutdown for the server's own closed signal, got: %v", endErr)
    }
}

type errorHandlerlessKernel struct{}

func (instance *errorHandlerlessKernel) Use(middlewares ...httpcontract.Middleware) {}

func (instance *errorHandlerlessKernel) SetNotFoundHandler(handler httpcontract.Handler) {}

func (instance *errorHandlerlessKernel) SetErrorHandler(handler httpcontract.ErrorHandler) {}

func (instance *errorHandlerlessKernel) SetForwardedHeadersPolicy(policy httpcontract.ForwardedHeadersPolicy) {
}

func (instance *errorHandlerlessKernel) SetSessionCookiePolicy(policy httpcontract.SessionCookiePolicy) {
}

func (instance *errorHandlerlessKernel) SetMethodPolicy(policy httpcontract.MethodPolicy) {
}

func (instance *errorHandlerlessKernel) ServeHttp(serviceContainer containercontract.Container) nethttp.Handler {
    return nil
}

var _ httpcontract.Kernel = (*errorHandlerlessKernel)(nil)

func TestKernelHasErrorHandler_ReadsTheHasDoor(t *testing.T) {
    bareKernel := http.NewKernel(http.NewRouter())

    if true == kernelHasErrorHandler(bareKernel) {
        t.Fatalf("expected no error handler on a fresh kernel")
    }

    bareKernel.SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return nil
        },
    )

    if false == kernelHasErrorHandler(bareKernel) {
        t.Fatalf("expected the installed error handler to be reported")
    }

    if true == kernelHasErrorHandler(&errorHandlerlessKernel{}) {
        t.Fatalf("expected a kernel without the door to be read as having no handler")
    }
}

/* the gate is proven through the boot-end registration itself: after it the exception listener
either answers a kernel.exception dispatch or leaves it unanswered, which is the observable
difference between the listener registered and skipped. The handler is installed BEFORE the
registration runs, because that is the contract — an error handler installed by boot takes the
listener's place. */
func TestRegisterKernelHttpListeners_SkipsTheExceptionListenerWhenAnErrorHandlerIsInstalled(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    applicationInstance.kernel.HttpKernel().SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return nil
        },
    )

    applicationInstance.registerKernelHttpListeners()

    runtimeInstance := runtime.New(
        context.Background(),
        applicationInstance.kernel.ServiceContainer().NewScope(),
        applicationInstance.kernel.ServiceContainer(),
    )

    exceptionEvent := http.NewKernelExceptionEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/fail"),
        exception.NewError("boot gate failure", nil, nil),
    )

    _, dispatchErr := applicationInstance.kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil != exceptionEvent.Response() {
        t.Fatalf("expected the framework exception listener to be skipped when a handler is installed")
    }
}

func TestRegisterKernelHttpListeners_RegistersTheExceptionListenerWithoutAnErrorHandler(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    applicationInstance.registerKernelHttpListeners()

    runtimeInstance := runtime.New(
        context.Background(),
        applicationInstance.kernel.ServiceContainer().NewScope(),
        applicationInstance.kernel.ServiceContainer(),
    )

    exceptionEvent := http.NewKernelExceptionEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/fail"),
        exception.NewError("boot gate failure", nil, nil),
    )

    _, dispatchErr := applicationInstance.kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected the framework exception listener to answer without a handler installed")
    }
}

/* the contrast half of the move: running the http server registers nothing — the listeners belong
to Boot, in every process shape, so what the server runs is exactly what the console inspects */
func TestRunHttp_RegistersNoKernelListeners(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := applicationInstance.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    runtimeInstance := runtime.New(
        context.Background(),
        applicationInstance.kernel.ServiceContainer().NewScope(),
        applicationInstance.kernel.ServiceContainer(),
    )

    exceptionEvent := http.NewKernelExceptionEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/fail"),
        exception.NewError("run http gate failure", nil, nil),
    )

    _, dispatchErr := applicationInstance.kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil != exceptionEvent.Response() {
        t.Fatalf("expected runHttp to register no listener; the boot-end registration owns them")
    }
}

/* countingHttpKernel is the errorHandlerless kernel plus the one door the shutdown drain reads, so a test can drive the drain against a count it controls rather than against a live server. */
type countingHttpKernel struct {
    errorHandlerlessKernel
    openScopes atomic.Int64
}

func (instance *countingHttpKernel) OpenRequestScopes() int64 {
    return instance.openScopes.Load()
}

var _ httpcontract.Kernel = (*countingHttpKernel)(nil)

/* the drain holds the exit until the last request scope closes. Shutdown answers only for the connections the server still owns, so a hijacked one — a websocket — returns it immediately and the process used to announce a clean stop with that handler still running under a container that was closing. */
func TestAwaitOpenRequestScopes_WaitsUntilTheLastScopeCloses(t *testing.T) {
    httpKernel := &countingHttpKernel{}
    httpKernel.openScopes.Store(2)

    go func() {
        time.Sleep(60 * time.Millisecond)
        httpKernel.openScopes.Store(1)
        time.Sleep(60 * time.Millisecond)
        httpKernel.openScopes.Store(0)
    }()

    startedAt := time.Now()

    drainErr := awaitOpenRequestScopes(context.Background(), httpKernel, &warningRecordingLogger{})
    if nil != drainErr {
        t.Fatalf("expected the drain to succeed once the scopes closed, got: %v", drainErr)
    }

    if elapsed := time.Since(startedAt); 100*time.Millisecond > elapsed {
        t.Fatalf("expected the drain to have waited for the scopes, returned after %v", elapsed)
    }
}

/* a drain that does not finish inside the budget is an error and not a warning, for the reason the shutdown overrun beside it is one: the process exits non-zero, which is how the operator is told that requests were still inside the server when it stopped. The count travels in the context, because "how many" is the whole of what the operator can act on. */
func TestAwaitOpenRequestScopes_ReportsTheScopesStillOpenWhenTheBudgetRunsOut(t *testing.T) {
    httpKernel := &countingHttpKernel{}
    httpKernel.openScopes.Store(3)

    budgetContext, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
    defer cancel()

    logger := &warningRecordingLogger{}

    drainErr := awaitOpenRequestScopes(budgetContext, httpKernel, logger)
    if nil == drainErr {
        t.Fatalf("expected the undrained scopes to be reported as a failure")
    }

    if "http shutdown left request scopes open" != drainErr.Error() {
        t.Fatalf("unexpected message: %q", drainErr.Error())
    }

    var exceptionErr *exception.Error
    if false == errors.As(drainErr, &exceptionErr) {
        t.Fatalf("expected an exception error, got %T", drainErr)
    }

    if int64(3) != exceptionErr.Context()["openRequestScopes"] {
        t.Fatalf("expected the count in the context, got %#v", exceptionErr.Context()["openRequestScopes"])
    }

    if false == errors.Is(drainErr, context.DeadlineExceeded) {
        t.Fatalf("expected the expired budget to stay reachable in the chain, got: %v", drainErr)
    }
}

/* a kernel that cannot be asked is not waited on at all: the absent door means "no measurement", and waiting on a number nobody maintains would hang every shutdown of a replacement kernel for its whole budget and then report a failure that never happened. */
func TestAwaitOpenRequestScopes_DoesNotWaitOnAKernelThatCannotBeAsked(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    drainErr := awaitOpenRequestScopes(cancelledContext, &errorHandlerlessKernel{}, &warningRecordingLogger{})
    if nil != drainErr {
        t.Fatalf("expected no drain for a kernel with no counter, got: %v", drainErr)
    }
}

/* the whole chain, end to end and through runHttp itself: a handler hijacks its connection, the kernel's request scope stays open behind it, and the shutdown must refuse to report a stop it did not obtain. net/http's Shutdown returns immediately for a hijacked connection — it stopped being the server's the moment the handler took it — so before the scope was counted this exact shape answered nil in no time at all while the handler ran on and the container closed under it. */
func TestRunHttp_RefusesToReportACleanStopWhileAHijackedHandlerIsStillServed(t *testing.T) {
    handlerHijacked := make(chan struct{})
    releaseHandler := make(chan struct{})

    kernelInstance := newTestKernel()

    kernelInstance.httpRouter.Handle(
        nethttp.MethodGet,
        "/upgrade",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            hijacker, ok := writer.(nethttp.Hijacker)
            if false == ok {
                close(handlerHijacked)

                return http.TextResponse(nethttp.StatusOK, "no hijacker"), nil
            }

            connection, _, hijackErr := hijacker.Hijack()
            if nil != hijackErr {
                close(handlerHijacked)

                return http.TextResponse(nethttp.StatusOK, "hijack failed"), nil
            }

            close(handlerHijacked)
            <-releaseHandler

            _ = connection.Close()

            return http.TextResponse(nethttp.StatusOK, "served"), nil
        },
    )

    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey:         "127.0.0.1:34521",
                config.HttpShutdownTimeoutKey: "200ms",
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(config.ModeHttp),
        kernel:               kernelInstance,
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerOptions(testhelper.NewEmbeddedStaticFs(), configuration), configuration),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return &warningRecordingLogger{}, nil
        },
    )

    applicationInstance.registerCache()

    /* the request path resolves these on its way to the handler, and the bare test container carries none of them: without the configuration the kernel answers 500 before routing, and the hijack this test is about never happens */
    kernelInstance.serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
    )
    kernelInstance.serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(), 30*time.Minute), nil
        },
    )
    kernelInstance.serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return kernelInstance.eventDispatcher, nil
        },
    )

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    for attempt := 0; attempt < 200; attempt++ {
        probeConnection, dialErr := net.Dial("tcp", "127.0.0.1:34521")
        if nil == dialErr {
            _ = probeConnection.Close()

            break
        }

        time.Sleep(10 * time.Millisecond)
    }

    go func() {
        /* the request ends in EOF by construction: the handler takes the connection and closes it, so there is no response to read. What the test observes is the shutdown, not this. */
        response, requestErr := nethttp.Get("http://127.0.0.1:34521/upgrade")
        if nil == requestErr {
            _ = response.Body.Close()
        }
    }()

    select {
    case <-handlerHijacked:

    case <-time.After(4 * time.Second):
        t.Fatalf("the hijacking handler was never reached")
    }

    cancelRun()

    var runErr error
    select {
    case runErr = <-runResult:

    case <-time.After(4 * time.Second):
        close(releaseHandler)

        t.Fatalf("expected runHttp to return within the configured budget; it is still waiting")
    }

    close(releaseHandler)

    if nil == runErr {
        t.Fatalf("expected the undrained request scope to be reported instead of a clean stop")
    }

    if false == strings.Contains(runErr.Error(), "http shutdown left request scopes open") {
        t.Fatalf("expected the undrained scopes in the failure, got %q", runErr.Error())
    }
}
