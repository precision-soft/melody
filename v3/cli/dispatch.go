package cli

import (
    "context"
    "io"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* DispatchCommand parses one command line against a command's own flags and runs it, with no banner, no scope close and no exit handling, for a caller that dispatches inside a process it keeps owning, as the cron runner does. arguments[0] is the name the command was invoked under, as for Root.Run; writer receives the command's output and the parser's refusals, and nil discards. The command's error is answered as it is, read through the interface. A ctx that descends from another command's action inherits that command's flag set; dispatch from the process's own context. The command's flags are refused when one is nil, repeats a spelling, carries an empty alias or, under ValidateDefaults, declares a default its own validator refuses, and the engine parses into fresh instances of them, so a command answering the same instances on every Flags() call is dispatched as any other, twice at once included; a Destination the flags declare is the one pointer every dispatch writes the parsed value into, so two dispatches in flight share it. */
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

    flags := prepareCommandFlags(command.Flags())

    engineCommand := &clicontract.CommandContext{
        Name:      arguments[0],
        Usage:     command.Description(),
        Flags:     flags,
        Writer:    writer,
        ErrWriter: writer,
        Action: func(actionContext context.Context, actionCommand *clicontract.CommandContext) error {
            return normalizeCliError(command.Run(runtimeInstance, actionCommand))
        },
        /* as in Register: a positional "help" or "h" reaches the command */
        HideHelpCommand: true,
        /* as in NewCommandContext: the engine's default would end the process on any error the command returns */
        ExitErrHandler: inertExitHandler,
    }

    return engineCommand.Run(ctx, NormalizeVerbosityArguments(arguments))
}
