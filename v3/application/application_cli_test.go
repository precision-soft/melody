package application

import (
    "context"
    "errors"
    "os"
    "testing"

    urfavecli "github.com/urfave/cli/v3"

    "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/config"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* @info core command registration */

func TestRegisterCoreCliCommandIfAbsent_RegistersWhenNamePartitionIsFree(t *testing.T) {
    instance := &Application{}

    instance.registerCoreCliCommandIfAbsent(http.NewRouteManifestCommand())

    if 1 != len(instance.cliCommands) {
        t.Fatalf("expected the core command to be registered, got %d", len(instance.cliCommands))
    }
}

func TestRegisterCoreCliCommandIfAbsent_SkipsWhenAppAlreadyRegisteredSameName(t *testing.T) {
    instance := &Application{}

    appCommand := http.NewRouteManifestCommand()
    instance.cliCommands = []clicontract.Command{appCommand}

    /* @important a framework-registered core command must not panic on a duplicate name when the application still wires the same command by hand; it is skipped so the upgrade stays backward-compatible */
    instance.registerCoreCliCommandIfAbsent(http.NewRouteManifestCommand())

    if 1 != len(instance.cliCommands) {
        t.Fatalf("expected the duplicate core command to be skipped, got %d commands", len(instance.cliCommands))
    }

    if appCommand != instance.cliCommands[0] {
        t.Fatalf("expected the application's own command instance to be kept")
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

    runErr := applicationInstance.runCli()
    if nil != runErr {
        t.Fatalf("unexpected run cli error: %v", runErr)
    }

    warnings := logger.warningsContaining(unboundedCacheWarningFragment)
    if 0 != len(warnings) {
        t.Fatalf("expected no cache warning on a cli run, got %v", warnings)
    }
}

/* the suggestion refusal travels unmarked so the exit path writes it to the application log: the rendered table lives only on stderr, and a run refused here used to be invisible to anything reading the log file */
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

/* the zero-match refusal is the same contract: unmarked, exit-coded, the full command list rendered on stderr */
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
