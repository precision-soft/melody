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

/* DispatchCommand parses and runs a complete command line whose first argument is the command name. It adds no banner, scope close or exit handling. The writer receives command and parser output; nil discards it. Command errors retain their identity and typed nil becomes nil. A context inherited from another CLI action can expose that action’s flags; use the process context to parse only this command’s flags. */
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

        ExitErrHandler: inertExitHandler,
    }

    return engineCommand.Run(ctx, arguments)
}
