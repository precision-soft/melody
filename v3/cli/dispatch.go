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

/* DispatchCommand parses one command line against a command's own flags and runs it, with none of what the registration path adds around a command: no banner, no scope close, no exit handling. It is for a caller that dispatches a melody command inside a process it owns and keeps owning — the cron runner drives one per due entry, each on its own goroutine into its own captured output, where the banners of concurrent runs would interleave on one stream and a scope closed here would end a scope the caller is still using.

   arguments is the whole command line, arguments[0] being the name the command was invoked under, exactly as Root.Run takes it. writer receives both what the command writes and what the parser writes about a refused flag; a nil writer discards. The error the command returned is answered as it is, read through the interface so a command declaring a concrete error type does not hand back a typed nil that reads as a failure.

   One engine behaviour travels with ctx and is worth knowing: a command dispatched with a context that descends from ANOTHER command's action inherits that command's flag set, so an argument naming a flag only the outer command declares is accepted rather than refused. Dispatch from the process's own context — which is what every caller inside melody does — and the command is parsed against its own flags alone. */
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

    if nil == runtimeInstance {
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

    if nil == writer {
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
        /* the same reason NewRoot has one: left at its default the engine ends the process itself on any error the command returns, and here that would take down a scheduler over one failed job */
        ExitErrHandler: inertExitHandler,
    }

    return engineCommand.Run(ctx, arguments)
}
