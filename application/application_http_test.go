package application

import (
    "context"
    "errors"
    "net"
    nethttp "net/http"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
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

        endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second)
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
    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond)
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

    endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second)
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

/* the gate is proven through runHttp itself: after the run the exception listener either answers a
kernel.exception dispatch or leaves it unanswered, which is the observable difference between the
listener registered and skipped. */
func TestRunHttp_SkipsTheExceptionListenerWhenAnErrorHandlerIsInstalled(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    applicationInstance.kernel.HttpKernel().SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return nil
        },
    )

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
        t.Fatalf("expected the framework exception listener to be skipped when a handler is installed")
    }
}

func TestRunHttp_RegistersTheExceptionListenerWithoutAnErrorHandler(t *testing.T) {
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

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected the framework exception listener to answer without a handler installed")
    }
}
