package cli

import (
    "context"
    "io"
    "strings"

    urfavecli "github.com/urfave/cli/v3"
)

/* Root is the command tree an application dispatches from: it holds the registered commands and runs one of them against a command line. It owns its command list, so the registration can trust what it walks. */
type Root struct {
    command     *urfavecli.Command
    writer      io.Writer
    errorWriter io.Writer
}

/* NewRoot builds the tree. The engine's exit handler is replaced with an inert one and cannot be put back: its default calls os.Exit on any error a command returns, and melody owns the process exit. */
func NewRoot(applicationName string, applicationDescription string) *Root {
    return &Root{
        command: &urfavecli.Command{
            Name:  applicationName,
            Usage: applicationDescription,
            ExitErrHandler: inertExitHandler,
        },
    }
}

/* SetWriter points the tree's output at a stream: the help and usage the tree writes, and the output of every command registered on it, whenever registered. Left unset, the stream is the process's standard output. */
func (instance *Root) SetWriter(writer io.Writer) {
    instance.writer = writer
    instance.command.Writer = writer

    for _, registered := range instance.command.Commands {
        registered.Writer = writer
    }
}

/* SetErrorWriter points the tree's error output at a stream, for the tree and for every command registered on it, for the same reason SetWriter does. Left unset, the stream is the process's standard error. */
func (instance *Root) SetErrorWriter(writer io.Writer) {
    instance.errorWriter = writer
    instance.command.ErrWriter = writer

    for _, registered := range instance.command.Commands {
        registered.ErrWriter = writer
    }
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
