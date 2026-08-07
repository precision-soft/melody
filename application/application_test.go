package application

import (
    "context"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

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

/* Run must tell the configuration the wiring phase is over before it dispatches anything, or a late Resolve silently rewrites parameters under services that already read them. The config package tests what MarkServing does; nothing tested that Run calls it, so deleting the call left both ./application/... and ./config/... green. This drives the real Run in cli mode and asks the configuration from inside the command. */
func TestRun_MarksTheConfigurationServingBeforeItDispatches(t *testing.T) {
    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    defer func() { os.Args = originalArguments }()

    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probe := &servingProbeApplicationCommand{}
    applicationInstance.RegisterCliCommand(probe)

    applicationInstance.Run(context.Background())

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

/* the secret front door differs from the ordinary one only in the marking, and the marking is the whole point: it is what keeps the value, and every parameter whose template reads it, out of the rendered configuration. */
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

/* marking an existing parameter takes effect at once; a name that matches nothing is queued rather than refused, because an environment key is legitimately undefined in some environments and the boot retries the queue before the configuration resolves. */
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

/* after the boot the wiring is done and the parameters have been read: a marking arriving here would redact nothing that has not already been rendered, so it is refused loudly rather than accepted as a no-op. */
func TestMarkParameterSecret_RefusesAfterBoot(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)
    applicationInstance.booted = true

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.MarkParameterSecret("app.token")
    }, "cannot mark a parameter secret after application boot")
}

/* the retry is what makes a marking declared before the module that registers the parameter work at all, and it must run before the resolve or the marking never travels into the templates that read the secret. What still matches nothing stays queued for the warning at the end of the boot. */
func TestApplyUnappliedSecretMarks_AppliesWhatALaterRegistrationMadeReal(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.MarkParameterSecret("app.late")
    applicationInstance.MarkParameterSecret("app.never")

    if 2 != len(applicationInstance.unappliedSecretMarks) {
        t.Fatalf("expected both markings to be queued, got %#v", applicationInstance.unappliedSecretMarks)
    }

    /* the module that owns the parameter registers it after the marking was declared */
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

/* the warning at the end of the boot is what keeps a misspelled name from silently redacting nothing: every phase that can register a parameter has run by then, so a marking still matching nothing names something that does not exist. A name a late phase did make real is applied here instead, without a warning, and the queue is emptied either way. */
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

/* ProcessRole is what wiring code asks to decide whether this process registers its background runners: the explicit --role flag wins, and an unset role widens to all rather than to nothing. */
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
        configuration:        configuration,
        runtimeFlags:         NewRuntimeFlags(mode),
        kernel:               newTestKernel(),
        moduleConfigurations: make(map[string]any),
    }
}

/* every built-in parameter has a development default, so an http process whose .env artifacts contributed nothing would serve as dev with debug tooling, announced by one warning; the refusal is the same direction the empty CORS allow list took. */
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

/* the configuration registry accepts exactly one name in this major; any other name is unreadable by construction, so storing it would tell the operator a configuration is active while nothing can ever consult it. */
func TestRegisterConfiguration_RefusesANameNothingConsumes(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterConfiguration("Logging", "misspelled")
    }, "unknown configuration name")
}

/* the marker tells a re-executed test binary that it is the child that must take the fatal exit rather than the parent that watches it; its value is the project directory whose log file the parent reads back */
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
            configuration:        configuration,
            runtimeFlags:         NewRuntimeFlags(config.ModeCli),
            kernel:               newTestKernel(),
            cliCommands:          make([]clicontract.Command, 0),
            moduleConfigurations: make(map[string]any),
        }

        applicationInstance.RegisterCliCommand(&panickingProbeApplicationCommand{})

        os.Args = []string{"probe", "probe:panic"}

        applicationInstance.Run(context.Background())

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

/* chainedError carries a cause the way a wrapped run failure does, so errors.As can be walked onto a typed-nil link. */
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

/* errors.As matches *ExitError on a typed-nil link and reports success; answering it would hand Exit a nil it refuses and discard the run's own error */
func TestResolveCliExitError_AnswersNothingForATypedNilLink(t *testing.T) {
    var typedNilExitError *exception.ExitError
    var cause error = typedNilExitError

    if _, isExitRequested := resolveCliExitError(&chainedError{cause: cause}); true == isExitRequested {
        t.Fatalf("expected a typed-nil link to answer no exit error")
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
