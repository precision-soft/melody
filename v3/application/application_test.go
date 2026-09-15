package application

import (
    "context"
    nethttp "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    }, "test")
}

func applicationBootRouteHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }
}

type dynamicRouteModule struct {
    fakeModule
}

func (instance dynamicRouteModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
    kernelInstance.HttpRouter().Handle(nethttp.MethodGet, "/users/:id", applicationBootRouteHandler())
}

func TestBoot_TheRootsRoutesRegisterBeforeAnyModules(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterHttpRoute(nethttp.MethodGet, "/users/me", applicationBootRouteHandler())
    applicationInstance.RegisterModule(dynamicRouteModule{fakeModule{name: "dynamic-route"}})

    kernelInstance := applicationInstance.Boot()

    matchResult, matched := kernelInstance.HttpRouter().Match(nethttp.MethodGet, "/users/me", "", "")
    if false == matched {
        t.Fatal("expected /users/me to match a route")
    }

    if identifier, hasIdentifier := matchResult.Params["id"]; true == hasIdentifier {
        t.Fatalf("expected the root's static route to win the dispatch, but the module's /users/:id matched with id=%q", identifier)
    }
}

type servingProbeApplicationCommand struct {
    ran        bool
    resolveErr error
}

func (instance *servingProbeApplicationCommand) Name() string {
    return "probe:serving"
}

func (instance *servingProbeApplicationCommand) Description() string {
    return "reports whether the configuration refuses a resolve while the command runs"
}

func (instance *servingProbeApplicationCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *servingProbeApplicationCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    instance.ran = true

    configuration := config.ConfigMustFromContainer(runtimeInstance.Container())
    instance.resolveErr = configuration.Resolve()

    return nil
}

func TestRun_MarksTheConfigurationServingBeforeItDispatches(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    defer func() { os.Args = originalArguments }()

    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probe := &servingProbeApplicationCommand{}
    applicationInstance.RegisterCliCommand(probe)

    applicationInstance.Run()

    if false == probe.ran {
        t.Fatal("the probe command never ran, so the assertion below would be vacuous")
    }

    if nil == probe.resolveErr {
        t.Fatal("expected the configuration to refuse a resolve while the command runs, which means Run never marked it serving")
    }

    if false == strings.Contains(probe.resolveErr.Error(), "begun serving") {
        t.Fatalf("expected the refusal to name the serving phase, got %q", probe.resolveErr.Error())
    }
}

type failingCloser struct{}

func (instance *failingCloser) Close() error {
    return exception.NewError("the probe service refuses to close", nil, nil)
}

func newFailingCloseApplication(t *testing.T) *Application {
    t.Helper()

    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterService(
        "service.test.failing.closer",
        func(resolver containercontract.Resolver) (*failingCloser, error) {
            return &failingCloser{}, nil
        },
    )

    container.MustFromResolver[*failingCloser](applicationInstance.kernel.ServiceContainer(), "service.test.failing.closer")

    return applicationInstance
}

func TestClose_DoesNotRereportAFailureSomebodyElseDiscovered(t *testing.T) {
    applicationInstance := newFailingCloseApplication(t)

    firstCloseErr := applicationInstance.kernel.ServiceContainer().Close()
    if nil == firstCloseErr {
        t.Fatalf("expected the discovering close to report the failure")
    }

    if closeErr := applicationInstance.close(context.Background()); nil != closeErr {
        t.Fatalf("expected the repeated close not to re-report the memoized failure, got: %v", closeErr)
    }
}

func TestClose_ReportsTheFailureItDiscoveredItself(t *testing.T) {
    applicationInstance := newFailingCloseApplication(t)

    closeErr := applicationInstance.close(context.Background())
    if nil == closeErr {
        t.Fatalf("expected the discovering close to report the teardown failure")
    }

    if false == strings.Contains(closeErr.Error(), "failed to close container services") {
        t.Fatalf("expected the container teardown failure, got: %v", closeErr)
    }
}

func TestCloseAndExitOnFailure_ExitsNonZeroOnATeardownFailureItDiscovered(t *testing.T) {
    exitedWith := -1
    originalExit := applicationExit
    applicationExit = func(code int) { exitedWith = code }
    defer func() { applicationExit = originalExit }()

    applicationInstance := newFailingCloseApplication(t)

    applicationInstance.closeAndExitOnFailure()

    if 1 != exitedWith {
        t.Fatalf("expected exit code 1 on a discovered teardown failure, got %d", exitedWith)
    }
}

func TestCloseAndExitOnFailure_StaysSilentOnACleanTeardown(t *testing.T) {
    exitedWith := -1
    originalExit := applicationExit
    applicationExit = func(code int) { exitedWith = code }
    defer func() { applicationExit = originalExit }()

    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.closeAndExitOnFailure()

    if -1 != exitedWith {
        t.Fatalf("expected no exit on a clean teardown, got code %d", exitedWith)
    }
}

func TestResolveExitLogger_PrefersTheContainerLoggerWhileItWrites(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    logFile, createErr := os.CreateTemp(t.TempDir(), "melody-exit-logger-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    fileLogger := logging.NewJsonLogger(logFile, loggingcontract.LevelInfo)

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return fileLogger, nil
        },
    )

    if fileLogger != applicationInstance.resolveExitLogger() {
        t.Fatalf("expected the live container logger to carry the final record")
    }
}

func TestResolveExitLogger_FallsBackWhenTheContainerLoggerIsClosed(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    logFile, createErr := os.CreateTemp(t.TempDir(), "melody-exit-logger-*.log")
    if nil != createErr {
        t.Fatalf("unexpected temp file error: %v", createErr)
    }

    fileLogger := logging.NewJsonLogger(logFile, loggingcontract.LevelInfo)

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return fileLogger, nil
        },
    )

    if fileLogger != applicationInstance.resolveExitLogger() {
        t.Fatalf("the container logger must be preferred before the teardown, or the fallback assertion below is vacuous")
    }

    if closeErr := applicationInstance.kernel.ServiceContainer().Close(); nil != closeErr {
        t.Fatalf("unexpected container close error: %v", closeErr)
    }

    if fileLogger == applicationInstance.resolveExitLogger() {
        t.Fatalf("expected the closed container logger to be refused for the final record")
    }
}

func TestResolveExitLogger_SurvivesANilKernel(t *testing.T) {
    applicationInstance := &Application{}

    if nil == applicationInstance.resolveExitLogger() {
        t.Fatalf("expected the emergency logger for a nil kernel")
    }
}

func TestRegisterSecretParameter_MarksTheRegistrationAsHoldingACredential(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterSecretParameter("app.token", "sk_live_51H")
    applicationInstance.RegisterParameter("app.pool", 12)

    secretParameter := applicationInstance.configuration.Get("app.token")
    if nil == secretParameter {
        t.Fatalf("expected the secret parameter to be registered")
    }
    if false == secretParameter.IsSecret() {
        t.Fatalf("expected the parameter registered through the secret door to be marked secret")
    }
    if "sk_live_51H" != secretParameter.String() {
        t.Fatalf("expected the value to be registered like any other, got %q", secretParameter.String())
    }

    ordinaryParameter := applicationInstance.configuration.Get("app.pool")
    if nil == ordinaryParameter {
        t.Fatalf("expected the ordinary parameter to be registered")
    }
    if true == ordinaryParameter.IsSecret() {
        t.Fatalf("expected the ordinary door to leave the parameter unmarked")
    }
}

func TestMarkParameterSecret_MarksWhatExistsAndQueuesWhatDoesNot(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterParameter("app.token", "sk_live_51H")

    applicationInstance.MarkParameterSecret("app.token")
    applicationInstance.MarkParameterSecret("app.absent")

    parameter := applicationInstance.configuration.Get("app.token")
    if nil == parameter || false == parameter.IsSecret() {
        t.Fatalf("expected the existing parameter to be marked at once")
    }

    if 1 != len(applicationInstance.unappliedSecretMarks) || "app.absent" != applicationInstance.unappliedSecretMarks[0] {
        t.Fatalf("expected the unmatched name to be queued for the retry, got %#v", applicationInstance.unappliedSecretMarks)
    }
}

func TestMarkParameterSecret_RefusesAfterBoot(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)
    applicationInstance.booted = true

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.MarkParameterSecret("app.token")
    }, "cannot mark a parameter secret after application boot")
}

func TestApplyUnappliedSecretMarks_AppliesWhatALaterRegistrationMadeReal(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.MarkParameterSecret("app.late")
    applicationInstance.MarkParameterSecret("app.never")

    if 2 != len(applicationInstance.unappliedSecretMarks) {
        t.Fatalf("expected both markings to be queued, got %#v", applicationInstance.unappliedSecretMarks)
    }

    applicationInstance.RegisterParameter("app.late", "sk_live_51H")

    applicationInstance.applyUnappliedSecretMarks()

    parameter := applicationInstance.configuration.Get("app.late")
    if nil == parameter || false == parameter.IsSecret() {
        t.Fatalf("expected the retry to mark the parameter its own registration arrived late for")
    }

    if 1 != len(applicationInstance.unappliedSecretMarks) || "app.never" != applicationInstance.unappliedSecretMarks[0] {
        t.Fatalf("expected the still-unmatched name to stay queued, got %#v", applicationInstance.unappliedSecretMarks)
    }
}

func TestWarnUnappliedSecretMarks_WarnsOnlyAboutWhatStillMatchesNothing(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    logger := &recordingLogger{}
    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logger, nil
        },
    )

    applicationInstance.MarkParameterSecret("app.misspelled")
    applicationInstance.MarkParameterSecret("app.registered.last")

    applicationInstance.RegisterParameter("app.registered.last", "sk_live_51H")

    applicationInstance.warnUnappliedSecretMarks()

    parameter := applicationInstance.configuration.Get("app.registered.last")
    if nil == parameter || false == parameter.IsSecret() {
        t.Fatalf("expected a name a late phase made real to be marked here rather than warned about")
    }

    if 1 != len(logger.warnings) {
        t.Fatalf("expected exactly one warning, got %d", len(logger.warnings))
    }

    if "app.misspelled" != logger.warnings[0]["parameterName"] {
        t.Fatalf("expected the warning to name the marking that matched nothing, got %+v", logger.warnings[0])
    }

    if 0 != len(applicationInstance.unappliedSecretMarks) {
        t.Fatalf("expected the queue to be emptied after the last retry, got %#v", applicationInstance.unappliedSecretMarks)
    }
}

func TestProcessRole_AnswersTheResolvedRole(t *testing.T) {
    applicationInstance := &Application{
        runtimeFlags: NewRuntimeFlagsWithRole(config.ModeCli, config.RoleWorker),
    }

    if config.RoleWorker != applicationInstance.ProcessRole() {
        t.Fatalf("expected the explicit role, got %q", applicationInstance.ProcessRole())
    }

    defaultRoleApplication := &Application{
        runtimeFlags: NewRuntimeFlags(config.ModeCli),
    }

    if config.RoleAll != defaultRoleApplication.ProcessRole() {
        t.Fatalf("expected an unset role to widen to all, got %q", defaultRoleApplication.ProcessRole())
    }
}

func newEnvironmentRefusalApplication(t *testing.T, mode string, environmentValues map[string]string) *Application {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: environmentValues})
    if nil != environmentErr {
        t.Fatalf("unexpected environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("unexpected configuration error: %v", configurationErr)
    }

    return &Application{
        ctx:                  context.Background(),
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(mode),
        kernel:               newTestKernel(),
        moduleConfigurations: make(map[string]any),
    }
}

func TestRefuseHttpBootWithoutEnvironment_RefusesAnHttpBootOnZeroKeys(t *testing.T) {
    applicationInstance := newEnvironmentRefusalApplication(t, config.ModeHttp, map[string]string{})

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.refuseHttpBootWithoutEnvironment()
    }, "no environment keys were loaded")
}

func TestRefuseHttpBootWithoutEnvironment_AcceptsAnHttpBootWithAnyKey(t *testing.T) {
    applicationInstance := newEnvironmentRefusalApplication(t, config.ModeHttp, map[string]string{config.EnvKey: "dev"})

    applicationInstance.refuseHttpBootWithoutEnvironment()
}

func TestRefuseHttpBootWithoutEnvironment_LeavesTheCliPermissive(t *testing.T) {
    applicationInstance := newEnvironmentRefusalApplication(t, config.ModeCli, map[string]string{})

    applicationInstance.refuseHttpBootWithoutEnvironment()
}

func TestRegisterConfiguration_RefusesANameNothingConsumes(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterConfiguration("Logging", "misspelled")
    }, "unknown configuration name")
}

const runPanicPathProbeMarker = "MELODY_TEST_RUN_PANIC_PATH_PROBE"

type panickingProbeApplicationCommand struct{}

func (instance *panickingProbeApplicationCommand) Name() string {
    return "probe:panic"
}

func (instance *panickingProbeApplicationCommand) Description() string {
    return "panics to drive the process-boundary handler"
}

func (instance *panickingProbeApplicationCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *panickingProbeApplicationCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    exception.Panic(exception.NewError("the probe command exploded", nil, nil))

    return nil
}

func TestRun_PanicPathWritesTheFatalRecordThroughTheLiveLoggerBeforeTeardown(t *testing.T) {
    projectDirectory := os.Getenv(runPanicPathProbeMarker)

    if "" != projectDirectory {
        environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: map[string]string{}})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(config.ModeCli),
            kernel:               newTestKernel(),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})

        os.Args = []string{"probe", "probe:panic"}

        applicationInstance.Run()

        os.Exit(93)
    }

    childProjectDirectory := t.TempDir()

    child := exec.Command(os.Args[0], "-test.run=TestRun_PanicPathWritesTheFatalRecordThroughTheLiveLoggerBeforeTeardown$")
    child.Env = append(os.Environ(), runPanicPathProbeMarker+"="+childProjectDirectory)

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError {
        t.Fatalf("expected the child to exit non-zero, got err %v with output: %s", runErr, string(output))
    }

    if 1 != exitError.ExitCode() {
        t.Fatalf("expected exit code 1, got %d with output: %s", exitError.ExitCode(), string(output))
    }

    logPath := filepath.Join(childProjectDirectory, "var", "log", config.EnvDevelopment+".log")

    logContent, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("expected the configured log file to exist at %s: %v; child output: %s", logPath, readErr, string(output))
    }

    if false == strings.Contains(string(logContent), "the probe command exploded") {
        t.Fatalf("expected the fatal record in the configured log file, got: %s; child output: %s", string(logContent), string(output))
    }

    if false == strings.Contains(string(output), "the probe command exploded") {
        t.Fatalf("expected the stderr echo to name the failure, got: %s", string(output))
    }
}

type typedNilProbeExitLogger struct {
    closedFlag bool
}

func (instance *typedNilProbeExitLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *typedNilProbeExitLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *typedNilProbeExitLogger) Info(message string, context loggingcontract.Context) {}

func (instance *typedNilProbeExitLogger) Warning(message string, context loggingcontract.Context) {
}

func (instance *typedNilProbeExitLogger) Error(message string, context loggingcontract.Context) {}

func (instance *typedNilProbeExitLogger) Emergency(message string, context loggingcontract.Context) {
}

func (instance *typedNilProbeExitLogger) Closed() bool {
    return instance.closedFlag
}

func TestResolveExitLogger_RefusesATypedNilContainerLogger(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return (*typedNilProbeExitLogger)(nil), nil
        },
    )

    resolvedLogger := applicationInstance.resolveExitLogger()

    if typedLogger, isTypedProbe := resolvedLogger.(*typedNilProbeExitLogger); true == isTypedProbe && nil == typedLogger {
        t.Fatalf("expected the typed-nil container logger to be refused for the final record")
    }

    if nil == resolvedLogger {
        t.Fatalf("expected the emergency logger, got nil")
    }
}

func TestClose_SurvivesANilKernel(t *testing.T) {
    applicationInstance := &Application{}

    applicationInstance.Close()
}

const bootPanicTeardownProbeMarker = "MELODY_TEST_BOOT_PANIC_TEARDOWN_PROBE"

func TestBoot_PanicPathRunsTheTeardownHook(t *testing.T) {
    projectDirectory := os.Getenv(bootPanicTeardownProbeMarker)

    if "" != projectDirectory {
        environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: map[string]string{}})
        if nil != environmentErr {
            os.Exit(91)
        }

        configuration, configurationErr := config.NewConfiguration(environment, projectDirectory)
        if nil != configurationErr {
            os.Exit(92)
        }

        applicationInstance := &Application{
            ctx:                  context.Background(),
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(config.ModeCli),
            kernel:               newTestKernel(),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.RegisterService(
            "service.test.failing.closer",
            func(resolver containercontract.Resolver) (*failingCloser, error) {
                return &failingCloser{}, nil
            },
        )

        container.MustFromResolver[*failingCloser](applicationInstance.kernel.ServiceContainer(), "service.test.failing.closer")

        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})
        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})

        applicationInstance.Boot()

        os.Exit(93)
    }

    childProjectDirectory := t.TempDir()

    child := exec.Command(os.Args[0], "-test.run=TestBoot_PanicPathRunsTheTeardownHook$")
    child.Env = append(os.Environ(), bootPanicTeardownProbeMarker+"="+childProjectDirectory)

    output, runErr := child.CombinedOutput()

    exitError, isExitError := runErr.(*exec.ExitError)
    if false == isExitError {
        t.Fatalf("expected the child to exit non-zero, got err %v with output: %s", runErr, string(output))
    }

    if 1 != exitError.ExitCode() {
        t.Fatalf("expected exit code 1, got %d with output: %s", exitError.ExitCode(), string(output))
    }

    if false == strings.Contains(string(output), "failed to close service container") {
        t.Fatalf("expected the teardown hook to run and report the failing container close, got: %s", string(output))
    }

    if false == strings.Contains(string(output), "melody: exiting with code 1") {
        t.Fatalf("expected the stderr echo after the teardown, got: %s", string(output))
    }
}

type chainedError struct {
    cause error
}

func (instance *chainedError) Error() string {
    return "chained"
}

func (instance *chainedError) Unwrap() error {
    return instance.cause
}

func TestResolveCliExitError_AnswersTheExitErrorTheChainCarries(t *testing.T) {
    exitError := exception.NewExitError(3, exception.NewError("command failed", nil, nil))

    resolved, isExitRequested := resolveCliExitError(&chainedError{cause: exitError})
    if false == isExitRequested {
        t.Fatalf("expected the wrapped exit error to be recognised")
    }

    if exitError != resolved {
        t.Fatalf("expected the wrapped exit error to be answered, got %v", resolved)
    }
}

func TestResolveCliExitError_AnswersNothingForAChainWithoutOne(t *testing.T) {
    if _, isExitRequested := resolveCliExitError(exception.NewError("command failed", nil, nil)); true == isExitRequested {
        t.Fatalf("expected no exit error")
    }
}

func TestResolveCliExitError_AnswersNothingForATypedNilLink(t *testing.T) {
    var typedNilExitError *exception.ExitError
    var cause error = typedNilExitError

    if _, isExitRequested := resolveCliExitError(&chainedError{cause: cause}); true == isExitRequested {
        t.Fatalf("expected a typed-nil link to answer no exit error")
    }
}

func TestResolveExitLogger_AnswersTheEmergencyLoggerForATypedNilKernel(t *testing.T) {
    applicationInstance := &Application{
        kernel:       (*testKernel)(nil),
        runtimeFlags: NewRuntimeFlags(config.ModeCli),
    }

    if nil == applicationInstance.resolveExitLogger() {
        t.Fatalf("expected a logger for a typed-nil kernel")
    }
}

func TestResolveExitLogger_FallsBackToTheConfiguredDestinationWhenTheContainerCannotAnswer(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    logger := applicationInstance.resolveExitLogger()

    if logging.EmergencyLogger() == logger {
        t.Fatalf("expected the configured-destination fallback, not the emergency logger")
    }

    marker := "exit fallback probe " + t.Name()
    logger.Emergency(marker, nil)

    logPath := resolveRuntimePath(
        applicationInstance.configuration.Kernel().ProjectDir(),
        applicationInstance.configuration.Kernel().LogPath(),
    )

    content, readErr := os.ReadFile(logPath)
    if nil != readErr {
        t.Fatalf("expected the configured log destination to exist, got %v", readErr)
    }

    if false == strings.Contains(string(content), marker) {
        t.Fatalf("expected the record on the configured destination %s", logPath)
    }
}

func TestResolveExitLogger_AnswersTheEmergencyLoggerWhenNothingIsConfigured(t *testing.T) {
    applicationInstance := &Application{}

    logger := applicationInstance.resolveExitLogger()

    if logging.EmergencyLogger() != logger {
        t.Fatalf("expected the emergency logger for an application holding no configuration, got %T", logger)
    }
}

func TestBoot_RegistersTheKernelListenersInEveryProcessShape(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    inspector, ok := applicationInstance.kernel.EventDispatcher().(eventcontract.EventDispatcherInspector)
    if false == ok {
        t.Fatalf("expected the dispatcher to support inspection")
    }

    listenerCountByEvent := map[string]int{}
    for _, registeredEvent := range inspector.RegisteredEvents() {
        listenerCountByEvent[registeredEvent.EventName] = len(registeredEvent.Listeners)
    }

    if 0 == listenerCountByEvent[kernelcontract.EventKernelException] {
        t.Fatal("expected the exception listener after boot")
    }

    if 0 == listenerCountByEvent[kernelcontract.EventKernelResponse] {
        t.Fatal("expected the response normalizer after boot")
    }

    if 0 == listenerCountByEvent[kernelcontract.EventKernelTerminate] {
        t.Fatal("expected the access-log listener after boot")
    }
}

func TestCloseAndExitOnFailure_AnAbandonedTeardownExitsNonZero(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    stepRan := false
    shieldedCloseStep = func(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
        stepRan = true

        return false
    }

    exitCode := 0
    applicationExit = func(code int) {
        exitCode = code
    }

    instance := &Application{}
    instance.closeAndExitOnFailure()

    if false == stepRan {
        t.Fatalf("expected the teardown to run through the shield")
    }

    if 1 != exitCode {
        t.Fatalf("expected an abandoned teardown to exit non-zero, got %d", exitCode)
    }
}

func TestCloseAndExitOnFailure_ACompletedTeardownExitsZero(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    shieldedCloseStep = func(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
        step(context.Background())

        return true
    }

    exited := false
    applicationExit = func(code int) {
        exited = true
    }

    instance := &Application{}
    instance.closeAndExitOnFailure()

    if true == exited {
        t.Fatalf("expected a clean teardown to leave the exit code alone")
    }
}

func TestCloseAndExitOnFailure_HandsTheShieldTheDeclaredTeardownBudget(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    receivedBudget := time.Duration(0)
    shieldedCloseStep = func(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
        receivedBudget = budget

        return true
    }

    applicationExit = func(code int) {}

    instance := newTeardownTimeoutTestApplication(t, "42s")
    instance.closeAndExitOnFailure()

    if 42*time.Second != receivedBudget {
        t.Fatalf("expected the declared teardown budget to reach the shield, got %s", receivedBudget)
    }
}

func TestCloseAndExitOnFailure_WithoutAConfigurationTheShieldGetsTheDefaultBudget(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    receivedBudget := time.Duration(0)
    shieldedCloseStep = func(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
        receivedBudget = budget

        return true
    }

    applicationExit = func(code int) {}

    instance := &Application{}
    instance.closeAndExitOnFailure()

    if config.DefaultTeardownTimeout != receivedBudget {
        t.Fatalf("expected the default teardown budget without a configuration, got %s", receivedBudget)
    }
}

func newTeardownTimeoutTestApplication(t *testing.T, teardownTimeout string) *Application {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(
        &mapEnvironmentSource{
            values: map[string]string{
                config.TeardownTimeoutKey: teardownTimeout,
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

    return &Application{
        ctx:           context.Background(),
        configuration: configuration,
        runtimeFlags:  NewRuntimeFlags(config.ModeHttp),
    }
}

func TestCloseAndExitOnFailure_AnOverrunAloneExitsZero(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    shieldedCloseStep = func(budget time.Duration, stepName string, step func(stepContext context.Context)) bool {
        stepContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
        defer cancel()

        step(stepContext)

        return true
    }

    exitedWith := -1
    applicationExit = func(code int) {
        exitedWith = code
    }

    kernelInstance := newTestKernel()
    applicationInstance := newScopedServiceApplication(kernelInstance)

    serviceContainer := kernelInstance.ServiceContainer()

    if registerErr := serviceContainer.Register(
        "app.eater",
        func(_ containercontract.Resolver) (*overrunSleeper, error) { return &overrunSleeper{sleep: 80 * time.Millisecond}, nil },
    ); nil != registerErr {
        t.Fatalf("unexpected register error: %v", registerErr)
    }

    if _, getErr := serviceContainer.Get("app.eater"); nil != getErr {
        t.Fatalf("unexpected get error: %v", getErr)
    }

    written := captureEmergencyLogger(t, func() {
        applicationInstance.closeAndExitOnFailure()
    })

    if -1 != exitedWith {
        t.Fatalf("expected an overrun alone to leave the exit code alone, got %d", exitedWith)
    }

    if false == strings.Contains(written, "overran its deadline") {
        t.Fatalf("expected the overrun in the journal, got %q", written)
    }
}
