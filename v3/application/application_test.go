package application

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type testKernel struct {
    configuration    configcontract.Configuration
    serviceContainer containercontract.Container
    eventDispatcher  eventcontract.EventDispatcher
    httpKernel       httpcontract.Kernel
    httpRouter       httpcontract.Router
    clock            clockcontract.Clock
}

func newTestKernel() *testKernel {
    httpRouter := http.NewRouter()

    return &testKernel{
        configuration:    nil,
        serviceContainer: container.NewContainer(),
        eventDispatcher:  event.NewEventDispatcher(clock.NewSystemClock()),
        httpKernel:       http.NewKernel(httpRouter),
        httpRouter:       httpRouter,
        clock:            clock.NewSystemClock(),
    }
}

func (instance *testKernel) Environment() string {
    return config.EnvDevelopment
}

func (instance *testKernel) DebugMode() bool {
    return true
}

func (instance *testKernel) ServiceContainer() containercontract.Container {
    return instance.serviceContainer
}

func (instance *testKernel) EventDispatcher() eventcontract.EventDispatcher {
    return instance.eventDispatcher
}

func (instance *testKernel) Config() configcontract.Configuration {
    return instance.configuration
}

func (instance *testKernel) HttpKernel() httpcontract.Kernel {
    return instance.httpKernel
}

func (instance *testKernel) HttpRouter() httpcontract.Router {
    return instance.httpRouter
}

func (instance *testKernel) Clock() clockcontract.Clock {
    return instance.clock
}

var _ kernelcontract.Kernel = (*testKernel)(nil)

func TestAssertPanics_UsesRecover(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        exception.Panic(exception.NewError("test", nil, nil))
    }, "test")
}

/* servingProbeApplicationCommand reports what the configuration answers while the command is running, which is the only moment the question means anything: the marker is set between boot and dispatch and nothing else in the process observes the transition. */
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
    commandContext *clicontract.CommandContext,
) error {
    instance.ran = true

    configuration := config.ConfigMustFromContainer(runtimeInstance.Container())
    instance.resolveErr = configuration.Resolve()

    return nil
}

/* @info Run must tell the configuration the wiring phase is over before it dispatches anything, or a late Resolve silently rewrites parameters under services that already read them. The config package tests what MarkServing does; nothing tested that Run calls it, so deleting the call left both ./application/... and ./config/... green. This drives the real Run in cli mode and asks the configuration from inside the command. */
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

/* failingCloser is a container service whose Close always fails, so the test controls which call discovers the teardown failure */
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

    /* resolving builds the instance, so the teardown has something whose Close fails */
    container.MustFromResolver[*failingCloser](applicationInstance.kernel.ServiceContainer(), "service.test.failing.closer")

    return applicationInstance
}

/* the container memoizes its close error, so a repeated Close re-receives a failure somebody else already discovered and folded into their own report; close must only report the failure it was first to see. */
func TestClose_DoesNotRereportAFailureSomebodyElseDiscovered(t *testing.T) {
    applicationInstance := newFailingCloseApplication(t)

    firstCloseErr := applicationInstance.kernel.ServiceContainer().Close()
    if nil == firstCloseErr {
        t.Fatalf("expected the discovering close to report the failure")
    }

    if closeErr := applicationInstance.close(); nil != closeErr {
        t.Fatalf("expected the repeated close not to re-report the memoized failure, got: %v", closeErr)
    }
}

func TestClose_ReportsTheFailureItDiscoveredItself(t *testing.T) {
    applicationInstance := newFailingCloseApplication(t)

    closeErr := applicationInstance.close()
    if nil == closeErr {
        t.Fatalf("expected the discovering close to report the teardown failure")
    }

    if false == strings.Contains(closeErr.Error(), "failed to close container services") {
        t.Fatalf("expected the container teardown failure, got: %v", closeErr)
    }
}

/* a teardown failure on the non-panic return of Run turns into a non-zero exit, symmetric with the cli path that folds close failures into the command result; exit 0 on a failed flush told the supervisor a clean story. */
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

/* the exit logger must refuse a container logger the teardown already closed: a closed file-backed logger silently drops every write, and preferring it loses the one record that explains the exit. */
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

/* the recover handler is the one place that must not panic: an Application assembled without NewApplication has a nil kernel, and the handler answers with the emergency logger instead of dereferencing it. */
func TestResolveExitLogger_SurvivesANilKernel(t *testing.T) {
    applicationInstance := &Application{}

    if nil == applicationInstance.resolveExitLogger() {
        t.Fatalf("expected the emergency logger for a nil kernel")
    }
}

/* the marker tells a re-executed test binary that it is the child whose Run must die on the probe command rather than the parent that watches it */
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
    commandContext *clicontract.CommandContext,
) error {
    exception.Panic(exception.NewError("the probe command exploded", nil, nil))

    return nil
}

/* the one proof that the fatal record survives the teardown ordering: the record must land in the configured file logger BEFORE Close runs, because the teardown closes that logger and a closed file logger silently drops every write. The child re-execution is required — the handler ends in os.Exit — and the mutant that restores the old defer order (teardown first) leaves the log file without the record. */
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

/* a factory handing back a typed nil is refused by the container with an error, and the resolver answers with the emergency logger — the pin covers the whole path; the resolver's own typed-nil clause stays as latent defense for a resolution path without the container's refusal */
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

/* the exit handler now runs Close as its before-exit hook, and a boot that died before the kernel was assembled reaches it with a nil kernel: the close must be the no-op it means, not a dereference */
func TestClose_SurvivesANilKernel(t *testing.T) {
    applicationInstance := &Application{}

    applicationInstance.Close()
}

/* the marker tells a re-executed test binary that it is the child whose Boot must die and take the teardown hook with it rather than the parent that watches */
const bootPanicTeardownProbeMarker = "MELODY_TEST_BOOT_PANIC_TEARDOWN_PROBE"

/* the proof that a boot panic tears the container down before the exit: the child's container holds a built service whose Close fails, so the teardown leaves a visible trace — the emergency record naming the failed container close — that the old path, which took os.Exit with the container never closed, could not produce. The boot dies on a command-name collision, which panics inside Boot under Boot's own handler. */
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

/* resolveExitLogger is evaluated as an argument, so it runs before the exit handler's own per-step shield begins: a nil receiver here would replace the panic being reported with a bare traceback that runs neither the teardown nor os.Exit */
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

    /* deliberately unbooted: the logger service does not exist yet, which is the window every boot failure dies in */
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

/*
TestCloseAndExitOnFailure_AnAbandonedTeardownExitsNonZero pins the clean
shutdown against the shield the panic path has had since the exit-step budget
was installed. The teardown loop is strictly sequential with no budget of its
own, so one Close that never returns — a pooled connection draining to a peer
that is gone — parked every service behind it and the process with them, on the
HEALTHY path, while the panicking one had ten seconds and an escape. A teardown
that had to be abandoned is not a clean shutdown and does not report one.
*/
func TestCloseAndExitOnFailure_AnAbandonedTeardownExitsNonZero(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    stepRan := false
    shieldedCloseStep = func(stepName string, step func()) bool {
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

/* a teardown that finished cleanly still exits zero: the shield must not turn every shutdown into a failure */
func TestCloseAndExitOnFailure_ACompletedTeardownExitsZero(t *testing.T) {
    originalStep := shieldedCloseStep
    originalExit := applicationExit
    defer func() {
        shieldedCloseStep = originalStep
        applicationExit = originalExit
    }()

    shieldedCloseStep = func(stepName string, step func()) bool {
        step()

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
