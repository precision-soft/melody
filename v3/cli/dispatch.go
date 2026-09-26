package cli

import (
    "context"
    "io"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    urfavecli "github.com/urfave/cli/v3"
)

/* DispatchCommand parses one command line against a command's own flags and runs it, with no banner, no scope close and no exit handling, for a caller that dispatches inside a process it keeps owning, as the cron runner does. arguments[0] is the name the command was invoked under, as for Root.Run; writer receives the command's output and the parser's refusals, and nil discards. The command's error is answered as it is, read through the interface. A ctx that descends from another command's action inherits that command's flag set; dispatch from the process's own context. */
func DispatchCommand(
    ctx context.Context,
    command clicontract.Command,
    runtimeInstance runtimecontract.Runtime,
    arguments []string,
    writer io.Writer,
) error {
    if true == internal.IsNilInterface(command) {
        exception.Panic(
            exception.NewError("cli command may not be nil", nil, nil),
        )
    }

    if true == internal.IsNilInterface(runtimeInstance) {
        exception.Panic(
            exception.NewError("runtime instance may not be nil in cli dispatch", nil, nil),
        )
    }

    if 0 == len(arguments) {
        exception.Panic(
            exception.NewError(
                "cli dispatch arguments may not be empty",
                map[string]any{
                    "commandName": command.Name(),
                },
                nil,
            ),
        )
    }

    if true == internal.IsNilInterface(writer) {
        writer = io.Discard
    }

    engineCommand := &urfavecli.Command{
        Name:      arguments[0],
        Usage:     command.Description(),
        Flags:     newEngineFlags(command.Flags()),
        Writer:    writer,
        ErrWriter: writer,
        Action: func(actionContext context.Context, actionCommand *urfavecli.Command) error {
            return normalizeCliError(command.Run(runtimeInstance, newEngineContext(actionCommand)))
        },
        /* as in NewRoot: the engine's default would end the process on any error the command returns */
        ExitErrHandler: inertExitHandler,
    }

    return engineCommand.Run(ctx, arguments)
}
