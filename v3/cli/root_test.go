package cli

import (
    "bytes"
    "context"
    "errors"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

func TestNewRoot_SetsNameAndUsage(t *testing.T) {
    rootCommand := NewRoot("app", "desc")

    if nil == rootCommand {
        t.Fatalf("expected a root command")
    }
    if "app" != rootCommand.command.Name {
        t.Fatalf("expected name %q, got %q", "app", rootCommand.command.Name)
    }
    if "desc" != rootCommand.command.Usage {
        t.Fatalf("expected usage %q, got %q", "desc", rootCommand.command.Usage)
    }
}

func TestRoot_CommandNamesAnswersTheRegistrationOrder(t *testing.T) {
    runtimeInstance := newTestRuntime()
    rootCommand := NewRoot("app", "desc")

    for _, commandName := range []string{"second", "first"} {
        Register(
            rootCommand,
            &testCommand{
                nameValue:        commandName,
                descriptionValue: commandName,
                flagsValue:       nil,
                runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
                    return nil
                },
            },
            runtimeInstance,
        )
    }

    names := rootCommand.CommandNames()

    if 2 != len(names) || "second" != names[0] || "first" != names[1] {
        t.Fatalf("expected the registration order, got %v", names)
    }
}

func TestRoot_SetWriterReachesACommandRegisteredBeforeIt(t *testing.T) {
    written := runProbeCommandThroughRoot(t, false)

    if false == bytes.Contains(written, []byte("probe wrote this")) {
        t.Fatalf("expected the command output on the tree's writer, got %q", written)
    }
}

func TestRoot_SetWriterReachesACommandRegisteredAfterIt(t *testing.T) {
    written := runProbeCommandThroughRoot(t, true)

    if false == bytes.Contains(written, []byte("probe wrote this")) {
        t.Fatalf("expected the command output on the tree's writer, got %q", written)
    }
}

func runProbeCommandThroughRoot(t *testing.T, setWriterFirst bool) []byte {
    t.Helper()

    runtimeInstance := newTestRuntime()
    rootCommand := NewRoot("app", "desc")
    buffer := &bytes.Buffer{}

    command := &testCommand{
        nameValue:        "probe",
        descriptionValue: "probe",
        flagsValue:       nil,
        runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
            _, _ = commandContext.Writer().Write([]byte("probe wrote this"))

            return nil
        },
    }

    if true == setWriterFirst {
        rootCommand.SetWriter(buffer)
        rootCommand.SetErrorWriter(buffer)
        Register(rootCommand, command, runtimeInstance)
    } else {
        Register(rootCommand, command, runtimeInstance)
        rootCommand.SetWriter(buffer)
        rootCommand.SetErrorWriter(buffer)
    }

    runErr := rootCommand.Run(context.Background(), []string{"app", "probe"})
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    return buffer.Bytes()
}

func TestNewRoot_ExitCodedErrorLeavesRunInsteadOfExitingInside(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    runtimeInstance := newTestRuntime()
    rootCommand := NewRoot("probe", "probe")
    rootCommand.SetWriter(&bytes.Buffer{})
    rootCommand.SetErrorWriter(&bytes.Buffer{})

    Register(
        rootCommand,
        &testCommand{
            nameValue:        "exit-coded",
            descriptionValue: "exit-coded",
            flagsValue:       nil,
            runCallback: func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
                return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
            },
        },
        runtimeInstance,
    )

    runErr := rootCommand.Run(context.Background(), []string{"probe", "exit-coded"})

    if -1 != exitedWith {
        t.Fatalf("expected the engine not to exit the process from inside Run, got an exit with code %d", exitedWith)
    }

    var exitError *exception.ExitError
    if false == errors.As(runErr, &exitError) {
        t.Fatalf("expected the exit-coded error to travel back out of Run, got %v", runErr)
    }

    if 7 != exitError.ExitCode() {
        t.Fatalf("expected the exit code to survive the trip out of Run, got %d", exitError.ExitCode())
    }
}

func TestNewRoot_WithoutTheInertExitHandlerTheEngineExitsFromInsideRun(t *testing.T) {
    exitedWith := -1
    originalExiter := urfavecli.OsExiter
    urfavecli.OsExiter = func(code int) { exitedWith = code }
    defer func() { urfavecli.OsExiter = originalExiter }()

    unguardedRoot := &urfavecli.Command{
        Name:      "probe",
        Usage:     "probe",
        Writer:    &bytes.Buffer{},
        ErrWriter: &bytes.Buffer{},
        Commands: []*urfavecli.Command{
            {
                Name: "exit-coded",
                Action: func(actionContext context.Context, actionCommand *urfavecli.Command) error {
                    return exception.NewExitError(7, exception.NewError("command asked for an exit code", nil, nil))
                },
            },
        },
    }

    _ = unguardedRoot.Run(context.Background(), []string{"probe", "exit-coded"})

    if 7 != exitedWith {
        t.Fatalf("expected the engine to take the exit itself without the handler, got %d", exitedWith)
    }
}
