package application

import (
    "bufio"
    "context"
    "errors"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "slices"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

func TestApplicationRegisterHttpRoute_AppendsRegistrarBeforeBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
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
        context.Background(),
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
        context.Background(),
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
        context.Background(),
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
        context.Background(),
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
        context.Background(),
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
        ctx:                  context.Background(),
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
        ctx:                  context.Background(),
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

        endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter()), &sync.WaitGroup{})
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
        ctx:                  context.Background(),
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
    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, http.NewKernel(http.NewRouter()), &sync.WaitGroup{})
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

    endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter()), &sync.WaitGroup{})
    if nil != endErr {
        t.Fatalf("expected a clean shutdown for the server's own closed signal, got: %v", endErr)
    }
}

/* boot-end registers the framework exception listener in every process shape; a console reads it through the introspection command, which has to report the set a serving process runs */
func TestRegisterKernelHttpListeners_RegistersTheExceptionListenerWithoutAnErrorHandler(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, logging.NewNopLogger())

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

/* what a serving process runs is exactly what a console process exposes to the introspection command: the exception listener is registered at boot-end in both, and runHttp adds none of its own. */
func TestRunHttp_ExposesTheSameListenerSetAConsoleProcessInspects(t *testing.T) {
    servingApplication := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())
    servingApplication.registerKernelHttpListeners()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := servingApplication.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    consoleApplication := newCacheWarningTestApplication(t, config.ModeCli, logging.NewNopLogger())
    consoleApplication.registerKernelHttpListeners()

    servingListeners := registeredListenerNames(t, servingApplication)
    consoleListeners := registeredListenerNames(t, consoleApplication)

    if 0 == len(consoleListeners) {
        t.Fatalf("expected the console dispatcher to expose the kernel listeners")
    }

    if false == slices.Equal(servingListeners, consoleListeners) {
        t.Fatalf("expected the serving set %v to equal the inspected set %v", servingListeners, consoleListeners)
    }
}

/* registeredListenerNames reads the dispatcher through the same door the introspection command uses, so the comparison is over what an operator would actually see. */
func registeredListenerNames(t *testing.T, applicationInstance *Application) []string {
    t.Helper()

    dispatcher, ok := applicationInstance.kernel.EventDispatcher().(*event.EventDispatcher)
    if false == ok {
        t.Fatalf("expected the framework dispatcher, got %T", applicationInstance.kernel.EventDispatcher())
    }

    names := make([]string, 0)
    for _, registeredEvent := range dispatcher.RegisteredEvents() {
        for _, listener := range registeredEvent.Listeners {
            names = append(names, registeredEvent.EventName+"/"+listener.ListenerName)
        }
    }

    slices.Sort(names)

    return names
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

/* countingHttpKernel is the errorHandlerless kernel plus the one door the shutdown drain reads, so a test can drive the drain against a count it controls rather than against a live server. */
type countingHttpKernel struct {
    errorHandlerlessKernel
    openScopes atomic.Int64
}

func (instance *countingHttpKernel) OpenRequestScopes() int64 {
    return instance.openScopes.Load()
}

var _ httpcontract.Kernel = (*countingHttpKernel)(nil)

/* Shutdown returns at once for a hijacked connection, so the drain is what holds the exit until the last request scope closes */
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
        ctx:                  context.Background(),
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

/* newServedExceptionTestApplication builds an http-mode application whose kernel serves a request end to end: the container carries the configuration, the session manager and the event dispatcher the request path resolves, and one route answers a 404 HttpException, the error that either the framework exception listener or an installed error handler has to render. */
func newServedExceptionTestApplication(t *testing.T) *Application {
    t.Helper()

    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    kernelInstance.httpRouter.Handle(
        nethttp.MethodGet,
        "/missing",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewHttpException(nethttp.StatusNotFound, "no such product")
        },
    )

    kernelInstance.serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return applicationInstance.configuration, nil
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

    return applicationInstance
}

/* serveMissingOverTheWire serves the kernel the way an application with its own net/http.Server does — Boot's kernel handed to a server, Run never called — and answers the status and body of the one 404 route. */
func serveMissingOverTheWire(t *testing.T, applicationInstance *Application) (int, string) {
    t.Helper()

    server := httptest.NewServer(applicationInstance.kernel.HttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
    defer server.Close()

    response, requestErr := nethttp.Get(server.URL + "/missing")
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    defer func() {
        _ = response.Body.Close()
    }()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    return response.StatusCode, string(body)
}

/* the shape the upgrade guide sanctions for different server limits: an application serves Boot's kernel with its own server and never calls Run. The framework exception listener is registered at boot-end in every process shape, so a handler's HttpException keeps its status there instead of falling to the kernel's 500. */
func TestServeHttp_AnApplicationServingBootsKernelItselfKeepsTheExceptionStatus(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    applicationInstance.registerKernelHttpListeners()

    status, body := serveMissingOverTheWire(t, applicationInstance)
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected the HttpException's 404 without Run, got %d: %s", status, body)
    }
}

/* SetErrorHandler stays open after Boot, and a handler installed there has to be the one consulted: the kernel marks the exception with the handler it has at the moment of the error and the framework listener stands aside, so no decision taken at boot-end or at Run can leave the handler unconsulted */
func TestServeHttp_AnErrorHandlerInstalledAfterBootIsConsulted(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    applicationInstance.registerKernelHttpListeners()

    applicationInstance.kernel.HttpKernel().SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return http.TextResponse(nethttp.StatusTeapot, "rendered by the application")
        },
    )

    status, body := serveMissingOverTheWire(t, applicationInstance)
    if nethttp.StatusTeapot != status || false == strings.Contains(body, "rendered by the application") {
        t.Fatalf("expected the handler installed after boot to render the error, got %d: %s", status, body)
    }
}

/* the sister of the test above for a handler installed before boot ended: the listener is registered all the same and stands aside for it */
func TestServeHttp_AnErrorHandlerInstalledBeforeBootIsConsulted(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    applicationInstance.kernel.HttpKernel().SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return http.TextResponse(nethttp.StatusTeapot, "rendered by the application")
        },
    )

    applicationInstance.registerKernelHttpListeners()

    status, body := serveMissingOverTheWire(t, applicationInstance)
    if nethttp.StatusTeapot != status || false == strings.Contains(body, "rendered by the application") {
        t.Fatalf("expected the handler installed before boot to render the error, got %d: %s", status, body)
    }
}

func TestServeHttp_AnErrorHandlerIsConsultedForAPanicAndAFailingNotFoundHandler(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    kernelInstance.httpRouter.Handle(
        nethttp.MethodGet,
        "/panics",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(exception.NewHttpException(nethttp.StatusNotFound, "the controller panicked"))
        },
    )

    applicationInstance.kernel.HttpKernel().SetNotFoundHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewHttpException(nethttp.StatusNotFound, "the not found handler failed")
        },
    )

    applicationInstance.registerKernelHttpListeners()

    applicationInstance.kernel.HttpKernel().SetErrorHandler(
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request, err error) httpcontract.Response {
            return http.TextResponse(nethttp.StatusTeapot, "rendered by the application")
        },
    )

    server := httptest.NewServer(applicationInstance.kernel.HttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
    defer server.Close()

    for _, path := range []string{"/panics", "/nowhere"} {
        response, requestErr := nethttp.Get(server.URL + path)
        if nil != requestErr {
            t.Fatalf("%s: unexpected request error: %v", path, requestErr)
        }

        body, readErr := io.ReadAll(response.Body)
        _ = response.Body.Close()
        if nil != readErr {
            t.Fatalf("%s: unexpected read error: %v", path, readErr)
        }

        if nethttp.StatusTeapot != response.StatusCode || false == strings.Contains(string(body), "rendered by the application") {
            t.Fatalf("%s: expected the installed handler to render the error, got %d: %s", path, response.StatusCode, body)
        }
    }
}

/* an http process that goes through Run serves with the framework exception listener registered at boot-end: with no handler installed, a kernel.exception dispatch is answered after runHttp as it is before it */
func TestRunHttp_RegistersTheExceptionListenerWhenNoHandlerWasInstalled(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    applicationInstance.registerKernelHttpListeners()

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
        exception.NewError("serving without a handler", nil, nil),
    )

    _, dispatchErr := applicationInstance.kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected the framework exception listener to answer after runHttp")
    }
}

/* the drain exists for the connection the server's own Shutdown does not drain — a hijacked one — and that connection is exactly what makes Shutdown report a budget overrun, so returning on the overrun would skip the wait in the situation it exists for and close the application container under a handler still on the wire. Both causes are independently actionable, so both have to survive.

   The error channel is filled by the Serve goroutine rather than ahead of the call: with a value already in it, both arms of the select are ready at once and the branch taken is a coin flip. */
func TestAwaitHttpServerEnd_DrainsOpenScopesEvenWhenTheShutdownOverran(t *testing.T) {
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

    httpKernel := &countingHttpKernel{}
    httpKernel.openScopes.Store(1)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, httpKernel, &sync.WaitGroup{})

    close(releaseHandler)

    if nil == endErr {
        t.Fatalf("expected the overrun shutdown to be reported as a failure")
    }

    if false == strings.Contains(endErr.Error(), "context deadline exceeded") {
        t.Fatalf("expected the budget overrun to survive, got %q", endErr.Error())
    }

    if false == strings.Contains(endErr.Error(), "http shutdown left request scopes open") {
        t.Fatalf("expected the drain to have run and reported the open scopes, got %q", endErr.Error())
    }
}

type shutdownHookRecordLogger struct {
    loggingcontract.Logger

    messages []string
}

func (instance *shutdownHookRecordLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.messages = append(instance.messages, message)
}

type shutdownHookMessagePanickingError struct{}

func (shutdownHookMessagePanickingError) Error() string {
    panic("Error() panics")
}

type shutdownHookUnwrapPanickingError struct{}

func (shutdownHookUnwrapPanickingError) Error() string {
    return "a hook failure whose Unwrap panics"
}

func (shutdownHookUnwrapPanickingError) Unwrap() error {
    panic("Unwrap() panics")
}

/* a shutdown hook runs on a goroutine net/http starts and never joins, so the recovery of a hook is the last boundary it has: an error the hook panicked with whose Error or Unwrap panicked raised a second panic there, while the server was still draining, and ended the process. The hook's panic is now filed under the message that could be rendered */
func TestWrapHttpShutdownHook_FilesAPanicWhoseErrorOrUnwrapPanics(t *testing.T) {
    for name, testCase := range map[string]struct {
        panicValue error
        message    string
    }{
        "error":  {panicValue: shutdownHookMessagePanickingError{}, message: "error message panicked: Error() panics"},
        "unwrap": {panicValue: shutdownHookUnwrapPanickingError{}, message: "a hook failure whose Unwrap panics"},
    } {
        logger := &shutdownHookRecordLogger{Logger: logging.NewNopLogger()}

        var hooksDone sync.WaitGroup
        wrapHttpShutdownHook(func() { panic(testCase.panicValue) }, &hooksDone, logger)()
        hooksDone.Wait()

        if 1 != len(logger.messages) || testCase.message != logger.messages[0] {
            t.Fatalf("%s: expected one record %q, got %v", name, testCase.message, logger.messages)
        }
    }
}

type decoratorContributingModule struct {
    fakeModule
    decorators []applicationcontract.HttpHandlerDecorator
}

func (instance decoratorContributingModule) RegisterHttpHandlerDecorators(kernelInstance kernelcontract.Kernel) []applicationcontract.HttpHandlerDecorator {
    return instance.decorators
}

func recordingHttpHandlerDecorator(name string, sequence *[]string) applicationcontract.HttpHandlerDecorator {
    return func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            *sequence = append(*sequence, name)
            next.ServeHTTP(writer, request)
        })
    }
}

/* the registered decorator comes first and the module's after it, since modules contribute theirs at boot; a nil the module hands back is skipped rather than called */
func TestApplicationDecorateHttpHandler_TheFirstRegisteredDecoratorIsOutermost(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    sequence := make([]string, 0, 4)

    applicationInstance.RegisterHttpHandlerDecorator(recordingHttpHandlerDecorator("registered", &sequence))
    applicationInstance.RegisterModule(decoratorContributingModule{
        fakeModule: fakeModule{name: "decorator.module"},
        decorators: []applicationcontract.HttpHandlerDecorator{
            nil,
            recordingHttpHandlerDecorator("module", &sequence),
        },
    })

    applicationInstance.Boot()

    handler := applicationInstance.decorateHttpHandler(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        sequence = append(sequence, "kernel")
    }))

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/", nil))

    if "registered,module,kernel" != strings.Join(sequence, ",") {
        t.Fatalf("expected the registered decorator outermost, then the module's, then the kernel's handler, got %v", sequence)
    }
}

func TestApplicationRegisterHttpHandlerDecorator_PanicsOnNil(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpHandlerDecorator(nil)
    }, "http handler decorator may not be nil")
}

func TestApplicationRegisterHttpHandlerDecorator_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            return next
        })
    }, "may not register http handler decorators after boot")
}

/* the seam is proved on its own above; this proves runHttp serves what the seam answers: a request over the wire must pass through the registered decorator, which marks the response before the kernel's handler answers it */
func TestRunHttp_ServesThroughTheRegisteredDecorators(t *testing.T) {
    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey: "127.0.0.1:34523",
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
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(config.ModeHttp),
        kernel:               newTestKernel(),
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

    applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            writer.Header().Set("X-Decorated", "yes")
            next.ServeHTTP(writer, request)
        })
    })

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    client := &nethttp.Client{
        Timeout:   2 * time.Second,
        Transport: &nethttp.Transport{DisableKeepAlives: true},
    }

    var response *nethttp.Response
    var requestErr error
    for attempt := 0; attempt < 200; attempt++ {
        response, requestErr = client.Get("http://127.0.0.1:34523/probe")
        if nil == requestErr {
            break
        }

        time.Sleep(10 * time.Millisecond)
    }
    if nil != requestErr {
        t.Fatalf("the server never answered: %v", requestErr)
    }
    _ = response.Body.Close()

    cancelRun()

    select {
    case <-runResult:

    case <-time.After(10 * time.Second):
        t.Fatalf("expected runHttp to return after the cancellation; it is still waiting")
    }

    if "yes" != response.Header.Get("X-Decorated") {
        t.Fatalf("expected the served response to pass through the registered decorator, got X-Decorated=%q (status %d)", response.Header.Get("X-Decorated"), response.StatusCode)
    }
}

func TestServeHttp_AnAsteriskFormTargetAnswersTheRootFirewallsRefusal(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    var handlerRan atomic.Bool
    markHandler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handlerRan.Store(true)

        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    kernelInstance.httpRouter.Handle(nethttp.MethodGet, "/:slug", markHandler)
    kernelInstance.httpKernel.SetNotFoundHandler(markHandler)

    firewall := security.NewCompiledFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        "matcher:main",
        []securitycontract.Rule{},
        security.NewResolverTokenSource(func(request httpcontract.Request) securitycontract.Token {
            return security.NewAnonymousToken()
        }),
        security.NewAccessControl(
            security.NewAccessControlRule("/", "ROLE_USER"),
        ),
        security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            security.NewRoleVoter(),
        ),
        security.NewRoleHierarchy(map[string][]string{}),
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        security.SourceFirewall,
        security.SourceFirewall,
        security.SourceFirewall,
        security.SourceNone,
        security.SourceNone,
    )

    registry := security.NewFirewallRegistry(
        security.NewCompiledConfiguration([]*security.CompiledFirewall{firewall}, nil),
    )

    security.RegisterKernelSecurityResolutionListener(applicationInstance.kernel, registry)
    security.RegisterKernelAccessControlListener(applicationInstance.kernel, registry)
    applicationInstance.registerKernelHttpListeners()

    server := httptest.NewServer(applicationInstance.kernel.HttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
    defer server.Close()

    for _, requestLine := range []string{"GET /x HTTP/1.1", "GET * HTTP/1.1"} {
        connection, dialErr := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
        if nil != dialErr {
            t.Fatalf("unexpected dial error: %v", dialErr)
        }

        _, writeErr := connection.Write([]byte(requestLine + "\r\nHost: localhost\r\nConnection: close\r\n\r\n"))
        if nil != writeErr {
            t.Fatalf("unexpected write error: %v", writeErr)
        }

        response, readErr := nethttp.ReadResponse(bufio.NewReader(connection), nil)
        if nil != readErr {
            t.Fatalf("unexpected read error for %s: %v", requestLine, readErr)
        }

        _ = response.Body.Close()
        _ = connection.Close()

        if nethttp.StatusUnauthorized != response.StatusCode {
            t.Fatalf("expected %s to answer the root firewall's 401, got %d", requestLine, response.StatusCode)
        }
    }

    if true == handlerRan.Load() {
        t.Fatalf("expected no handler to run for an anonymous request under the root firewall")
    }
}

func TestServeHttp_ASlashLessPathThatFoldsIsRefusedBeforeTheFirewallAndTheRouterDisagree(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    var handlerRan atomic.Bool
    markHandler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handlerRan.Store(true)

        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    kernelInstance.httpRouter.Handle(nethttp.MethodGet, "/admin/*rest", markHandler)

    firewall := security.NewCompiledFirewall(
        "main",
        security.NewPathPrefixMatcher("/"),
        "matcher:main",
        []securitycontract.Rule{},
        security.NewResolverTokenSource(func(request httpcontract.Request) securitycontract.Token {
            return security.NewAnonymousToken()
        }),
        security.NewAccessControl(
            security.NewAccessControlRule("/admin", "ROLE_ADMIN"),
            security.NewAccessControlExactRule("/public", securitycontract.AttributePublicAccess),
        ),
        security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            security.NewRoleVoter(),
        ),
        security.NewRoleHierarchy(map[string][]string{}),
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        security.SourceFirewall,
        security.SourceFirewall,
        security.SourceFirewall,
        security.SourceNone,
        security.SourceNone,
    )

    registry := security.NewFirewallRegistry(
        security.NewCompiledConfiguration([]*security.CompiledFirewall{firewall}, nil),
    )

    security.RegisterKernelSecurityResolutionListener(applicationInstance.kernel, registry)
    security.RegisterKernelAccessControlListener(applicationInstance.kernel, registry)
    applicationInstance.registerKernelHttpListeners()

    /* a handler in front of the kernel that strips a prefix hands "/apiadmin/../public" on as "admin/../public": the router routes it to the catch-all of "/admin" while the access-control matcher folds it to the rule of "/public" */
    server := httptest.NewServer(nethttp.StripPrefix("/api", applicationInstance.kernel.HttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer())))
    defer server.Close()

    for _, testCase := range []struct {
        requestLine    string
        expectedStatus int
    }{
        {"GET /apiadmin/../public HTTP/1.1", nethttp.StatusBadRequest},
        {"GET /api/admin/x HTTP/1.1", nethttp.StatusUnauthorized},
    } {
        connection, dialErr := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
        if nil != dialErr {
            t.Fatalf("unexpected dial error: %v", dialErr)
        }

        _, writeErr := connection.Write([]byte(testCase.requestLine + "\r\nHost: localhost\r\nConnection: close\r\n\r\n"))
        if nil != writeErr {
            t.Fatalf("unexpected write error: %v", writeErr)
        }

        response, readErr := nethttp.ReadResponse(bufio.NewReader(connection), nil)
        if nil != readErr {
            t.Fatalf("unexpected read error for %s: %v", testCase.requestLine, readErr)
        }

        _ = response.Body.Close()
        _ = connection.Close()

        if testCase.expectedStatus != response.StatusCode {
            t.Fatalf("expected %s to answer %d, got %d", testCase.requestLine, testCase.expectedStatus, response.StatusCode)
        }
    }

    if true == handlerRan.Load() {
        t.Fatalf("expected the catch-all of /admin never to run for an anonymous request")
    }
}

/* the boot warnings the boot collected from the declarations it compiled are written once into the configured journal on the http path, and not at all by a command */
func TestRunHttp_WritesTheCollectedBootWarningsOnceAndRunCliDoesNot(t *testing.T) {
    bootWarning := internal.BootWarning{
        Name:    "security.firewallShadowed",
        Message: "the firewall is never selected",
        Context: loggingcontract.Context{"firewall": "apiAdmin"},
    }

    httpLogger := &warningRecordingLogger{}
    httpApplication := newCacheWarningTestApplication(t, config.ModeHttp, httpLogger)
    httpApplication.bootWarnings = []internal.BootWarning{bootWarning}

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := httpApplication.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    if warnings := httpLogger.warningsContaining(bootWarning.Message); 1 != len(warnings) {
        t.Fatalf("expected the boot warning written once on the http path, got %v", warnings)
    }

    cliLogger := &warningRecordingLogger{}
    cliApplication := newCacheWarningTestApplication(t, config.ModeCli, cliLogger)
    cliApplication.bootWarnings = []internal.BootWarning{bootWarning}

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    if runErr := cliApplication.runCli(); nil != runErr {
        t.Fatalf("unexpected run cli error: %v", runErr)
    }

    if warnings := cliLogger.warningsContaining(bootWarning.Message); 0 != len(warnings) {
        t.Fatalf("expected no boot warning on a cli run, got %v", warnings)
    }
}
