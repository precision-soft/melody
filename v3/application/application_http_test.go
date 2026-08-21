package application

import (
    "context"
    "errors"
    nethttp "net/http"
    "os"
    "os/exec"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/config"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* @info http.Server.Shutdown neither cancels an in-flight request's context nor tracks a hijacked connection, so a Server-Sent Events stream or a websocket blocks the entire shutdown timeout and is then cut mid-flight. The hook is what lets the application release those handlers, and net/http runs it the moment Shutdown begins. */
func TestOnHttpShutdown_HooksRunWhenTheServerShutsDown(t *testing.T) {
    released := make(chan struct{})

    httpServer := &nethttp.Server{Handler: nethttp.NotFoundHandler()}
    httpServer.RegisterOnShutdown(func() { close(released) })

    shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    if shutdownErr := httpServer.Shutdown(shutdownContext); nil != shutdownErr {
        t.Fatalf("shutdown: %v", shutdownErr)
    }

    select {
    case <-released:
    case <-time.After(2 * time.Second):
        t.Fatalf("the shutdown hook never ran")
    }
}

/* @info The hook list reaches the server: a nil hook is rejected outright, and a registered one is kept. */
func TestOnHttpShutdown_RejectsNilAndKeepsTheHook(t *testing.T) {
    applicationInstance := &Application{}

    applicationInstance.OnHttpShutdown(func() {})
    if 1 != len(applicationInstance.httpShutdownHooks) {
        t.Fatalf("expected the hook to be registered, got %d", len(applicationInstance.httpShutdownHooks))
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("a nil shutdown hook must panic")
        }
    }()

    applicationInstance.OnHttpShutdown(nil)
}

/* @info OnHttpShutdown copies its hooks into the net/http server once, on the main goroutine, before ListenAndServe; a hook registered after boot never reaches the server (silently dropped) and the append races, so — like every sibling registrar — it must panic once the application has booted. */
func TestOnHttpShutdown_PanicsAfterBoot(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.OnHttpShutdown(func() {})
    }, "may not register http shutdown hooks after boot")
}

/* @info net/http runs each shutdown hook on a bare `go f()`, where a panic cannot be recovered by Run's logOnRecoverAndExit and would hard-crash the process mid-drain, skipping Application.Close(). The wrapper recovers the panic through the framework logger and still marks the hook done, so a panicking hook is contained like every other extension point. */
func TestWrapHttpShutdownHook_ContainsAPanicAndStillCompletes(t *testing.T) {
    var hooksDone sync.WaitGroup

    wrapped := wrapHttpShutdownHook(
        func() {
            exception.Panic(exception.NewError("shutdown hook boom", nil, nil))
        },
        &hooksDone,
        logging.NewNopLogger(),
    )

    /* net/http runs the hook on its own goroutine; an unrecovered panic there would crash the whole test binary */
    hookReturned := make(chan struct{})
    go func() {
        wrapped()

        close(hookReturned)
    }()

    select {
    case <-hookReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("the wrapped shutdown hook never returned")
    }

    hooksDone.Wait()
}

/* @info Shutdown starts the hooks on detached goroutines and returns immediately when the only open connections are hijacked, so runHttp must join the hooks before returning. waitForHttpShutdownHooks must not report done until the registered hook has actually finished. */
func TestWaitForHttpShutdownHooks_BlocksUntilTheHookCompletes(t *testing.T) {
    var hooksDone sync.WaitGroup

    hookFinished := make(chan struct{})

    wrapped := wrapHttpShutdownHook(
        func() {
            time.Sleep(50 * time.Millisecond)

            close(hookFinished)
        },
        &hooksDone,
        logging.NewNopLogger(),
    )

    /* net/http starts each shutdown hook on its own goroutine */
    go wrapped()

    waitReturned := make(chan struct{})
    go func() {
        waitForHttpShutdownHooks(&hooksDone, context.Background())

        close(waitReturned)
    }()

    select {
    case <-waitReturned:
        select {
        case <-hookFinished:
        default:
            t.Fatalf("the wait returned before the shutdown hook completed")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("the wait never returned")
    }
}

/* @info The join is bounded by the shutdown budget: a hook that never finishes must not pin the process forever, so a spent context releases the wait. */
func TestWaitForHttpShutdownHooks_ReturnsWhenTheBudgetIsSpent(t *testing.T) {
    var hooksDone sync.WaitGroup

    /* a hook that never completes */
    hooksDone.Add(1)

    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    waitReturned := make(chan struct{})
    go func() {
        waitForHttpShutdownHooks(&hooksDone, ctx)

        close(waitReturned)
    }()

    select {
    case <-waitReturned:
    case <-time.After(2 * time.Second):
        t.Fatalf("the wait ignored the spent shutdown budget and hung")
    }
}

/* the reproduction below cannot be observed from inside the test binary: the unpatched wrapper answers an *exception.ExitError with os.Exit, which no recover intercepts and which would simply end the test run. TestMain therefore doubles as a probe entry point — when the environment variable names a probe the process runs that reproduction and exits zero instead of running the suite — and the test re-executes this binary, bounds it with a deadline and asserts on the child's exit status. */
const shutdownHookProbeEnvironmentVariable = "MELODY_APPLICATION_SHUTDOWN_HOOK_PROBE"

const shutdownHookProbeExitCode = 7

func TestMain(mainInstance *testing.M) {
    probeName := os.Getenv(shutdownHookProbeEnvironmentVariable)
    if "" != probeName {
        runShutdownHookProbe(probeName)

        os.Exit(0)
    }

    os.Exit(mainInstance.Run())
}

func runShutdownHookProbe(probeName string) {
    switch probeName {
    case "hookAsksToExit":
        var hooksDone sync.WaitGroup

        wrapped := wrapHttpShutdownHook(
            func() {
                exception.Exit(
                    exception.NewExitError(
                        shutdownHookProbeExitCode,
                        exception.NewError("shutdown hook asked to exit", nil, nil),
                    ),
                )
            },
            &hooksDone,
            logging.NewNopLogger(),
        )

        wrapped()

        hooksDone.Wait()

    default:
        os.Exit(97)
    }
}

/* @info logging.LogOnRecover answers an *exception.ExitError with os.Exit, and a shutdown hook runs while the server is still draining: an exit there cuts the in-flight requests the drain exists to finish, skips the remaining hooks and skips Application.Close(). The wrapper must log the hook's demand and let the drain run to the end. */
func TestWrapHttpShutdownHook_DoesNotExitTheProcessWhenTheHookAsksTo(t *testing.T) {
    binaryPath, executableErr := os.Executable()
    if nil != executableErr {
        t.Fatalf("could not locate the test binary to re-execute: %v", executableErr)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    command := exec.CommandContext(ctx, binaryPath)
    command.Env = append(os.Environ(), shutdownHookProbeEnvironmentVariable+"=hookAsksToExit")

    combinedOutput, runErr := command.CombinedOutput()

    if nil != ctx.Err() {
        t.Fatalf("the shutdown hook probe never returned and had to be killed; output: %s", combinedOutput)
    }

    if nil == runErr {
        return
    }

    exitErr := (*exec.ExitError)(nil)
    if true == errors.As(runErr, &exitErr) {
        if shutdownHookProbeExitCode == exitErr.ExitCode() {
            t.Fatalf("the shutdown hook terminated the process with its own exit code %d, cutting the drain short; output: %s", exitErr.ExitCode(), combinedOutput)
        }

        t.Fatalf("the shutdown hook probe died with exit status %d instead of returning; output: %s", exitErr.ExitCode(), combinedOutput)
    }

    t.Fatalf("could not run the shutdown hook probe: %v; output: %s", runErr, combinedOutput)
}

/* @info an exit demand from a hook is downgraded, not discarded: the error it carries is what reaches the log, so the reason the hook wanted the process gone is still on the record */
func TestHttpShutdownHookError_UnwrapsTheErrorAnExitDemandCarries(t *testing.T) {
    carried := exception.NewError("shutdown hook asked to exit", nil, nil)

    resolved := httpShutdownHookError(
        exception.NewExitError(shutdownHookProbeExitCode, carried),
    )

    if carried != resolved {
        t.Fatalf("expected the error carried by the exit demand, got %+v", resolved)
    }
}

/* @info a value that is not an error at all still has to reach the log as one, so a hook panicking with a bare string is not silently swallowed */
func TestHttpShutdownHookError_WrapsANonErrorPanicValue(t *testing.T) {
    resolved := httpShutdownHookError("bare string panic")

    if nil == resolved {
        t.Fatalf("expected a non-nil error for a bare panic value")
    }
    if "panic in http shutdown hook" != resolved.Message() {
        t.Fatalf("unexpected message for a bare panic value: %q", resolved.Message())
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

/* @info The unbounded default cache keeps every key for the life of the process, which only matters in a process that stays up; the http path is where the application is told, once, at boot. */
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

/* @info An application that bounded its own cache backend must not be nagged, or the warning stops meaning anything. */
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

/* @info The storage melody wired itself lives in this process and nothing outside it can expire an entry; with a ttl of zero nothing inside it can either. A request that arrives without a session cookie gets a session regardless of who sent it, so the pair is the one combination in which an unauthenticated caller decides how much memory this process keeps. */
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

/* @info a lifetime the deployment chose reclaims the entries the default storage holds, so there is nothing left to warn about. */
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

/* @info a storage the application registered is the operator's to expire, so an unbounded ttl beside it is a choice rather than a hazard melody introduced. */
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
