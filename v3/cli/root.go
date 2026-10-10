package cli

import (
    "context"
    "io"
    "strings"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* Root is the command tree an application dispatches from: it holds the registered commands and runs one of them against a command line. It owns its command list, so the registration can trust what it walks. */
type Root struct {
    command *clicontract.CommandContext
}

/* NewRoot builds the tree over NewCommandContext, whose inert exit handler it keeps: the tree's command is its own, so no caller can put the engine's default back. */
func NewRoot(applicationName string, applicationDescription string) *Root {
    return &Root{
        command: NewCommandContext(applicationName, applicationDescription),
    }
}

/* SetWriter points the tree's output at a stream: the help and usage the tree writes, and the output of every command registered on it, whenever registered. Left unset, the stream is the process's standard output. */
func (instance *Root) SetWriter(writer io.Writer) {
    instance.command.Writer = writer

    for _, registered := range instance.command.Commands {
        registered.Writer = writer
    }
}

/* SetErrorWriter points the tree's error output at a stream, for the tree and for every command registered on it, for the same reason SetWriter does. Left unset, the stream is the process's standard error. */
func (instance *Root) SetErrorWriter(writer io.Writer) {
    instance.command.ErrWriter = writer

    for _, registered := range instance.command.Commands {
        registered.ErrWriter = writer
    }
}

/* Register appends a command to the tree, as cli.Register does on a root command; the tree's streams reach it whether they are set before or after. */
func (instance *Root) Register(command clicontract.Command, runtimeInstance runtimecontract.Runtime) {
    registerCommand(instance.command, command, runtimeInstance)
}

/* Run dispatches one command line, arguments[0] being the program name as the process received it. The error a command returned is answered rather than acted on: the caller owns what happens to the process. */
func (instance *Root) Run(ctx context.Context, arguments []string) error {
    return instance.command.Run(ctx, arguments)
}

/* CommandNames answers the names registered on the tree, in registration order. */
func (instance *Root) CommandNames() []string {
    names := make([]string, 0, len(instance.command.Commands))

    for _, registered := range instance.command.Commands {
        names = append(names, strings.TrimSpace(registered.Name))
    }

    return names
}
