package application

import (
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
    "bufio"
    "context"
    "errors"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "slices"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/debug"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/http/middleware"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func TestApplicationRegisterHttpRoute_AppendsRegistrarBeforeBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterHttpRoute(
        "/test",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    if 1 != len(applicationInstance.httpRouteRegistrars) {
        t.Fatalf("expected 1 registrar, got %d", len(applicationInstance.httpRouteRegistrars))
    }
}

func TestApplicationRegisterHttpRoute_RefusesDuringTheBootWindow(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        (&Application{booting: true}).RegisterHttpRoute(
            "/late",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
            httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )
    }, "may not register http routes from inside a module boot hook")
}

func TestApplicationRegisterHttpRoute_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpRoute(
            "/test",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            },
            httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )
    }, "may not register http routes after boot")
}

func TestApplication_RegisterHttpRoute_CarriesTheOptionsToTheRouter(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    options := httpcontract.RouteOptions{
        Name:         "probe.update",
        Methods:      []string{nethttp.MethodPut, nethttp.MethodPatch},
        Requirements: map[string]string{"id": "[0-9]+"},
    }

    applicationInstance.RegisterHttpRoute(
        "/probe/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        options,
    )

    options.Methods[0] = nethttp.MethodDelete
    options.Requirements["id"] = "[a-z]+"

    applicationInstance.bootHttp()

    for _, route := range applicationInstance.kernel.HttpRouter().RouteDefinitions() {
        if "probe.update" != route.Name() {
            continue
        }

        if 2 != len(route.Methods()) || nethttp.MethodPut != route.Methods()[0] || nethttp.MethodPatch != route.Methods()[1] {
            t.Fatalf("expected the route under both declared methods, got %v", route.Methods())
        }

        if "[0-9]+" != route.Requirements()["id"] {
            t.Fatalf("expected the requirement declared at the call, got %v", route.Requirements())
        }

        return
    }

    t.Fatal("expected the route found by its name")
}

func TestApplication_RegisterHttpRoute_ZeroOptionsAnswerEveryMethod(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterHttpRoute(
        "/every-method-probe",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        httpcontract.RouteOptions{},
    )

    applicationInstance.bootHttp()

    for _, method := range []string{nethttp.MethodGet, nethttp.MethodPost, nethttp.MethodDelete} {
        if _, matched := applicationInstance.kernel.HttpRouter().Match(method, "/every-method-probe", "", ""); false == matched {
            t.Fatalf("expected the zero options to answer %s", method)
        }
    }
}

func TestApplicationRegisterHttpMiddlewares_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpMiddlewares([]httpcontract.Middleware{func(next httpcontract.Handler) httpcontract.Handler {
            return next
        }})
    }, "may not register http middlewares after boot")
}

func TestApplicationRegisterHttpMiddlewareFactories_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpMiddlewareFactories([]middlewarepipeline.MiddlewareFactory{
            func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                return func(next httpcontract.Handler) httpcontract.Handler {
                    return next
                }, nil
            },
        })
    }, "may not register http middleware factories after boot")
}

func TestBootHttp_HandsEveryDeclaredRouteToTheRouter(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterHttpRoute(
        "/boot-http-probe",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        httpcontract.RouteOptions{Methods: []string{nethttp.MethodGet}},
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

func TestBootHttp_TheApplicationsRouteKeepsTheMethodItDeclared(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.RegisterHttpRoute(
        "/boot-http-post-probe",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        httpcontract.RouteOptions{Methods: []string{nethttp.MethodPost}},
    )

    applicationInstance.bootHttp()

    if _, matched := applicationInstance.kernel.HttpRouter().Match(nethttp.MethodGet, "/boot-http-post-probe", "", ""); true == matched {
        t.Fatalf("expected a route declared for POST to stay unmatched by GET")
    }

    if _, matched := applicationInstance.kernel.HttpRouter().Match(nethttp.MethodPost, "/boot-http-post-probe", "", ""); false == matched {
        t.Fatalf("expected the route declared for POST to match POST")
    }
}

func TestApplicationRegisterHttpMiddlewares_HandsThemToThePipeline(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    before := len(applicationInstance.httpMiddlewares.definitions)

    applicationInstance.RegisterHttpMiddlewares([]httpcontract.Middleware{func(next httpcontract.Handler) httpcontract.Handler {
        return next
    }})

    applicationInstance.RegisterHttpMiddlewareFactories([]middlewarepipeline.MiddlewareFactory{
        func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
            return func(next httpcontract.Handler) httpcontract.Handler {
                return next
            }, nil
        },
    })

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

func (instance *warningRecordingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *warningRecordingLogger) CauseChainDepth() int {
    return 8
}

var _ loggingcontract.Logger = (*warningRecordingLogger)(nil)

const cacheWarningFragment = "cache"

func registerTestEventDispatcher(applicationInstance *Application) {
    applicationInstance.RegisterService(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return applicationInstance.kernel.EventDispatcher(), nil
        },
        containercontract.RegisterOptions{},
    )
}

func newCacheWarningTestApplication(t *testing.T, mode string, logger loggingcontract.Logger) *Application {
    t.Helper()

    return newCacheWarningTestApplicationOver(t, mode, map[string]string{}, logger)
}

func newCacheWarningTestApplicationOver(t *testing.T, mode string, extraValues map[string]string, logger loggingcontract.Logger) *Application {
    t.Helper()

    values := map[string]string{
        config.HttpAddressKey: freeLoopbackAddress(t),
    }
    for key, value := range extraValues {
        values[key] = value
    }

    environment, environmentErr := config.NewEnvironment(context.Background(), 
        &mapEnvironmentSource{
            values: values,
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: mode}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        containercontract.RegisterOptions{},
    )

    registerTestEventDispatcher(applicationInstance)

    applicationInstance.registerCache()

    return applicationInstance
}

func TestRunHttp_WritesNoWarningAboutTheDefaultCacheBackend(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(cacheWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no cache warning, got %v", warnings)
    }
}

const unboundedSessionWarningFragment = "default in-memory session storage"

func newSessionWarningTestApplication(t *testing.T, sessionTtl string, logger loggingcontract.Logger) *Application {
    t.Helper()

    return newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionTtlKey: sessionTtl}, logger)
}

func newSessionBoundTestApplication(t *testing.T, mode string, sessionValues map[string]string, logger loggingcontract.Logger) *Application {
    t.Helper()

    values := map[string]string{
        config.HttpAddressKey: freeLoopbackAddress(t),
    }
    for key, value := range sessionValues {
        if "" != value {
            values[key] = value
        }
    }

    environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: mode}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        containercontract.RegisterOptions{},
    )

    registerSessionCollaborators(applicationInstance)
    applicationInstance.registerHttpSession()

    registerTestEventDispatcher(applicationInstance)

    return applicationInstance
}

func TestRunHttp_WarnsAboutTheDefaultSessionStorageWithAnUnboundedTtl(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newSessionWarningTestApplication(t, "unlimited", logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    applicationInstance.checkDefaultSessionStorageBound()

    runErr := applicationInstance.runHttp(cancelledContext)
    if nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedSessionWarningFragment)
    if 1 != len(warnings) {
        t.Fatalf("expected exactly one session warning on the http path, got %d: %v", len(warnings), warnings)
    }
}

func TestCheckDefaultSessionStorageBound_NamesTheCodeAndTheCountOfTheWarning(t *testing.T) {
    applicationInstance := newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionTtlKey: "unlimited", config.HttpSessionMaxSessionsKey: "7"}, &warningRecordingLogger{})

    applicationInstance.checkDefaultSessionStorageBound()

    if 1 != len(applicationInstance.bootWarnings) || internal.BootWarningNameSessionUnboundedTtlOnBoundedDefaultStorage != applicationInstance.bootWarnings[0].Name || 7 != applicationInstance.bootWarnings[0].Context["maxSessions"] {
        t.Fatalf("expected the coded session warning naming 7 sessions, got %v", applicationInstance.bootWarnings)
    }
}

func TestCheckDefaultSessionStorageBound_RefusesAnUnboundedTtlBesideAnUnboundedCount(t *testing.T) {
    applicationInstance := newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionTtlKey: "unlimited", config.HttpSessionMaxSessionsKey: "unlimited"}, &warningRecordingLogger{})

    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || false == strings.Contains(refusal.Message(), "an unbounded session count") {
            t.Fatalf("expected the unbounded pair refused, got %v", refusal)
        }
    }()

    applicationInstance.checkDefaultSessionStorageBound()
}

func TestCheckDefaultSessionStorageBound_AdmitsTheUnboundedPairWhereNoDefaultStorageServes(t *testing.T) {
    unbounded := map[string]string{config.HttpSessionTtlKey: "unlimited", config.HttpSessionMaxSessionsKey: "unlimited"}

    cliApplication := newSessionBoundTestApplication(t, config.ModeCli, unbounded, &warningRecordingLogger{})
    cliApplication.checkDefaultSessionStorageBound()

    ownStorageApplication := newSessionBoundTestApplication(t, config.ModeHttp, unbounded, &warningRecordingLogger{})
    ownStorageApplication.defaultInMemorySessionStorage = false
    ownStorageApplication.checkDefaultSessionStorageBound()

    boundedTtlApplication := newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionMaxSessionsKey: "unlimited"}, &warningRecordingLogger{})
    boundedTtlApplication.checkDefaultSessionStorageBound()

    for _, applicationInstance := range []*Application{cliApplication, ownStorageApplication, boundedTtlApplication} {
        if 0 != len(applicationInstance.bootWarnings) {
            t.Fatalf("expected no session warning, got %v", applicationInstance.bootWarnings)
        }
    }
}

func TestRegisterHttpSession_TheDefaultStorageHoldsTheConfiguredCount(t *testing.T) {
    applicationInstance := newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionMaxSessionsKey: "1"}, &warningRecordingLogger{})

    storage := container.MustFromResolver[sessioncontract.Storage](context.Background(), applicationInstance.kernel.ServiceContainer(), session.ServiceSessionStorage)

    if saveErr := storage.Save(sessionRuntime(), "first", map[string]any{"a": 1}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected first save error: %v", saveErr)
    }

    if saveErr := storage.Save(sessionRuntime(), "second", map[string]any{"a": 1}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the second session refused at a ceiling of one")
    }
}

func TestRunHttp_StaysSilentWhenTheSessionTtlIsBounded(t *testing.T) {
    for _, sessionTtl := range []string{"30m", ""} {
        logger := &warningRecordingLogger{}

        applicationInstance := newSessionWarningTestApplication(t, sessionTtl, logger)

        cancelledContext, cancel := context.WithCancel(context.Background())
        cancel()

        applicationInstance.checkDefaultSessionStorageBound()

        runErr := applicationInstance.runHttp(cancelledContext)
        if nil != runErr {
            t.Fatalf("unexpected run http error: %v", runErr)
        }

        warnings := logger.warningsContaining(unboundedSessionWarningFragment)
        if 0 != len(warnings) {
            t.Fatalf("ttl %q: expected no session warning when the ttl is bounded, got %v", sessionTtl, warnings)
        }
    }
}

func TestRunHttp_StaysSilentWhenTheApplicationRegisteredItsSessionStorage(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newSessionWarningTestApplication(t, "", logger)
    applicationInstance.defaultInMemorySessionStorage = false

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    applicationInstance.checkDefaultSessionStorageBound()

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

        endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter(), http.KernelConfig{}), &httpShutdownHookSet{})
        if nil == endErr {
            t.Fatalf("expected the serve error to be reported instead of a clean shutdown (iteration %d)", iteration)
        }

        if false == errors.Is(endErr, serveErr) {
            t.Fatalf("expected the serve error in the chain, got: %v", endErr)
        }
    }
}

func TestRunHttp_CutsTheShutdownAtTheBudgetTheEnvironmentConfigured(t *testing.T) {
    assertRunHttpCutsTheShutdownAtTheBudget(
        t,
        map[string]string{config.HttpShutdownTimeoutKey: "50ms"},
        ApplicationConfig{},
    )
}

func TestRunHttp_CutsTheShutdownAtTheBudgetTheApplicationsFieldConfigured(t *testing.T) {
    assertRunHttpCutsTheShutdownAtTheBudget(
        t,
        map[string]string{},
        ApplicationConfig{HttpServer: HttpServerConfig{ShutdownTimeout: bound.Of(50 * time.Millisecond)}},
    )
}

func assertRunHttpCutsTheShutdownAtTheBudget(t *testing.T, values map[string]string, applicationConfig ApplicationConfig) {
    t.Helper()

    listener, address := listenLoopback(t)

    logger := &warningRecordingLogger{}

    values[config.HttpAddressKey] = address
    values[config.ScopeTeardownTimeoutKey] = "10ms"

    environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        applicationConfig:    applicationConfig,
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
        containercontract.RegisterOptions{},
    )

    registerTestEventDispatcher(applicationInstance)

    applicationInstance.registerCache()

    registerServingProbe(t, applicationInstance)
    applicationInstance.httpListener = listener

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    awaitServing(t, address)

    connection, dialErr := net.Dial("tcp", address)
    if nil != dialErr {
        t.Fatalf("unexpected dial error: %v", dialErr)
    }
    defer func() {
        _ = connection.Close()
    }()

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
    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, http.NewKernel(http.NewRouter(), http.KernelConfig{}), &httpShutdownHookSet{})
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

    endErr := awaitHttpServerEnd(cancelledContext, &nethttp.Server{}, errorChannel, &warningRecordingLogger{}, time.Second, http.NewKernel(http.NewRouter(), http.KernelConfig{}), &httpShutdownHookSet{})
    if nil != endErr {
        t.Fatalf("expected a clean shutdown for the server's own closed signal, got: %v", endErr)
    }
}

func TestRegisterKernelHttpListeners_LeavesTheBuiltInExceptionListenerToTheHttpKernel(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, logging.NewNopLogger())

    applicationInstance.registerKernelHttpListeners()

    for _, listenerName := range registeredListenerNames(t, applicationInstance) {
        if true == strings.HasPrefix(listenerName, kernelcontract.EventKernelException+"/") {
            t.Fatalf("expected no kernel.exception listener on a console's dispatcher, got %s", listenerName)
        }
    }
}

func TestRunHttp_ServesTheListenerSetAConsoleInspectsPlusTheDeclaredBuiltIn(t *testing.T) {
    servingApplication := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())
    servingApplication.registerKernelHttpListeners()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := servingApplication.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    consoleApplication := newCacheWarningTestApplication(t, config.ModeCli, logging.NewNopLogger())
    consoleApplication.registerKernelHttpListeners()

    consoleListeners := registeredListenerNames(t, consoleApplication)

    if 0 == len(consoleListeners) {
        t.Fatalf("expected the console dispatcher to expose the kernel listeners")
    }

    builtIn := make([]string, 0, 1)
    servingWithoutBuiltIn := make([]string, 0)
    for _, listenerName := range registeredListenerNames(t, servingApplication) {
        if true == strings.HasPrefix(listenerName, kernelcontract.EventKernelException+"/") {
            builtIn = append(builtIn, listenerName)

            continue
        }

        servingWithoutBuiltIn = append(servingWithoutBuiltIn, listenerName)
    }

    if false == slices.Equal(servingWithoutBuiltIn, consoleListeners) {
        t.Fatalf("expected the serving set %v to equal the inspected set %v", servingWithoutBuiltIn, consoleListeners)
    }

    if 1 != len(builtIn) || false == strings.HasSuffix(builtIn[0], "builtInExceptionListener") {
        t.Fatalf("expected the built-in exception listener to be the one the kernel adds, got %v", builtIn)
    }

    declared := make([]debug.DeferredListener, 0, 1)
    for _, deferredListener := range consoleApplication.servingProcessListeners() {
        if kernelcontract.EventKernelException == deferredListener.EventName {
            declared = append(declared, deferredListener)
        }
    }

    if 1 != len(declared) || http.BuiltInExceptionListenerPriority != declared[0].Priority {
        t.Fatalf("expected the console to declare the built-in at its priority, got %+v", declared)
    }
}

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

type countingHttpKernel struct {
    openScopes atomic.Int64
}

func (instance *countingHttpKernel) OpenRequestScopes() int64 {
    return instance.openScopes.Load()
}

var _ openRequestScopeReporter = (*countingHttpKernel)(nil)

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

func TestAwaitOpenRequestScopes_DoesNotWaitWithoutAKernelToAsk(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    var typedNilKernel *http.Kernel

    drainErr := awaitOpenRequestScopes(cancelledContext, typedNilKernel, &warningRecordingLogger{})
    if nil != drainErr {
        t.Fatalf("expected no drain without a kernel to ask, got: %v", drainErr)
    }
}

func TestRunHttp_RefusesToReportACleanStopWhileAHijackedHandlerIsStillServed(t *testing.T) {
    listener, address := listenLoopback(t)

    handlerHijacked := make(chan struct{})
    releaseHandler := make(chan struct{})

    kernelInstance := newTestKernel()

    kernelInstance.httpRouter.Handle(
        "/upgrade",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
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
        http.RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    environment, environmentErr := config.NewEnvironment(context.Background(), 
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey:         address,
                config.HttpShutdownTimeoutKey: "200ms",
                config.ScopeTeardownTimeoutKey: "10ms",
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               kernelInstance,
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return &warningRecordingLogger{}, nil
        },
        containercontract.RegisterOptions{},
    )

    registerTestEventDispatcher(applicationInstance)

    applicationInstance.registerCache()

    kernelInstance.serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
        containercontract.RegisterOptions{},
    )
    kernelInstance.serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}), nil
        },
        containercontract.RegisterOptions{},
    )

    registerServingProbe(t, applicationInstance)
    applicationInstance.httpListener = listener

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    awaitServing(t, address)

    go func() {
        response, requestErr := nethttp.Get("http://"+address+"/upgrade")
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

func newServedExceptionTestApplication(t *testing.T) *Application {
    t.Helper()

    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    kernelInstance.httpRouter.Handle(
        "/missing",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewHttpException(nethttp.StatusNotFound, "no such product")
        },
        http.RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    kernelInstance.serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return applicationInstance.configuration, nil
        },
        containercontract.RegisterOptions{},
    )
    kernelInstance.serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30*time.Minute)}), nil
        },
        containercontract.RegisterOptions{},
    )

    return applicationInstance
}

func serveMissingOverTheWire(t *testing.T, applicationInstance *Application) (int, string) {
    t.Helper()

    server := httptest.NewServer(applicationInstance.servedHttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
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

func TestServeHttp_AnApplicationServingBootsKernelItselfKeepsTheExceptionStatus(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    applicationInstance.registerKernelHttpListeners()

    status, body := serveMissingOverTheWire(t, applicationInstance)
    if nethttp.StatusNotFound != status {
        t.Fatalf("expected the HttpException's 404 without Run, got %d: %s", status, body)
    }
}

func addTeapotExceptionListener(applicationInstance *Application) {
    applicationInstance.kernel.EventDispatcher().AddListener(
        kernelcontract.EventKernelException,
        func(runtimeInstance containercontract.Runtime, eventValue eventcontract.Event) error {
            eventValue.Payload().(*http.KernelExceptionEvent).SetResponse(http.TextResponse(nethttp.StatusTeapot, "rendered by the application"))

            return nil
        },
        eventcontract.ListenerConfig{},
    )
}

func TestServeHttp_AnExceptionListenerAddedAfterTheHandlerWasBuiltAnswers(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    applicationInstance.registerKernelHttpListeners()

    handler := applicationInstance.servedHttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer())

    addTeapotExceptionListener(applicationInstance)

    server := httptest.NewServer(handler)
    defer server.Close()

    response, requestErr := nethttp.Get(server.URL + "/missing")
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    defer func() { _ = response.Body.Close() }()

    body, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        t.Fatalf("unexpected read error: %v", readErr)
    }

    if nethttp.StatusTeapot != response.StatusCode || false == strings.Contains(string(body), "rendered by the application") {
        t.Fatalf("expected the listener added after the build to answer, got %d: %s", response.StatusCode, body)
    }
}

func TestServeHttp_AnExceptionListenerAddedBeforeBootEndedAnswers(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    addTeapotExceptionListener(applicationInstance)

    applicationInstance.registerKernelHttpListeners()

    status, body := serveMissingOverTheWire(t, applicationInstance)
    if nethttp.StatusTeapot != status || false == strings.Contains(body, "rendered by the application") {
        t.Fatalf("expected the listener added before boot ended to answer, got %d: %s", status, body)
    }
}

func TestServeHttp_AnExceptionListenerAnswersAPanicAndAFailingNotFoundHandler(t *testing.T) {
    applicationInstance := newServedExceptionTestApplication(t)

    kernelInstance, ok := applicationInstance.kernel.(*testKernel)
    if false == ok {
        t.Fatalf("expected the test kernel, got %T", applicationInstance.kernel)
    }

    kernelInstance.httpRouter.Handle(
        "/panics",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            panic(exception.NewHttpException(nethttp.StatusNotFound, "the controller panicked"))
        },
        http.RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    applicationInstance.httpKernel = http.NewKernel(kernelInstance.httpRouter, http.KernelConfig{
        NotFoundHandler: func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, exception.NewHttpException(nethttp.StatusNotFound, "the not found handler failed")
        },
    })

    applicationInstance.registerKernelHttpListeners()

    addTeapotExceptionListener(applicationInstance)

    server := httptest.NewServer(applicationInstance.servedHttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
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
            t.Fatalf("%s: expected the listener to answer the error, got %d: %s", path, response.StatusCode, body)
        }
    }
}

func TestRunHttp_ServesWithTheBuiltInExceptionListener(t *testing.T) {
    applicationInstance := newCacheWarningTestApplication(t, config.ModeHttp, logging.NewNopLogger())

    applicationInstance.registerKernelHttpListeners()

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := applicationInstance.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    runtimeInstance := runtime.NewRequest(
        context.Background(),
        applicationInstance.kernel.ServiceContainer().NewScope(),
)

    exceptionEvent := http.NewKernelExceptionEvent(
        runtimeInstance,
        testhelper.NewHttpTestRequest(nethttp.MethodGet, "http://example.com/fail"),
        exception.NewError("serving failure", nil, nil),
        http.KernelExceptionEventConfig{},
    )

    _, dispatchErr := applicationInstance.kernel.EventDispatcher().DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
    if nil != dispatchErr {
        t.Fatalf("unexpected dispatch error: %v", dispatchErr)
    }

    if nil == exceptionEvent.Response() {
        t.Fatalf("expected the framework exception listener to answer after runHttp")
    }
}

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

    endErr := awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, httpKernel, &httpShutdownHookSet{})

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

func TestWrapHttpShutdownHook_FilesAPanicWhoseErrorOrUnwrapPanics(t *testing.T) {
    for name, testCase := range map[string]struct {
        panicValue error
        message    string
    }{
        "error":  {panicValue: shutdownHookMessagePanickingError{}, message: "error message panicked: Error() panics"},
        "unwrap": {panicValue: shutdownHookUnwrapPanickingError{}, message: "a hook failure whose Unwrap panics"},
    } {
        logger := &shutdownHookRecordLogger{Logger: logging.NewNopLogger()}

        hooks := &httpShutdownHookSet{}
        wrapHttpShutdownHook(func(shutdownContext context.Context) { panic(testCase.panicValue) }, hooks, logger)()
        hooks.done.Wait()

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

func TestApplicationDecorateHttpHandler_TheFirstRegisteredDecoratorIsOutermost(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    sequence := make([]string, 0, 4)

    applicationInstance.RegisterHttpHandlerDecorator(recordingHttpHandlerDecorator("registered", &sequence))
    applicationInstance.RegisterModule(decoratorContributingModule{
        fakeModule: fakeModule{name: "decorator.module"},
        decorators: []applicationcontract.HttpHandlerDecorator{
            recordingHttpHandlerDecorator("module", &sequence),
        },
    })

    applicationInstance.Boot()

    handler, decorateErr := applicationInstance.decorateHttpHandler(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        sequence = append(sequence, "kernel")
    }))
    if nil != decorateErr {
        t.Fatalf("unexpected decorate error: %v", decorateErr)
    }

    handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/", nil))

    if "registered,module,kernel" != strings.Join(sequence, ",") {
        t.Fatalf("expected the registered decorator outermost, then the module's, then the kernel's handler, got %v", sequence)
    }
}

func TestApplicationRegisterHttpHandlerDecorator_PanicsOnNil(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpHandlerDecorator(nil)
    }, "http handler decorator may not be nil")
}

func TestApplicationRegisterHttpHandlerDecorator_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs()})

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            return next
        })
    }, "may not register http handler decorators after boot")
}

func TestRunHttp_ServesThroughTheRegisteredDecorators(t *testing.T) {
    listener, address := listenLoopback(t)

    environment, environmentErr := config.NewEnvironment(context.Background(), 
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey: address,
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return &warningRecordingLogger{}, nil
        },
        containercontract.RegisterOptions{},
    )

    registerTestEventDispatcher(applicationInstance)

    applicationInstance.registerCache()

    applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
        return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
            writer.Header().Set("X-Decorated", "yes")
            next.ServeHTTP(writer, request)
        })
    })

    registerServingProbe(t, applicationInstance)
    applicationInstance.httpListener = listener

    runContext, cancelRun := context.WithCancel(context.Background())
    defer cancelRun()

    runResult := make(chan error, 1)
    go func() {
        runResult <- applicationInstance.runHttp(runContext)
    }()

    awaitServing(t, address)

    client := &nethttp.Client{
        Timeout:   2 * time.Second,
        Transport: &nethttp.Transport{DisableKeepAlives: true},
    }

    var response *nethttp.Response
    var requestErr error
    for attempt := 0; attempt < 200; attempt++ {
        response, requestErr = client.Get("http://"+address+"/probe")
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
    markHandler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handlerRan.Store(true)

        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    kernelInstance.httpRouter.Handle("/:slug", markHandler, http.RouteOptions{Methods: []string{nethttp.MethodGet}})
    applicationInstance.httpKernel = http.NewKernel(kernelInstance.httpRouter, http.KernelConfig{NotFoundHandler: markHandler})

    firewall := security.NewCompiledFirewall(security.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            security.NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher:main",
        TokenSource: security.NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return security.NewAnonymousToken(), nil
        }, security.ResolverTokenSourceConfig{}),
        AccessControl: security.NewAccessControl([]security.AccessControlRule{
            accesscontrol.NewRawPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{"ROLE_USER"}}),
        }),
        AccessDecisionManager: security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{security.NewRoleVoter()}, security.AccessDecisionManagerConfig{}),
        RoleHierarchy:               security.NewRoleHierarchy(map[string][]string{}),
        RoleHierarchySource:         security.SourceFirewall,
        AccessDecisionManagerSource: security.SourceFirewall,
        AccessControlSource:         security.SourceFirewall,
    })

    registry := security.NewFirewallRegistry(
        security.NewCompiledConfiguration([]*security.CompiledFirewall{firewall}, nil),
    )

    security.RegisterKernelSecurityResolutionListener(applicationInstance.kernel, registry)
    security.RegisterKernelAccessControlListener(applicationInstance.kernel, registry)
    applicationInstance.registerKernelHttpListeners()

    server := httptest.NewServer(applicationInstance.servedHttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer()))
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
    markHandler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        handlerRan.Store(true)

        return http.EmptyResponse(nethttp.StatusOK), nil
    }

    kernelInstance.httpRouter.Handle("/admin/*rest", markHandler, http.RouteOptions{Methods: []string{nethttp.MethodGet}})

    firewall := security.NewCompiledFirewall(security.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            security.NewPathPrefixMatcher("/"),
        MatcherDescription: "matcher:main",
        TokenSource: security.NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
            return security.NewAnonymousToken(), nil
        }, security.ResolverTokenSourceConfig{}),
        AccessControl: security.NewAccessControl([]security.AccessControlRule{
            accesscontrol.NewRawPrefixRule("/admin", accesscontrol.RuleConfig{Attributes: []string{"ROLE_ADMIN"}}),
            accesscontrol.NewExactRule("/public", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}}),
        }),
        AccessDecisionManager: security.NewAccessDecisionManager(
            securitycontract.DecisionStrategyAffirmative,
            []securitycontract.Voter{security.NewRoleVoter()}, security.AccessDecisionManagerConfig{}),
        RoleHierarchy:               security.NewRoleHierarchy(map[string][]string{}),
        RoleHierarchySource:         security.SourceFirewall,
        AccessDecisionManagerSource: security.SourceFirewall,
        AccessControlSource:         security.SourceFirewall,
    })

    registry := security.NewFirewallRegistry(
        security.NewCompiledConfiguration([]*security.CompiledFirewall{firewall}, nil),
    )

    security.RegisterKernelSecurityResolutionListener(applicationInstance.kernel, registry)
    security.RegisterKernelAccessControlListener(applicationInstance.kernel, registry)
    applicationInstance.registerKernelHttpListeners()

    server := httptest.NewServer(nethttp.StripPrefix("/api", applicationInstance.servedHttpKernel().ServeHttp(applicationInstance.kernel.ServiceContainer())))
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

func TestRunHttpAndRunCli_WriteTheCollectedBootWarningsOnce(t *testing.T) {
    bootWarning := internal.BootWarning{
        Name:    internal.BootWarningNameFirewallDoorReachUnknown,
        Message: "a firewall door's reach is not known at boot",
        Context: loggingcontract.Context{"firewall": "main"},
    }
    otherWarning := internal.BootWarning{
        Name:    "application.other",
        Message: "another hazard",
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

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    for _, testCase := range []struct {
        name          string
        values        map[string]string
        expectedFirst int
        expectedOther int
    }{
        {"every code", map[string]string{}, 1, 1},
        {"one code ignored", map[string]string{config.CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown}, 0, 1},
        {"the journal disabled", map[string]string{config.CliBootWarningsDisabledKey: "true"}, 0, 0},
    } {
        cliLogger := &warningRecordingLogger{}
        cliApplication := newCacheWarningTestApplicationOver(t, config.ModeCli, testCase.values, cliLogger)
        cliApplication.bootWarnings = []internal.BootWarning{bootWarning, otherWarning}

        if runErr := cliApplication.runCli(); nil != runErr {
            t.Fatalf("%s: unexpected run cli error: %v", testCase.name, runErr)
        }

        if warnings := cliLogger.warningsContaining(bootWarning.Message); testCase.expectedFirst != len(warnings) {
            t.Fatalf("%s: expected the door warning written %d times on a cli run, got %v", testCase.name, testCase.expectedFirst, warnings)
        }

        if warnings := cliLogger.warningsContaining(otherWarning.Message); testCase.expectedOther != len(warnings) {
            t.Fatalf("%s: expected the other warning written %d times on a cli run, got %v", testCase.name, testCase.expectedOther, warnings)
        }
    }
}

func TestRefuseCliBootWarningKeysInHttp_RefusesEitherKeySetInAnHttpProcess(t *testing.T) {
    for _, values := range []map[string]string{
        {config.CliBootWarningsIgnoredKey: internal.BootWarningNameFirewallDoorReachUnknown},
        {config.CliBootWarningsDisabledKey: "false"},
    } {
        applicationInstance := newCacheWarningTestApplicationOver(t, config.ModeHttp, values, &warningRecordingLogger{})

        func() {
            defer func() {
                refusal, isRefusal := recover().(*exception.Error)
                if false == isRefusal || false == strings.HasPrefix(refusal.Message(), "a cli boot warning key is set in an http process") {
                    t.Fatalf("%v: expected the key refused in http, got %v", values, refusal)
                }

                for key := range values {
                    if key != refusal.Context()["environmentKey"] {
                        t.Fatalf("expected %s named, got %v", key, refusal.Context())
                    }
                }
            }()

            applicationInstance.refuseCliBootWarningKeysInHttp()
        }()
    }

    newCacheWarningTestApplication(t, config.ModeHttp, &warningRecordingLogger{}).refuseCliBootWarningKeysInHttp()
    newCacheWarningTestApplicationOver(t, config.ModeHttp, map[string]string{config.CliBootWarningsIgnoredKey: "  ", config.CliBootWarningsDisabledKey: ""}, &warningRecordingLogger{}).refuseCliBootWarningKeysInHttp()
    newCacheWarningTestApplicationOver(t, config.ModeCli, map[string]string{config.CliBootWarningsDisabledKey: "true"}, &warningRecordingLogger{}).refuseCliBootWarningKeysInHttp()
}

func TestRunHttp_RefusesAServerLimitTheApplicationConfigSetsBelowItsMinimum(t *testing.T) {
    applicationInstance := newSessionWarningTestApplication(t, "", &warningRecordingLogger{})
    applicationInstance.applicationConfig = ApplicationConfig{HttpServer: HttpServerConfig{ReadTimeout: bound.Of(time.Duration(0))}}

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := applicationInstance.runHttp(cancelledContext); nil == runErr {
        t.Fatalf("expected a zero read timeout refused")
    }
}

func TestRunHttp_RefusesAScopeTeardownTimeoutOutlastingTheShutdownBeforeServing(t *testing.T) {
    environment, environmentErr := config.NewEnvironment(context.Background(), 
        &mapEnvironmentSource{
            values: map[string]string{
                config.HttpAddressKey:         freeLoopbackAddress(t),
                config.HttpShutdownTimeoutKey: "2s",
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    runErr := applicationInstance.runHttp(context.Background())
    if nil == runErr || "the scope teardown timeout must stay under the http shutdown timeout" != runErr.Error() {
        t.Fatalf("expected the boot refused by name, got %v", runErr)
    }
}

type refusedBoundSessionManager struct {
    *session.Manager
}

func (instance *refusedBoundSessionManager) StorageTimeout() bound.Duration {
    return bound.Of(time.Duration(0))
}

func TestBootHttpPipeline_RefusesAManagerDeclaringAStorageTimeoutTheRuleRefusesInsideBoot(t *testing.T) {
    probeKind := containercontract.KindUnspecified

    environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: map[string]string{config.HttpAddressKey: freeLoopbackAddress(t)}})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir(), config.ConfigurationConfig{})
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    applicationInstance := &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
        kernel:               newTestKernel(),
        httpMiddlewares:      NewHttpMiddleware(newStaticFileServerConfig(testhelper.NewEmbeddedStaticFs(), configuration)),
        moduleConfigurations: make(map[string]any),
    }

    applicationInstance.RegisterService(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            probeKind = resolver.Runtime().Kind()

            return &refusedBoundSessionManager{Manager: session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{})}, nil
        },
        containercontract.RegisterOptions{},
    )

    openBootRuntime(t, applicationInstance)

    var recovered any
    func() {
        defer func() {
            recovered = recover()
        }()

        applicationInstance.bootHttpPipeline(applicationInstance.kernel.BootRuntime())
    }()

    exceptionErr, isExceptionErr := recovered.(*exception.Error)
    if false == isExceptionErr || "the session manager declares a storage timeout the rule refuses" != exceptionErr.Error() || session.StorageTimeoutRule().Name != exceptionErr.Context()["field"] {
        t.Fatalf("expected the boot refused naming %s, got %v", session.StorageTimeoutRule().Name, recovered)
    }

    if containercontract.KindBoot != probeKind {
        t.Fatalf("expected the probe to build the session manager under the boot runtime, got %s", probeKind)
    }
}

type loadFailingApplicationSessionStorage struct{}

func (instance *loadFailingApplicationSessionStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, errors.New("session store is down")
}

func (instance *loadFailingApplicationSessionStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return nil
}

func (instance *loadFailingApplicationSessionStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *loadFailingApplicationSessionStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func prepareSessionReaderOverAnOutage(serviceContainer containercontract.Container, httpRouter httpcontract.Router) {

    serviceContainer.MustOverrideProtectedInstance(
        session.ServiceSessionManager,
        session.NewManager(&loadFailingApplicationSessionStorage{}, session.ManagerConfig{Ttl: bound.Of(time.Hour)}),
        container.OverrideOptions{},
    )

    httpRouter.Handle(
        "/reads-the-session",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            if _, sessionErr := http.RequestSession(request); nil != sessionErr {
                return nil, sessionErr
            }

            return http.TextResponse(nethttp.StatusOK, "ok"), nil
        },
        http.RouteOptions{Methods: []string{nethttp.MethodGet}},
    )
}

func serveSessionReader(t *testing.T, applicationInstance *Application) int {
    t.Helper()

    request := httptest.NewRequest(nethttp.MethodGet, "/reads-the-session", nil)
    request.AddCookie(&nethttp.Cookie{Name: session.SessionCookieName, Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})

    return serveTestRequest(t, applicationInstance, request).Code
}

func TestApplication_InstallsTheSessionPolicyOnTheHttpKernel(t *testing.T) {
    for field, expected := range map[bool]int{true: nethttp.StatusOK, false: nethttp.StatusServiceUnavailable} {
        applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), HttpKernel: http.KernelConfig{SessionPolicy: httpcontract.SessionPolicy{EmptyOnOutage: field}}})
        applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp})
        serviceContainer := applicationInstance.kernel.ServiceContainer()

        applicationInstance.Boot()

        prepareSessionReaderOverAnOutage(serviceContainer, applicationInstance.kernel.HttpRouter())

        if status := serveSessionReader(t, applicationInstance); expected != status {
            t.Fatalf("field %v: expected %d at construction, got %d", field, expected, status)
        }

        _ = serviceContainer.CloseWithContext(context.Background())
    }
}

type infoRecordingLogger struct {
    warningRecordingLogger
    infos []string
}

func (instance *infoRecordingLogger) Info(message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.infos = append(instance.infos, message)
}

func TestApplication_TheStartUpRecordSpellsTheEnvironment(t *testing.T) {
    logger := &infoRecordingLogger{}

    applicationInstance := newSessionBoundTestApplication(t, config.ModeHttp, map[string]string{config.HttpSessionTtlKey: "1h"}, logger)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if runErr := applicationInstance.runHttp(cancelledContext); nil != runErr {
        t.Fatalf("unexpected run http error: %v", runErr)
    }

    expected := "with environment `" + applicationInstance.configuration.Kernel().Environment() + "`"

    logger.mutex.Lock()
    defer logger.mutex.Unlock()

    for _, message := range logger.infos {
        if true == strings.HasPrefix(message, "starting http server on ") {
            if false == strings.HasSuffix(message, expected) {
                t.Fatalf("expected the start-up record to end with %q, got %q", expected, message)
            }

            return
        }
    }

    t.Fatalf("expected the start-up record, got %v", logger.infos)
}

func TestAwaitHttpServerEnd_HandsAShutdownHookTheShutdownsContext(t *testing.T) {
    errorChannel := make(chan error, 1)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    hooks := &httpShutdownHookSet{}
    handed := make(chan context.Context, 1)
    httpServer := &nethttp.Server{}
    httpServer.RegisterOnShutdown(wrapHttpShutdownHook(func(shutdownContext context.Context) {
        handed <- shutdownContext
        errorChannel <- nethttp.ErrServerClosed
    }, hooks, &warningRecordingLogger{}))

    before := time.Now()
    _ = awaitHttpServerEnd(cancelledContext, httpServer, errorChannel, &warningRecordingLogger{}, 50*time.Millisecond, http.NewKernel(http.NewRouter(), http.KernelConfig{}), hooks)

    shutdownContext := <-handed

    deadline, hasDeadline := shutdownContext.Deadline()
    if false == hasDeadline || deadline.Before(before) || deadline.After(before.Add(time.Second)) {
        t.Fatalf("expected the shutdown's deadline on the hook's context, got %v %v", deadline, hasDeadline)
    }

    select {
    case <-shutdownContext.Done():
    case <-time.After(2 * time.Second):
        t.Fatalf("expected the hook's context done once the budget ends")
    }
}

/* headersTestModule serves one session firewall over / with a json route, a failing route, a stream, a route that turns X-Frame-Options off, a route that sets its own cache and frame headers, and a write the csrf check refuses */
type headersTestModule struct {
    csrfApplicationTestModule
}

func (instance *headersTestModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    instance.csrfApplicationTestModule.RegisterHttpRoutes(kernelInstance)

    router := kernelInstance.HttpRouter()
    router.Handle("/json", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.JsonResponse(nethttp.StatusOK, map[string]string{"ok": "yes"})
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/fail", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, exception.NewError("boom", nil, nil)
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/stream", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        streamWriter, streamErr := http.NewServerSentEventWriter(writer, http.ServerSentEventWriterConfig{})
        if nil != streamErr {
            return nil, streamErr
        }

        return nil, streamWriter.Send(http.ServerSentEvent{Data: "one"})
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/framed", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return http.TextResponse(nethttp.StatusOK, "framed"), nil
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}, Protection: http.RouteProtectionConfig{SecurityHeadersDisabled: []string{http.HeaderFrameOptions}}})
    router.Handle("/own", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        response := http.TextResponse(nethttp.StatusOK, "own")
        response.Headers().Set("Cache-Control", "public, max-age=60")
        response.Headers().Set(http.HeaderFrameOptions, "SAMEORIGIN")

        return response, nil
    }, http.RouteOptions{Methods: []string{nethttp.MethodGet}})
}

func bootHeadersTestApplication(t *testing.T, environmentName string, applicationConfig ApplicationConfig) *Application {
    t.Helper()

    return bootServingTestApplication(t, environmentName, applicationConfig, func(applicationInstance *Application) {
        applicationInstance.RegisterModule(&headersTestModule{csrfApplicationTestModule: csrfApplicationTestModule{fakeModule: fakeModule{name: "headers"}}})
    })
}

func assertDefaultSecurityHeaders(t *testing.T, what string, header nethttp.Header) {
    t.Helper()

    for name, value := range map[string]string{
        http.HeaderContentTypeOptions: "nosniff",
        http.HeaderFrameOptions:       "DENY",
        http.HeaderReferrerPolicy:     "strict-origin-when-cross-origin",
    } {
        if values := header.Values(name); 1 != len(values) || value != values[0] {
            t.Fatalf("%s: expected one %s: %s, got %v", what, name, value, header)
        }
    }

    if _, present := header[http.HeaderStrictTransportSecurity]; true == present {
        t.Fatalf("%s: expected no HSTS over http, got %v", what, header)
    }
}

func TestApplication_DefaultHeaders_OnAStaticFile(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/"+servingTestStaticFile, nil))
    if nethttp.StatusOK != recorder.Code || "body{}" != recorder.Body.String() {
        t.Fatalf("expected the static file served, got %d %q", recorder.Code, recorder.Body.String())
    }

    assertDefaultSecurityHeaders(t, "static file", recorder.Result().Header)
}

func TestApplication_DefaultHeaders_OnAJsonRoute(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    assertDefaultSecurityHeaders(t, "json route", serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/json", nil)).Result().Header)
}

func TestApplication_DefaultHeaders_OnAFirewallRefusal(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    request := httptest.NewRequest(nethttp.MethodPost, "http://app.example.com/account", nil)
    request.AddCookie(sessionCookieOfAnIdentity(t, applicationInstance.kernel))
    request.Header.Set("Sec-Fetch-Site", "cross-site")

    recorder := serveTestRequest(t, applicationInstance, request)
    if nethttp.StatusForbidden != recorder.Code {
        t.Fatalf("expected the forged write refused 403, got %d", recorder.Code)
    }

    assertDefaultSecurityHeaders(t, "firewall refusal", recorder.Result().Header)
}

func TestApplication_DefaultHeaders_OnTheErrorPage(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/fail", nil))
    if nethttp.StatusInternalServerError != recorder.Code {
        t.Fatalf("expected the error page, got %d", recorder.Code)
    }

    assertDefaultSecurityHeaders(t, "error page", recorder.Result().Header)
}

func TestApplication_DefaultHeaders_OnANotFound(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/missing", nil))
    if nethttp.StatusNotFound != recorder.Code {
        t.Fatalf("expected a not found, got %d", recorder.Code)
    }

    assertDefaultSecurityHeaders(t, "not found", recorder.Result().Header)
}

func TestApplication_DefaultHeaders_OnAServerSentEventStream(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/stream", nil))
    if "text/event-stream" != recorder.Result().Header.Get("Content-Type") || false == strings.Contains(recorder.Body.String(), "data: one") {
        t.Fatalf("expected the stream served, got %d %v %q", recorder.Code, recorder.Result().Header, recorder.Body.String())
    }

    assertDefaultSecurityHeaders(t, "stream", recorder.Result().Header)
}

func TestApplication_SecurityHeadersDisabledByField_NoneSentAndNoListener(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{SecurityHeaders: middleware.SecurityHeadersConfig{Disabled: true}})

    for _, path := range []string{"/json", "/missing", "/" + servingTestStaticFile} {
        header := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, path, nil)).Result().Header
        for _, name := range http.SecurityHeaderNames() {
            if _, present := header[name]; true == present {
                t.Fatalf("%s: expected no security header, got %v", path, header)
            }
        }
    }

    for _, registeredEvent := range applicationInstance.kernel.EventDispatcher().RegisteredEvents() {
        for _, listener := range registeredEvent.Listeners {
            if slices.Contains(listener.Config.Tags, kernelcontract.ListenerTagKernelSecurityHeaders) {
                t.Fatalf("expected no security headers listener, got %+v on %s", listener, registeredEvent.EventName)
            }
        }
    }
}

func TestApplication_StrictTransportSecurity_BehindATrustedProxy_Sent(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{HttpKernel: http.KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{TrustForwardedHeaders: true, TrustedProxyList: []string{"192.0.2.0/24"}}}})

    request := httptest.NewRequest(nethttp.MethodGet, "/json", nil)
    request.Header.Set("X-Forwarded-Proto", "https")

    header := serveTestRequest(t, applicationInstance, request).Result().Header
    if "max-age=31536000" != header.Get(http.HeaderStrictTransportSecurity) {
        t.Fatalf("expected HSTS on a request a trusted proxy states https, got %v", header)
    }
}

func TestApplication_StrictTransportSecurity_BehindAnUntrustedPeer_NotSent(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{HttpKernel: http.KernelConfig{ForwardedHeadersPolicy: httpcontract.ForwardedHeadersPolicy{TrustForwardedHeaders: true, TrustedProxyList: []string{"10.0.0.0/8"}}}})

    request := httptest.NewRequest(nethttp.MethodGet, "/json", nil)
    request.Header.Set("X-Forwarded-Proto", "https")

    header := serveTestRequest(t, applicationInstance, request).Result().Header
    if _, present := header[http.HeaderStrictTransportSecurity]; true == present {
        t.Fatalf("expected no HSTS on a scheme an untrusted peer states, got %v", header)
    }
}

func TestApplication_SecurityHeaders_LeaveCacheControlAndCorsAlone(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    header := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/own", nil)).Result().Header
    if "public, max-age=60" != header.Get("Cache-Control") || "SAMEORIGIN" != strings.Join(header.Values(http.HeaderFrameOptions), ",") || "nosniff" != header.Get(http.HeaderContentTypeOptions) {
        t.Fatalf("expected the route's own cache and frame headers kept and the others added, got %v", header)
    }

    for name := range header {
        if true == strings.HasPrefix(name, "Access-Control-") {
            t.Fatalf("expected no cors header added, got %v", header)
        }
    }
}

func TestApplication_SecurityHeaders_SameInDevAndProd(t *testing.T) {
    headersOf := func(environmentName string) map[string][]string {
        applicationInstance := bootHeadersTestApplication(t, environmentName, ApplicationConfig{})

        answered := map[string][]string{}
        for _, path := range []string{"/json", "/fail", "/missing"} {
            header := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, path, nil)).Result().Header
            for _, name := range http.SecurityHeaderNames() {
                answered[path+" "+name] = header.Values(name)
            }
        }

        return answered
    }

    development := headersOf(config.EnvironmentDevelopment)
    production := headersOf(config.EnvironmentProduction)

    for key, values := range development {
        if strings.Join(values, ",") != strings.Join(production[key], ",") {
            t.Fatalf("%s: expected the same header in dev and prod, got %v and %v", key, values, production[key])
        }
    }
}

func TestApplication_RouteProtection_DropsTheHeaderOnThatRouteOnly(t *testing.T) {
    applicationInstance := bootHeadersTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{})

    framed := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/framed", nil)).Result().Header
    if _, present := framed[http.HeaderFrameOptions]; true == present || "nosniff" != framed.Get(http.HeaderContentTypeOptions) {
        t.Fatalf("expected X-Frame-Options dropped on the route alone, got %v", framed)
    }

    assertDefaultSecurityHeaders(t, "another route", serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/json", nil)).Result().Header)
}

const malformedSecurityHeadersProbeMarker = "MELODY_TEST_MALFORMED_SECURITY_HEADERS_PROBE"

func TestApplication_SecurityHeaders_AMalformedValueRefusesTheApplicationWhenDisabled(t *testing.T) {
    if "" != os.Getenv(malformedSecurityHeadersProbeMarker) {
        NewApplication(context.Background(), ApplicationConfig{EnvironmentFiles: testhelper.NewEmbeddedEnvironmentFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), SecurityHeaders: middleware.SecurityHeadersConfig{Disabled: true, FrameOptions: "ALLOW-FROM https://a.example"}})

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestApplication_SecurityHeaders_AMalformedValueRefusesTheApplicationWhenDisabled$")
    child.Env = append(os.Environ(), malformedSecurityHeadersProbeMarker+"=1")

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() || false == strings.Contains(string(output), "X-Frame-Options admits DENY or SAMEORIGIN") {
        t.Fatalf("expected the application refused naming the frame options, got err %v with output: %s", runErr, string(output))
    }
}

func TestApplicationRegisterHttpRoute_CopiesTheSecurityHeadersDisabled(t *testing.T) {
    names := []string{http.HeaderFrameOptions}

    applicationInstance := bootServingTestApplication(t, config.EnvironmentDevelopment, ApplicationConfig{}, func(applicationInstance *Application) {
        applicationInstance.RegisterHttpRoute("/copied", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return http.TextResponse(nethttp.StatusOK, "copied"), nil
        }, http.RouteOptions{Methods: []string{nethttp.MethodGet}, Protection: http.RouteProtectionConfig{SecurityHeadersDisabled: names}})

        names[0] = http.HeaderReferrerPolicy
    })

    header := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/copied", nil)).Result().Header
    if _, present := header[http.HeaderFrameOptions]; true == present || "strict-origin-when-cross-origin" != header.Get(http.HeaderReferrerPolicy) {
        t.Fatalf("expected the names as registered, got %v", header)
    }
}

type bodyCapTestModule struct {
    csrfApplicationTestModule
}

func (instance *bodyCapTestModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    formLength := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        postBag, formErr := request.Post()
        if nil != formErr {
            return nil, formErr
        }

        return http.TextResponse(nethttp.StatusOK, strconv.Itoa(len(postBag.All()))), nil
    }

    kernelInstance.HttpRouter().Handle("/upload", formLength, http.RouteOptions{Methods: []string{nethttp.MethodPost}, Protection: http.RouteProtectionConfig{MaxBodyBytes: bound.Of(1024)}})
    kernelInstance.HttpRouter().Handle("/account", formLength, http.RouteOptions{Methods: []string{nethttp.MethodPost}})
}

func TestApplication_UploadRouteCap_LeavesTheOtherRoutesAtTheApplicationCap(t *testing.T) {
    applicationInstance := bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, config.HttpMaxRequestBodyBytesKey+"=64\n", ApplicationConfig{}, func(applicationInstance *Application) {
        applicationInstance.RegisterModule(&bodyCapTestModule{csrfApplicationTestModule: csrfApplicationTestModule{fakeModule: fakeModule{name: "cap"}}})
    })

    post := func(path string) int {
        request := httptest.NewRequest(nethttp.MethodPost, path, strings.NewReader("value="+strings.Repeat("a", 512)))
        request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

        return serveTestRequest(t, applicationInstance, request).Code
    }

    if status := post("/upload"); nethttp.StatusOK != status {
        t.Fatalf("expected the upload route's own cap to admit the body, got %d", status)
    }

    if status := post("/account"); nethttp.StatusRequestEntityTooLarge != status {
        t.Fatalf("expected the application's cap on another route, got %d", status)
    }
}

type handlerTestContextKey struct{}

/* bootHandlerTestApplication boots a production http application holding one plain route and one route that holds its request until release is closed */
func bootHandlerTestApplication(t *testing.T, environmentLines string, prepare func(applicationInstance *Application)) (*Application, chan struct{}, chan struct{}) {
    t.Helper()

    entered := make(chan struct{}, 1)
    release := make(chan struct{})

    applicationInstance := bootServingTestApplicationWithEnvironment(t, config.EnvironmentProduction, environmentLines, ApplicationConfig{}, func(applicationInstance *Application) {
        applicationInstance.RegisterHttpRoute(
            "/plain",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return http.TextResponse(nethttp.StatusOK, "plain"), nil
            },
            http.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        applicationInstance.RegisterHttpRoute(
            "/held",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                entered <- struct{}{}
                <-release

                return http.TextResponse(nethttp.StatusOK, "held"), nil
            },
            http.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        if nil != prepare {
            prepare(applicationInstance)
        }
    })

    return applicationInstance, entered, release
}

func mustHttpHandler(t *testing.T, applicationInstance *Application) nethttp.Handler {
    t.Helper()

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil != handlerErr {
        t.Fatalf("unexpected http handler error: %v", handlerErr)
    }

    return handler
}

/* holdOpenRequestScope serves the held route on its own goroutine and returns once the request is inside the handler */
func holdOpenRequestScope(t *testing.T, handler nethttp.Handler, entered chan struct{}) *sync.WaitGroup {
    t.Helper()

    served := &sync.WaitGroup{}
    served.Add(1)

    go func() {
        defer served.Done()

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(nethttp.MethodGet, "/held", nil))
    }()

    <-entered

    return served
}

type shutdownHookRecorder struct {
    calls    atomic.Int64
    contexts sync.Map
}

func (instance *shutdownHookRecorder) hook(name string) func(shutdownContext context.Context) {
    return func(shutdownContext context.Context) {
        instance.calls.Add(1)
        instance.contexts.Store(name, shutdownContext.Value(handlerTestContextKey{}))
    }
}

func (instance *shutdownHookRecorder) contextOf(name string) any {
    value, _ := instance.contexts.Load(name)

    return value
}

func TestApplicationHttpHandler_ServesARouteThroughThePipelineBuiltAtBoot(t *testing.T) {
    var middlewareRan atomic.Bool

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpMiddlewares([]httpcontract.Middleware{
            func(next httpcontract.Handler) httpcontract.Handler {
                return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    middlewareRan.Store(true)

                    return next(runtimeInstance, writer, request)
                }
            },
        })
    })

    server := httptest.NewServer(mustHttpHandler(t, applicationInstance))
    defer server.Close()

    response, requestErr := nethttp.Get(server.URL + "/plain")
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    defer func() { _ = response.Body.Close() }()

    body, _ := io.ReadAll(response.Body)
    if nethttp.StatusOK != response.StatusCode || "plain" != string(body) {
        t.Fatalf("expected the route served, got %d %q", response.StatusCode, body)
    }

    if false == middlewareRan.Load() {
        t.Fatalf("expected the application's middleware to run in the handed-out handler")
    }
}

func TestApplicationHttpHandler_WrapsTheHandlerInTheRegisteredDecorators(t *testing.T) {
    order := make([]string, 0, 2)
    orderMutex := sync.Mutex{}

    decorator := func(name string) applicationcontract.HttpHandlerDecorator {
        return func(next nethttp.Handler) nethttp.Handler {
            return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
                orderMutex.Lock()
                order = append(order, name)
                orderMutex.Unlock()

                next.ServeHTTP(writer, request)
            })
        }
    }

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpHandlerDecorator(decorator("first"))
        applicationInstance.RegisterHttpHandlerDecorator(decorator("second"))
    })

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/plain", nil))
    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the route served, got %d", recorder.Code)
    }

    if false == slices.Equal([]string{"first", "second"}, order) {
        t.Fatalf("expected the first registered decorator outermost, got %v", order)
    }
}

func TestApplicationHttpHandler_AnswersTheSameHandlerOnEveryCall(t *testing.T) {
    var decorations atomic.Int64

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            decorations.Add(1)

            return next
        })
    })

    mustHttpHandler(t, applicationInstance)
    mustHttpHandler(t, applicationInstance)

    if 1 != decorations.Load() {
        t.Fatalf("expected the handler built once, decorated %d times", decorations.Load())
    }
}

func TestApplicationHttpHandler_ReturnsThePreServeRefusal_AndMemoizesIt(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, config.ScopeTeardownTimeoutKey+"=10s\n"+config.HttpShutdownTimeoutKey+"=5s\n", nil)

    handler, firstErr := applicationInstance.HttpHandler()
    if nil != handler || nil == firstErr {
        t.Fatalf("expected the scope teardown outlasting the shutdown refused, got %v %v", handler, firstErr)
    }

    if "the scope teardown timeout must stay under the http shutdown timeout" != firstErr.Error() {
        t.Fatalf("expected the refusal of the scope teardown, got %v", firstErr)
    }

    _, secondErr := applicationInstance.HttpHandler()
    if firstErr != secondErr {
        t.Fatalf("expected the refusal memoized, got %v then %v", firstErr, secondErr)
    }
}

func TestApplicationHttpHandler_AppliesTheHttpKernelConfig(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpUnavailableRetryAfterKey+"=7s\n", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpRoute(
            "/unavailable",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, exception.MarkInfrastructureFailure(errors.New("cache is down"))
            },
            http.RouteOptions{Methods: []string{nethttp.MethodGet}},
        )
    })

    recorder := serveTestRequest(t, applicationInstance, httptest.NewRequest(nethttp.MethodGet, "/unavailable", nil))
    if nethttp.StatusServiceUnavailable != recorder.Code || "7" != recorder.Header().Get("Retry-After") {
        t.Fatalf("expected 503 with the key's Retry-After 7, got %d %q", recorder.Code, recorder.Header().Get("Retry-After"))
    }
}

func TestApplicationHttpHandler_MarksTheConfigurationServing(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)

    if resolveErr := applicationInstance.configuration.Resolve(); nil != resolveErr {
        t.Fatalf("expected a resolve admitted before the handler is handed out, got %v", resolveErr)
    }

    mustHttpHandler(t, applicationInstance)

    resolveErr := applicationInstance.configuration.Resolve()
    if nil == resolveErr || false == strings.Contains(resolveErr.Error(), "begun serving") {
        t.Fatalf("expected a resolve refused once the handler was handed out, got %v", resolveErr)
    }
}

func TestApplicationHttpHandler_WritesTheBootWarningsOnce(t *testing.T) {
    var logger *levelCapturingLogger

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        logger = registerLevelCapturingLogger(applicationInstance)
        applicationInstance.bootWarnings = append(applicationInstance.bootWarnings, internal.BootWarning{Name: "handler.test", Message: "the handler test's boot warning"})
    })

    from := logger.recorded()
    mustHttpHandler(t, applicationInstance)

    if 0 == logger.countSince(from, loggingcontract.LevelWarning) {
        t.Fatalf("expected the boot warnings written when the handler is handed out")
    }

    from = logger.recorded()
    mustHttpHandler(t, applicationInstance)

    if 0 != logger.countSince(from, loggingcontract.LevelWarning) {
        t.Fatalf("expected the boot warnings written once")
    }
}

func TestApplicationHttpHandler_CliMode_Refused(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)
    applicationInstance.runtimeFlags = NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeCli})

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil != handler || nil == handlerErr || "the http handler is asked of a process in cli mode" != handlerErr.Error() {
        t.Fatalf("expected the cli mode refused, got %v %v", handler, handlerErr)
    }

    if config.ModeCli != exception.LogContext(handlerErr, exception.LogContextConfig{})["mode"] {
        t.Fatalf("expected the refusal to name the mode, got %v", exception.LogContext(handlerErr, exception.LogContextConfig{}))
    }
}

func TestApplicationHttpHandler_AfterRun_Refused(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)
    applicationInstance.servingClaim.Store(servingClaimRun)

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil != handler || nil == handlerErr || "the http handler is asked of an application that runs its own server" != handlerErr.Error() {
        t.Fatalf("expected the handler refused once Run started, got %v %v", handler, handlerErr)
    }
}

func TestApplicationRun_AfterTheHandlerWasHandedOut_Panics(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)
    mustHttpHandler(t, applicationInstance)

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.Run()
    }, "application run called after its http handler was handed out")
}

func TestApplicationBeginHttpShutdown_HandsTheContextToEachHookOnce(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("first"))
        applicationInstance.OnHttpShutdown(recorder.hook("second"))
    })
    mustHttpHandler(t, applicationInstance)

    shutdownContext := context.WithValue(context.Background(), handlerTestContextKey{}, "the shutdown's")

    for range 2 {
        if beginErr := applicationInstance.BeginHttpShutdown(shutdownContext); nil != beginErr {
            t.Fatalf("unexpected begin error: %v", beginErr)
        }
    }

    waitForHttpShutdownHooks(&applicationInstance.httpShutdownHookSet.done, context.Background())

    if 2 != recorder.calls.Load() {
        t.Fatalf("expected each hook started once, got %d calls", recorder.calls.Load())
    }

    for _, name := range []string{"first", "second"} {
        if "the shutdown's" != recorder.contextOf(name) {
            t.Fatalf("expected %s handed the shutdown's context, got %v", name, recorder.contextOf(name))
        }
    }
}

func TestApplicationBeginHttpShutdown_NilContext_Refused(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)
    mustHttpHandler(t, applicationInstance)

    var nilContext context.Context

    beginErr := applicationInstance.BeginHttpShutdown(nilContext)
    if nil == beginErr || "the shutdown context is nil" != beginErr.Error() {
        t.Fatalf("expected the nil context refused, got %v", beginErr)
    }

    if "Application.BeginHttpShutdown" != exception.LogContext(beginErr, exception.LogContextConfig{})["door"] {
        t.Fatalf("expected the refusal to name the door, got %v", exception.LogContext(beginErr, exception.LogContextConfig{}))
    }
}

func TestApplicationBeginHttpShutdown_OfARunApplication_StartsNothing(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, stopRun, runEnded := runServingTestApplication(t, func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("only"))
    })

    if beginErr := applicationInstance.BeginHttpShutdown(context.Background()); nil != beginErr {
        t.Fatalf("unexpected begin error: %v", beginErr)
    }

    waitForHttpShutdownHooks(&applicationInstance.httpShutdownHookSet.done, context.Background())

    if 0 != recorder.calls.Load() {
        t.Fatalf("expected no hook started for an application that runs its own server, got %d", recorder.calls.Load())
    }

    stopRun()
    <-runEnded

    if 1 != recorder.calls.Load() {
        t.Fatalf("expected the hook started once, by the server Run owns, got %d", recorder.calls.Load())
    }
}

func TestApplicationCloseWithContext_HandedOut_RunsTheHooksUnderTheCloseContext(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("only"))
    })
    mustHttpHandler(t, applicationInstance)

    closeContext := context.WithValue(context.Background(), handlerTestContextKey{}, "the close's")

    if closeErr := applicationInstance.CloseWithContext(closeContext); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.calls.Load() || "the close's" != recorder.contextOf("only") {
        t.Fatalf("expected the hook run once under the close context before the close returned, got %d calls, context %v", recorder.calls.Load(), recorder.contextOf("only"))
    }
}

func TestApplicationCloseWithContext_HandedOut_WaitsForAnOpenRequestScope(t *testing.T) {
    applicationInstance, entered, release := bootHandlerTestApplication(t, "", nil)
    served := holdOpenRequestScope(t, mustHttpHandler(t, applicationInstance), entered)
    defer served.Wait()
    defer close(release)

    closeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    closeErr := applicationInstance.CloseWithContext(closeContext)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "http shutdown left request scopes open") || false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the open request scope reported under the close's deadline, got %v", closeErr)
    }
}

func TestApplicationCloseWithContext_HandedOut_HooksBegunEarlier_NotStartedTwice(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("only"))
    })
    mustHttpHandler(t, applicationInstance)

    if beginErr := applicationInstance.BeginHttpShutdown(context.WithValue(context.Background(), handlerTestContextKey{}, "the shutdown's")); nil != beginErr {
        t.Fatalf("unexpected begin error: %v", beginErr)
    }

    if closeErr := applicationInstance.CloseWithContext(context.WithValue(context.Background(), handlerTestContextKey{}, "the close's")); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.calls.Load() || "the shutdown's" != recorder.contextOf("only") {
        t.Fatalf("expected the hook begun earlier not started again by the close, got %d calls, context %v", recorder.calls.Load(), recorder.contextOf("only"))
    }
}

func TestApplicationCloseWithContext_OfARunApplication_StartsNoHook(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, stopRun, runEnded := runServingTestApplication(t, func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("only"))
    })

    stopRun()
    <-runEnded

    if closeErr := applicationInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.calls.Load() {
        t.Fatalf("expected the hook started once, by the server Run owns, and never by the close, got %d", recorder.calls.Load())
    }
}

func TestApplicationCloseWithContext_HandedOut_JoinsTheWindDownFailureWithTheTeardowns(t *testing.T) {
    applicationInstance, entered, release := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterService(
            "app.failing.close",
            func(resolver containercontract.Resolver) (*failingCloseProbe, error) {
                return &failingCloseProbe{}, nil
            },
            containercontract.RegisterOptions{},
        )
    })

    if _, getErr := applicationInstance.kernel.ServiceContainer().Get(t.Context(), "app.failing.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    served := holdOpenRequestScope(t, mustHttpHandler(t, applicationInstance), entered)
    defer served.Wait()
    defer close(release)

    closeContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    closeErr := applicationInstance.CloseWithContext(closeContext)
    if nil == closeErr || false == strings.Contains(closeErr.Error(), "http shutdown left request scopes open") || false == strings.Contains(closeErr.Error(), "failed to close container services") {
        t.Fatalf("expected the wind-down's failure joined with the teardown's, got %v", closeErr)
    }
}

func TestApplicationHttpHandler_ConcurrentCalls_BuildOneHandler(t *testing.T) {
    var decorations atomic.Int64

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            decorations.Add(1)

            return next
        })
    })

    callers := sync.WaitGroup{}
    var failures atomic.Int64

    for range 8 {
        callers.Add(1)

        go func() {
            defer callers.Done()

            if _, handlerErr := applicationInstance.HttpHandler(); nil != handlerErr {
                failures.Add(1)
            }
        }()
    }

    callers.Wait()

    if 0 != failures.Load() || 1 != decorations.Load() {
        t.Fatalf("expected every concurrent caller answered the one handler, got %d failures and %d decorations", failures.Load(), decorations.Load())
    }
}

func TestApplicationBeginHttpShutdown_ConcurrentCalls_StartEachHookOnce(t *testing.T) {
    recorder := &shutdownHookRecorder{}

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(recorder.hook("only"))
    })
    mustHttpHandler(t, applicationInstance)

    callers := sync.WaitGroup{}

    for range 8 {
        callers.Add(1)

        go func() {
            defer callers.Done()

            _ = applicationInstance.BeginHttpShutdown(context.Background())
        }()
    }

    callers.Wait()

    if closeErr := applicationInstance.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 1 != recorder.calls.Load() {
        t.Fatalf("expected the hook started once by concurrent callers and the close, got %d", recorder.calls.Load())
    }
}

/* runServingTestApplication runs the application's own server on a loopback listener it bound and answers once that server answers its probe, with the door that stops it and the channel closed when Run returned */
func runServingTestApplication(t *testing.T, prepare func(applicationInstance *Application)) (*Application, func(), chan struct{}) {
    t.Helper()

    listener, address := listenLoopback(t)

    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpAddressKey+"="+address+"\n", func(applicationInstance *Application) {
        registerServingProbe(t, applicationInstance)

        if nil != prepare {
            prepare(applicationInstance)
        }
    })
    applicationInstance.httpListener = listener

    stopRun, runEnded := startServingRun(t, applicationInstance, address)

    return applicationInstance, stopRun, runEnded
}

/* startServingRun runs the application on its own goroutine and answers once its server answers the probe on address, with the door that stops it and the channel closed when Run returned */
func startServingRun(t *testing.T, applicationInstance *Application, address string) (func(), chan struct{}) {
    t.Helper()

    runContext, stopRun := context.WithCancel(context.Background())
    t.Cleanup(stopRun)
    applicationInstance.ctx = runContext

    originalExit := applicationExit
    applicationExit = func(code int) { t.Errorf("expected the run to end cleanly, exited %d", code) }
    t.Cleanup(func() { applicationExit = originalExit })

    runEnded := make(chan struct{})
    go func() {
        defer close(runEnded)

        applicationInstance.Run()
    }()

    awaitServing(t, address)

    return stopRun, runEnded
}

func awaitRunEnded(t *testing.T, stopRun func(), runEnded chan struct{}) {
    t.Helper()

    stopRun()

    select {
    case <-runEnded:
    case <-time.After(10 * time.Second):
        t.Fatalf("expected the run to end once its context was cancelled")
    }
}

/* the one serving test without the listener seam: the server listens on the configured address itself, so its port is taken free and may race another process */
func TestApplicationRun_ServesOnTheConfiguredAddress(t *testing.T) {
    address := freeLoopbackAddress(t)

    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpAddressKey+"="+address+"\n", func(applicationInstance *Application) {
        registerServingProbe(t, applicationInstance)
    })

    stopRun, runEnded := startServingRun(t, applicationInstance, address)

    response, requestErr := (&nethttp.Client{Timeout: 5 * time.Second}).Get("http://" + address + "/plain")
    if nil != requestErr {
        t.Fatalf("unexpected request error: %v", requestErr)
    }
    body, _ := io.ReadAll(response.Body)
    _ = response.Body.Close()

    if nethttp.StatusOK != response.StatusCode || "plain" != string(body) {
        t.Fatalf("expected the route served on the configured address, got %d %q", response.StatusCode, body)
    }

    awaitRunEnded(t, stopRun, runEnded)
}

/* Run waits for a handler build in progress instead of judging its claim: a build that hands nothing out leaves Run to serve, one that hands the handler out refuses Run as handed out */
func TestApplicationRun_DuringAHandlerBuild_WaitsForItsOutcome(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        handsOut bool
    }{
        {name: "a build that hands the handler out", handsOut: true},
        {name: "a build that hands nothing out", handsOut: false},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            listener, address := listenLoopback(t)

            buildEntered := make(chan struct{})
            releaseBuild := make(chan struct{})
            var builds atomic.Int64

            applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpAddressKey+"="+address+"\n", func(applicationInstance *Application) {
                registerServingProbe(t, applicationInstance)

                applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
                    if 1 != builds.Add(1) {
                        return next
                    }

                    close(buildEntered)
                    <-releaseBuild

                    if false == testCase.handsOut {
                        return nil
                    }

                    return next
                })
            })
            applicationInstance.httpListener = listener

            handlerEnded := make(chan error, 1)
            go func() {
                _, handlerErr := applicationInstance.HttpHandler()
                handlerEnded <- handlerErr
            }()

            <-buildEntered

            if true == testCase.handsOut {
                runPanicked := make(chan any, 1)
                go func() {
                    defer func() { runPanicked <- recover() }()

                    applicationInstance.Run()
                }()

                select {
                case recovered := <-runPanicked:
                    t.Fatalf("expected Run to wait for the build, it ended with %v", recovered)
                case <-time.After(100 * time.Millisecond):
                }

                close(releaseBuild)

                if handlerErr := <-handlerEnded; nil != handlerErr {
                    t.Fatalf("expected the handler handed out, got %v", handlerErr)
                }

                recovered := <-runPanicked
                recoveredErr, isError := recovered.(error)
                if false == isError || "application run called after its http handler was handed out" != recoveredErr.Error() {
                    t.Fatalf("expected Run refused as handed out, got %v", recovered)
                }

                return
            }

            go func() {
                time.Sleep(100 * time.Millisecond)
                close(releaseBuild)
            }()

            stopRun, runEnded := startServingRun(t, applicationInstance, address)

            if handlerErr := <-handlerEnded; nil == handlerErr {
                t.Fatalf("expected the build that answered a nil handler refused")
            }

            awaitRunEnded(t, stopRun, runEnded)
        })
    }
}

func TestApplicationHttpHandler_ADecoratorPanicking_LeavesNoNilHandlerBehind(t *testing.T) {
    var decorations atomic.Int64

    applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            if 1 == decorations.Add(1) {
                panic("the decorator's first build panics")
            }

            return next
        })
    })

    func() {
        defer func() { _ = recover() }()

        _, _ = applicationInstance.HttpHandler()
    }()

    handler, handlerErr := applicationInstance.HttpHandler()
    if nil == handler || nil != handlerErr {
        t.Fatalf("expected the build retried after the recovered panic, got %v %v", handler, handlerErr)
    }

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/plain", nil))
    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the retried handler to serve the route, got %d", recorder.Code)
    }
}

func TestApplicationHttpHandler_HandingNothingOut_LeavesTheApplicationUnclaimed(t *testing.T) {
    t.Run("a decorator panicking", func(t *testing.T) {
        applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
            applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
                panic("the decorator panics")
            })
        })

        func() {
            defer func() { _ = recover() }()

            _, _ = applicationInstance.HttpHandler()
        }()

        if claim := applicationInstance.servingClaim.Load(); servingClaimNone != claim {
            t.Fatalf("expected nothing handed out to leave the application unclaimed, got claim %d", claim)
        }
    })

    t.Run("a refused build", func(t *testing.T) {
        applicationInstance, _, _ := bootHandlerTestApplication(t, config.ScopeTeardownTimeoutKey+"=10s\n"+config.HttpShutdownTimeoutKey+"=5s\n", nil)

        if _, handlerErr := applicationInstance.HttpHandler(); nil == handlerErr {
            t.Fatalf("expected the build refused")
        }

        if claim := applicationInstance.servingClaim.Load(); servingClaimNone != claim {
            t.Fatalf("expected nothing handed out to leave the application unclaimed, got claim %d", claim)
        }
    })

    t.Run("a handed-out handler", func(t *testing.T) {
        applicationInstance, _, _ := bootHandlerTestApplication(t, "", nil)
        mustHttpHandler(t, applicationInstance)

        if claim := applicationInstance.servingClaim.Load(); servingClaimHttpHandler != claim {
            t.Fatalf("expected the handed-out handler to keep the claim, got claim %d", claim)
        }
    })
}

/* the marker tells a re-executed test binary that it is the child whose boot dies inside HttpHandler */
const httpHandlerBootFailureProbeMarker = "MELODY_TEST_HTTP_HANDLER_BOOT_FAILURE_PROBE"

/* a boot that dies inside HttpHandler exits through the before-exit close, which must not wait on the build HttpHandler still holds: the exit comes well inside the teardown budget and the container's teardown ran */
func TestApplicationHttpHandler_ABootFailureExitsPromptly_AndTearsTheContainerDown(t *testing.T) {
    projectDirectory := os.Getenv(httpHandlerBootFailureProbeMarker)

    if "" != projectDirectory {
        environment, environmentErr := config.NewEnvironment(context.Background(), &mapEnvironmentSource{values: map[string]string{config.EnvironmentNameKey: config.EnvironmentDevelopment}})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory, config.ConfigurationConfig{})
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(RuntimeFlagsConfig{Mode: config.ModeHttp}),
            kernel:               newTestKernel(),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.RegisterService(
            "service.test.failing.closer",
            func(resolver containercontract.Resolver) (*failingCloser, error) {
                return &failingCloser{}, nil
            },
            container.RegisterOptions{},
        )

        container.MustFromResolver[*failingCloser](t.Context(), applicationInstance.kernel.ServiceContainer(), "service.test.failing.closer")

        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})
        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})

        _, _ = applicationInstance.HttpHandler()

        os.Exit(93)
    }

    child := exec.Command(os.Args[0], "-test.run=^TestApplicationHttpHandler_ABootFailureExitsPromptly_AndTearsTheContainerDown$")
    child.Env = append(os.Environ(), httpHandlerBootFailureProbeMarker+"="+t.TempDir())

    startedAt := time.Now()
    output, runErr := child.CombinedOutput()
    elapsed := time.Since(startedAt)

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError || 1 != exitError.ExitCode() {
        t.Fatalf("expected the child to exit 1, got err %v with output: %s", runErr, string(output))
    }

    if 3*time.Second < elapsed {
        t.Fatalf("expected the exit well inside the 10s teardown budget, took %v with output: %s", elapsed, string(output))
    }

    if false == strings.Contains(string(output), "failed to close service container") {
        t.Fatalf("expected the before-exit close to tear the container down, got: %s", string(output))
    }
}

func TestApplicationHttpHandler_ADecoratorAnsweringNil_Refused(t *testing.T) {
    var typedNil *nethttp.ServeMux

    rows := map[string]nethttp.Handler{
        "nil":       nil,
        "typed nil": typedNil,
    }

    for name, answer := range rows {
        t.Run(name, func(t *testing.T) {
            var outerRan atomic.Bool

            applicationInstance, _, _ := bootHandlerTestApplication(t, "", func(applicationInstance *Application) {
                applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
                    outerRan.Store(true)

                    return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {})
                })
                applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
                    return answer
                })
            })

            handler, handlerErr := applicationInstance.HttpHandler()
            if nil != handler || nil == handlerErr || "http handler decorator answered nil" != handlerErr.Error() {
                t.Fatalf("expected the nil answer refused, got %v %v", handler, handlerErr)
            }

            if 1 != exception.LogContext(handlerErr, exception.LogContextConfig{})["index"] {
                t.Fatalf("expected the refusal to name the decorator's index 1, got %v", exception.LogContext(handlerErr, exception.LogContextConfig{}))
            }

            if true == outerRan.Load() {
                t.Fatalf("expected the outer decorator never handed the nil answer")
            }

            _, secondErr := applicationInstance.HttpHandler()
            if handlerErr != secondErr {
                t.Fatalf("expected the refusal memoized, got %v then %v", handlerErr, secondErr)
            }
        })
    }
}

func TestRunHttp_ADecoratorAnsweringNil_Refused(t *testing.T) {
    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpAddressKey+"="+freeLoopbackAddress(t)+"\n", func(applicationInstance *Application) {
        applicationInstance.RegisterHttpHandlerDecorator(func(next nethttp.Handler) nethttp.Handler {
            return nil
        })
    })

    runErr := applicationInstance.runHttp(context.Background())
    if nil == runErr || "http handler decorator answered nil" != runErr.Error() {
        t.Fatalf("expected the run refused before serving, got %v", runErr)
    }

    if 0 != exception.LogContext(runErr, exception.LogContextConfig{})["index"] {
        t.Fatalf("expected the refusal to name the decorator's index 0, got %v", exception.LogContext(runErr, exception.LogContextConfig{}))
    }
}

func TestApplicationClose_HandedOut_BoundsTheWindDownByTheShutdownTimeout(t *testing.T) {
    var hookDeadline atomic.Bool

    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpShutdownTimeoutKey+"=100ms\n"+config.ScopeTeardownTimeoutKey+"=10ms\n", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(func(shutdownContext context.Context) {
            _, hasDeadline := shutdownContext.Deadline()
            hookDeadline.Store(hasDeadline)

            <-shutdownContext.Done()
        })
    })
    mustHttpHandler(t, applicationInstance)

    closed := make(chan struct{})
    go func() {
        defer close(closed)

        applicationInstance.Close()
    }()

    select {
    case <-closed:
    case <-time.After(3 * time.Second):
        t.Fatalf("expected the close bounded by the 100ms shutdown timeout; it is still waiting")
    }

    if false == hookDeadline.Load() {
        t.Fatalf("expected the hook handed a context carrying the shutdown timeout's deadline")
    }
}

/* bootDeadlineCloseTestApplication hands out the handler of an application whose shutdown hook blocks until its context ends and whose one built service closes through closeProbe */
func bootDeadlineCloseTestApplication(t *testing.T, shutdownTimeout string, closeProbe func(closeContext context.Context) error) *Application {
    t.Helper()

    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpShutdownTimeoutKey+"="+shutdownTimeout+"\n"+config.ScopeTeardownTimeoutKey+"=10ms\n", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(func(shutdownContext context.Context) {
            <-shutdownContext.Done()
        })
        applicationInstance.RegisterService(
            "app.context.close",
            func(resolver containercontract.Resolver) (*contextClosingApplicationProbe, error) {
                return &contextClosingApplicationProbe{close: closeProbe}, nil
            },
            containercontract.RegisterOptions{TypeRegistrationDisabled: true},
        )
    })

    if _, getErr := applicationInstance.kernel.ServiceContainer().Get(t.Context(), "app.context.close"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    mustHttpHandler(t, applicationInstance)

    return applicationInstance
}

func TestApplicationCloseWithContext_HandedOut_TheCallersDeadlineBoundsTheWholeClose(t *testing.T) {
    teardownContextErr := make(chan error, 1)

    applicationInstance := bootDeadlineCloseTestApplication(t, "5s", func(closeContext context.Context) error {
        select {
        case <-closeContext.Done():
        case <-time.After(2 * time.Second):
        }
        teardownContextErr <- closeContext.Err()

        return nil
    })

    closeContext, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
    defer cancel()

    startedAt := time.Now()
    _ = applicationInstance.CloseWithContext(closeContext)

    if elapsed := time.Since(startedAt); 500*time.Millisecond < elapsed {
        t.Fatalf("expected the whole close bounded by the caller's 300ms deadline, took %v", elapsed)
    }

    select {
    case err := <-teardownContextErr:
        if false == errors.Is(err, context.DeadlineExceeded) {
            t.Fatalf("expected the teardown to run under what is left of the caller's context, got %v", err)
        }
    default:
        t.Fatalf("expected the teardown to close the service")
    }
}

func TestApplicationCloseWithContext_HandedOut_ACallersCancelReachesTheTeardown(t *testing.T) {
    teardownContextErr := make(chan error, 1)

    applicationInstance := bootDeadlineCloseTestApplication(t, "5s", func(closeContext context.Context) error {
        teardownContextErr <- closeContext.Err()

        return nil
    })

    closeContext, cancel := context.WithTimeout(context.Background(), time.Hour)
    defer cancel()

    time.AfterFunc(100*time.Millisecond, cancel)

    startedAt := time.Now()
    _ = applicationInstance.CloseWithContext(closeContext)

    if elapsed := time.Since(startedAt); 2*time.Second < elapsed {
        t.Fatalf("expected the caller's cancel to end the wind-down, took %v", elapsed)
    }

    select {
    case err := <-teardownContextErr:
        if false == errors.Is(err, context.Canceled) {
            t.Fatalf("expected the caller's cancel to reach the teardown, got %v", err)
        }
    default:
        t.Fatalf("expected the teardown to close the service")
    }
}

func TestApplicationCloseWithContext_HandedOut_TheShutdownTimeoutEarlier_TheTeardownGetsWhatIsLeft(t *testing.T) {
    type teardownObservation struct {
        err       error
        remaining time.Duration
    }

    observed := make(chan teardownObservation, 1)

    applicationInstance := bootDeadlineCloseTestApplication(t, "200ms", func(closeContext context.Context) error {
        deadline, hasDeadline := closeContext.Deadline()
        if false == hasDeadline {
            observed <- teardownObservation{err: errors.New("the teardown context carries no deadline")}

            return nil
        }

        observed <- teardownObservation{err: closeContext.Err(), remaining: time.Until(deadline)}

        return nil
    })

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    _ = applicationInstance.CloseWithContext(closeContext)

    select {
    case observation := <-observed:
        if nil != observation.err {
            t.Fatalf("expected the teardown under a live context once the shutdown timeout ended the wind-down, got %v", observation.err)
        }

        if 0 >= observation.remaining || 850*time.Millisecond < observation.remaining {
            t.Fatalf("expected the teardown to get what is left of the caller's 1s after the 200ms wind-down, got %v", observation.remaining)
        }
    default:
        t.Fatalf("expected the teardown to close the service")
    }
}

func TestApplicationClose_HandedOut_WaitsForAHookWithinTheShutdownTimeout(t *testing.T) {
    var hookReturned atomic.Bool

    applicationInstance, _, _ := bootHandlerTestApplication(t, config.HttpShutdownTimeoutKey+"=5s\n"+config.ScopeTeardownTimeoutKey+"=10ms\n", func(applicationInstance *Application) {
        applicationInstance.OnHttpShutdown(func(shutdownContext context.Context) {
            time.Sleep(100 * time.Millisecond)

            hookReturned.Store(true)
        })
    })
    mustHttpHandler(t, applicationInstance)

    applicationInstance.Close()

    if false == hookReturned.Load() {
        t.Fatalf("expected the close to wait for the hook within the 5s shutdown timeout")
    }
}
