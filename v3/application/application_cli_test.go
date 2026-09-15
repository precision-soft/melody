package application

import (
    "bytes"
    "context"
    "errors"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    urfavecli "github.com/urfave/cli/v3"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/debug"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type exitCodedProbeApplicationCommand struct{}

func (instance *exitCodedProbeApplicationCommand) Name() string {
    return "probe:exit"
}

func (instance *exitCodedProbeApplicationCommand) Description() string {
    return "returns an exit-coded error"
}

func (instance *exitCodedProbeApplicationCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{}
}

func (instance *exitCodedProbeApplicationCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
}

func TestRunCli_InstallsTheExitErrHandlerOnTheRootCommand(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterCliCommand(&exitCodedProbeApplicationCommand{})

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:exit"}
    defer func() { os.Args = originalArguments }()

    runErr := applicationInstance.runCli()

    if -1 != exitedWith {
        t.Fatalf("expected runCli to keep the cli library from exiting the process, got an exit with code %d", exitedWith)
    }

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected the exit-coded error to travel back out of runCli, got %v", runErr)
    }

    if 7 != exitError.ExitCode() {
        t.Fatalf("expected the exit code to survive, got %d", exitError.ExitCode())
    }
}

func TestRunCli_DoesNotWarnAboutTheUnboundedDefaultCacheBackend(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, logger)

    if false == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected the cli application to carry the same unbounded default backend the http path warns about")
    }

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    runErr := applicationInstance.runCli()
    if nil != runErr {
        t.Fatalf("unexpected run cli error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedCacheWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no cache warning on a cli run, got %v", warnings)
    }
}

func TestRegisterCliCommand_JudgesTheNameTrimmed(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterCliCommand(&namedTestCommand{name: "app:padded"})
    applicationInstance.RegisterCliCommand(&namedTestCommand{name: "app:padded "})

    if 1 != len(applicationInstance.bootCollisions) {
        t.Fatalf("expected the padded duplicate to be recorded as a collision, got %d", len(applicationInstance.bootCollisions))
    }

    reversedApplication := newCollisionTestApplication(t)

    reversedApplication.RegisterCliCommand(&namedTestCommand{name: "app:reversed "})
    reversedApplication.RegisterCliCommand(&namedTestCommand{name: "app:reversed"})

    if 1 != len(reversedApplication.bootCollisions) {
        t.Fatalf("expected the reversed padded duplicate to be recorded as a collision, got %d", len(reversedApplication.bootCollisions))
    }

    testhelper.AssertPanicsWithError(t, func() {
        applicationInstance.RegisterCliCommand(&namedTestCommand{name: "   "})
    }, "cli command name may not be empty")
}

func TestRunCli_DispatchesACommandWhoseNameCarriesPadding(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probe := &servingProbeApplicationCommand{}
    paddedProbe := &paddedNameProbeCommand{inner: probe}
    applicationInstance.RegisterCliCommand(paddedProbe)

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:serving"}
    defer func() { os.Args = originalArguments }()

    runErr := applicationInstance.runCli()
    if nil != runErr {
        t.Fatalf("expected the padded command to be dispatchable, got: %v", runErr)
    }

    if false == probe.ran {
        t.Fatalf("expected the padded command to have run")
    }
}

type paddedNameProbeCommand struct {
    inner clicontract.Command
}

func (instance *paddedNameProbeCommand) Name() string {
    return " " + instance.inner.Name() + " "
}

func (instance *paddedNameProbeCommand) Description() string {
    return instance.inner.Description()
}

func (instance *paddedNameProbeCommand) Flags() []clicontract.Flag {
    return instance.inner.Flags()
}

func (instance *paddedNameProbeCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    return instance.inner.Run(runtimeInstance, commandContext)
}

func TestSuggestCliCommand_ReturnsTheRefusalUnmarked(t *testing.T) {
    suggestErr := suggestCliCommand(
        []string{"app", "product"},
        []commandSuggestion{
            {Name: "example:product", Description: "lists the products"},
        },
        clock.NewSystemClock(),
    )
    if nil == suggestErr {
        t.Fatalf("expected the suggestion refusal")
    }

    var exitError *exception.ExitError
    if false == errors.As(suggestErr, &exitError) {
        t.Fatalf("expected an ExitError, got %v", suggestErr)
    }
    if 2 != exitError.ExitCode() {
        t.Fatalf("expected exit code 2, got %d", exitError.ExitCode())
    }
    if true == exitError.ErrorValue().AlreadyLogged() {
        t.Fatalf("expected the refusal to travel unmarked so the exit path logs it")
    }
}

func TestSuggestCliCommand_ReturnsTheZeroMatchRefusalUnmarked(t *testing.T) {
    suggestErr := suggestCliCommand(
        []string{"app", "nosuchthing"},
        []commandSuggestion{
            {Name: "example:product", Description: "lists the products"},
        },
        clock.NewSystemClock(),
    )
    if nil == suggestErr {
        t.Fatalf("expected the refusal")
    }

    var exitError *exception.ExitError
    if false == errors.As(suggestErr, &exitError) {
        t.Fatalf("expected an ExitError, got %v", suggestErr)
    }
    if true == exitError.ErrorValue().AlreadyLogged() {
        t.Fatalf("expected the refusal to travel unmarked")
    }
}

func TestPrintCliCommandNotFoundHeader_EscapesTheArgvDerivedName(t *testing.T) {
    buffer := &bytes.Buffer{}

    printCliCommandNotFoundHeader(buffer, "bad\rname\x1b[2K", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))

    written := buffer.String()
    if true == strings.Contains(written, "\r") || true == strings.Contains(written, "\x1b") {
        t.Fatalf("expected no raw control byte in the header, got %q", written)
    }
    if false == strings.Contains(written, `bad\rname\x1b[2K`) {
        t.Fatalf("expected the escaped spelling kept in place, got %q", written)
    }
    if false == strings.Contains(written, "[command not found]") {
        t.Fatalf("expected the header verdict kept, got %q", written)
    }
}

func TestNormalizeCliVerbosityArguments_RewritesOnlyTheRepeatedVerbosityFlag(t *testing.T) {
    cases := []struct {
        name      string
        arguments []string
        expected  []string
    }{
        {
            name:      "no arguments at all",
            arguments: []string{},
            expected:  []string{},
        },
        {
            name:      "a single -v becomes level one",
            arguments: []string{"app", "work", "-v"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=1"},
        },
        {
            name:      "the repeated letter becomes the level",
            arguments: []string{"app", "work", "-vvv"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=3"},
        },
        {
            name:      "a long flag is left to the library",
            arguments: []string{"app", "work", "--verbose"},
            expected:  []string{"app", "work", "--verbose"},
        },
        {
            name:      "a short flag that is not the verbosity one passes through",
            arguments: []string{"app", "work", "-x"},
            expected:  []string{"app", "work", "-x"},
        },
        {
            name:      "a short flag that merely starts with v passes through",
            arguments: []string{"app", "work", "-vx"},
            expected:  []string{"app", "work", "-vx"},
        },
        {
            name:      "a lone dash is not a flag",
            arguments: []string{"app", "work", "-"},
            expected:  []string{"app", "work", "-"},
        },
        {
            name:      "everything after the terminator belongs to the command",
            arguments: []string{"app", "work", "-vv", "--", "-vv", "-v"},
            expected:  []string{"app", "work", "--" + output.FlagNameVerbosity + "=2", "--", "-vv", "-v"},
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            normalized := normalizeCliVerbosityArguments(testCase.arguments)

            if len(testCase.expected) != len(normalized) {
                t.Fatalf("expected %d arguments, got %d: %#v", len(testCase.expected), len(normalized), normalized)
            }

            for index := range testCase.expected {
                if testCase.expected[index] != normalized[index] {
                    t.Fatalf("expected argument %d to be %q, got %q", index, testCase.expected[index], normalized[index])
                }
            }
        })
    }
}

type typedNilProbeCommand struct{}

func (instance *typedNilProbeCommand) Name() string {
    return "typed:nil:probe"
}

func (instance *typedNilProbeCommand) Description() string {
    return "typed nil probe"
}

func (instance *typedNilProbeCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *typedNilProbeCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    return nil
}

func TestApplicationRegisterCliCommand_RefusesATypedNilCommand(t *testing.T) {
    applicationInstance := &Application{}

    testhelper.AssertPanicsWithError(
        t,
        func() {
            applicationInstance.RegisterCliCommand((*typedNilProbeCommand)(nil))
        },
        "cli command may not be nil",
    )
}

type processContextProbeCliCommand struct {
    seenProcessId   string
    seenStartedAt   time.Time
    accessorAnswers bool
}

func (instance *processContextProbeCliCommand) Name() string {
    return "probe:process-context"
}

func (instance *processContextProbeCliCommand) Description() string {
    return "captures the process context the run scope carries"
}

func (instance *processContextProbeCliCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *processContextProbeCliCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    processContext := ProcessContextMustFromResolver(runtimeInstance.Scope())

    instance.seenProcessId = processContext.ProcessId()
    instance.seenStartedAt = processContext.StartedAt()
    instance.accessorAnswers = nil != ProcessContextFromResolver(runtimeInstance.Scope())

    return nil
}

func TestRunCli_InstallsTheProcessContextIntoTheRunScope(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    probeCommand := &processContextProbeCliCommand{}
    applicationInstance.RegisterCliCommand(probeCommand)

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:process-context"}
    defer func() { os.Args = originalArguments }()

    if runErr := applicationInstance.runCli(); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if "" == probeCommand.seenProcessId {
        t.Fatalf("expected the run scope to carry a process context with a generated id")
    }

    if true == probeCommand.seenStartedAt.IsZero() {
        t.Fatalf("expected the process context to carry the run's start moment")
    }

    if false == probeCommand.accessorAnswers {
        t.Fatalf("expected the tolerant accessor to answer the installed context")
    }
}

type contextCapturingCliLogger struct {
    mutex    sync.Mutex
    contexts []map[string]any
    messages []string
}

func (instance *contextCapturingCliLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
    instance.contexts = append(instance.contexts, context)
}

func (instance *contextCapturingCliLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *contextCapturingCliLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *contextCapturingCliLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *contextCapturingCliLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *contextCapturingCliLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *contextCapturingCliLogger) contextOfMessage(message string) (map[string]any, bool) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for index, recordedMessage := range instance.messages {
        if message == recordedMessage {
            return instance.contexts[index], true
        }
    }

    return nil, false
}

type providedKeyProbeCliCommand struct{}

func (instance *providedKeyProbeCliCommand) Name() string {
    return "probe:provided-key"
}

func (instance *providedKeyProbeCliCommand) Description() string {
    return "logs its own value under the correlation key"
}

func (instance *providedKeyProbeCliCommand) Flags() []clicontract.Flag {
    return nil
}

func (instance *providedKeyProbeCliCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    logger := logging.LoggerMustFromRuntime(runtimeInstance)

    logger.Info("probe record with a caller process id", loggingcontract.Context{"processId": "caller-owned"})

    return nil
}

func TestRunCli_TheRunLoggerPreservesACallerProcessIdUnderProvided(t *testing.T) {
    baseLogger := &contextCapturingCliLogger{}

    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return baseLogger, nil
        },
    )

    applicationInstance.RegisterCliCommand(&providedKeyProbeCliCommand{})

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:provided-key"}
    defer func() { os.Args = originalArguments }()

    if runErr := applicationInstance.runCli(); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    recordContext, recorded := baseLogger.contextOfMessage("probe record with a caller process id")
    if false == recorded {
        t.Fatalf("expected the probe record to reach the base logger")
    }

    generatedId, hasGeneratedId := recordContext["processId"].(string)
    if false == hasGeneratedId || "" == generatedId || "caller-owned" == generatedId {
        t.Fatalf("expected the generated id to win the correlation key, got %v", recordContext["processId"])
    }

    if "caller-owned" != recordContext["processIdProvided"] {
        t.Fatalf("expected the caller's value verbatim under the provided key, got %v", recordContext["processIdProvided"])
    }

    if _, hasClaim := recordContext["processIdClaimed"]; true == hasClaim {
        t.Fatalf("expected the console path to write no claimed key, got %v", recordContext["processIdClaimed"])
    }
}

func TestBootCli_LeavesTheDebugVersionApplicationSlotEmpty(t *testing.T) {
    applicationInstance := NewApplication(
        context.Background(),
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.Boot()

    versionCommand := (*debug.VersionCommand)(nil)
    for _, command := range applicationInstance.cliCommands {
        if candidate, isVersion := command.(*debug.VersionCommand); true == isVersion {
            versionCommand = candidate
        }
    }

    if nil == versionCommand {
        t.Fatal("expected bootCli to register debug:version in the dev environment")
    }

    if "" != versionCommand.ApplicationVersion {
        t.Fatalf("expected the wiring to leave the application slot empty, got %q", versionCommand.ApplicationVersion)
    }
}
