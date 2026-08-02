package application

import (
    "context"
    "errors"
    "os"
    "testing"

    urfavecli "github.com/urfave/cli/v3"

    "github.com/precision-soft/melody/cli"
    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    "github.com/precision-soft/melody/config"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func exitCodedProbeCommand() *clicontract.CommandContext {
    return &clicontract.CommandContext{
        Name: "exit-coded",
        Action: func(actionContext context.Context, actionCommandContext *clicontract.CommandContext) error {
            return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
        },
    }
}

/* @info runCli installs a no-op ExitErrHandler on the root command, and the whole cli shutdown path depends on what that buys: the library must hand the exit-coded error back instead of taking the process down from inside Run, or the application's deferred Close and its structured error log never run. This locks that library contract, so a cli upgrade that changes it fails here rather than silently skipping teardown in production. */
func TestRunCli_ExitCodedErrorLeavesRunInsteadOfExitingInside(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    rootCli := cli.NewCommandContext("probe", "probe")
    rootCli.ExitErrHandler = func(handlerContext context.Context, handlerCommandContext *clicontract.CommandContext, handlerErr error) {
    }
    rootCli.Commands = append(rootCli.Commands, exitCodedProbeCommand())

    runErr := rootCli.Run(context.Background(), []string{"probe", "exit-coded"})

    if -1 != exitedWith {
        t.Fatalf("expected the cli library not to exit the process from inside Run, got an exit with code %d", exitedWith)
    }

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected the exit-coded error to travel back out of Run, got %v", runErr)
    }

    if 7 != exitError.ExitCode() {
        t.Fatalf("expected the exit code to survive the trip out of Run, got %d", exitError.ExitCode())
    }
}

/* @info the control: with no handler the library resolves the exit itself from inside Run, which is exactly the path that skipped the application's teardown */
func TestRunCli_WithoutExitErrHandlerTheLibraryExitsFromInsideRun(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    rootCli := cli.NewCommandContext("probe", "probe")
    rootCli.Commands = append(rootCli.Commands, exitCodedProbeCommand())

    _ = rootCli.Run(context.Background(), []string{"probe", "exit-coded"})

    if 7 != exitedWith {
        t.Fatalf("expected the cli library to take the exit itself without a handler, got %d", exitedWith)
    }
}

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
    commandContext *clicontract.CommandContext,
) error {
    return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
}

/* @info the two tests above lock the library contract, but both build their own root command, so they would still pass if runCli stopped installing the handler; this one drives the real runCli */
func TestRunCli_InstallsTheExitErrHandlerOnTheRootCommand(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    applicationInstance := NewApplication(
        testhelper.NewEmbeddedEnvFs(),
        testhelper.NewEmbeddedStaticFs(),
    )

    applicationInstance.RegisterCliCommand(&exitCodedProbeApplicationCommand{})

    applicationInstance.Boot()

    originalArguments := os.Args
    os.Args = []string{"probe", "probe:exit"}
    defer func() { os.Args = originalArguments }()

    runErr := applicationInstance.runCli(context.Background())

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

/* @info The unbounded default cache is a hazard only in a process that stays up: a command builds its map, runs and takes it away with it. A cli invocation must therefore see no cache warning at all, or every command a scheduler runs prints advice its lifetime makes meaningless. This drives the real runCli against the same wiring the http test warns from. */
func TestRunCli_DoesNotWarnAboutTheUnboundedDefaultCacheBackend(t *testing.T) {
    logger := &warningRecordingLogger{}

    applicationInstance := newCacheWarningTestApplication(t, config.ModeCli, logger)

    if false == applicationInstance.unboundedDefaultCacheBackend {
        t.Fatalf("expected the cli application to carry the same unbounded default backend the http path warns about")
    }

    originalArguments := os.Args
    os.Args = []string{"melody"}
    defer func() { os.Args = originalArguments }()

    runErr := applicationInstance.runCli(context.Background())
    if nil != runErr {
        t.Fatalf("unexpected run cli error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedCacheWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no cache warning on a cli run, got %v", warnings)
    }
}

/* @info three normalization points must agree on a command's name — the boot registration, the cli library's trimmed registration, and the suggestion gate's trimmed input. A padded name judged raw at boot registered under a spelling no argv can produce: the suggestion table blocked every invocation of a command that exists. */
func TestRegisterCliCommand_JudgesTheNameTrimmed(t *testing.T) {
    applicationInstance := newCollisionTestApplication(t)

    applicationInstance.RegisterCliCommand(&namedTestCommand{name: "app:padded"})
    applicationInstance.RegisterCliCommand(&namedTestCommand{name: "app:padded "})

    if 1 != len(applicationInstance.bootCollisions) {
        t.Fatalf("expected the padded duplicate to be recorded as a collision, got %d", len(applicationInstance.bootCollisions))
    }

    /* the reversed order exercises the other side of the comparison: the already-registered name is the padded one */
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

/* @info the whole path: a command whose Name carries padding must still be reachable from argv — the suggestion gate compares the trimmed input against the trimmed name and the cli library dispatches the trimmed registration. */
func TestRunCli_DispatchesACommandWhoseNameCarriesPadding(t *testing.T) {
    applicationInstance := NewApplication(
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

    runErr := applicationInstance.runCli(context.Background())
    if nil != runErr {
        t.Fatalf("expected the padded command to be dispatchable, got: %v", runErr)
    }

    if false == probe.ran {
        t.Fatalf("expected the padded command to have run")
    }
}

/* paddedNameProbeCommand wraps a command and pads its name, the shape the trimming exists for */
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
    commandContext *clicontract.CommandContext,
) error {
    return instance.inner.Run(runtimeInstance, commandContext)
}

/* @info the suggestion refusal travels unmarked so the exit path writes it to the application log: the rendered table lives only on stderr, and a run refused here used to be invisible to anything reading the log file */
func TestSuggestCliCommand_ReturnsTheRefusalUnmarked(t *testing.T) {
    /* the input is a substring of the available name, so this refusal travels through the matches-found branch, not the zero-match one */
    suggestErr := suggestCliCommand(
        []string{"app", "product"},
        []commandSuggestion{
            {Name: "example:product", Description: "lists the products"},
        },
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

/* @info the zero-match refusal is the same contract: unmarked, exit-coded, the full command list rendered on stderr */
func TestSuggestCliCommand_ReturnsTheZeroMatchRefusalUnmarked(t *testing.T) {
    suggestErr := suggestCliCommand(
        []string{"app", "nosuchthing"},
        []commandSuggestion{
            {Name: "example:product", Description: "lists the products"},
        },
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

/* @info the short verbosity flags are rewritten before the cli library parses anything, because the library has no notion of a repeated letter: -vv means level two here and nothing at all there. Everything else must pass through untouched — a lone dash, a long flag, a short flag that merely starts with v, and everything after the end-of-options terminator, which belongs to the command and not to the runtime. */
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
