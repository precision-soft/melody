package application

import (
    "context"
    "errors"
    "os"
    "testing"

    urfavecli "github.com/urfave/cli/v3"

    "github.com/precision-soft/melody/v3/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
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
