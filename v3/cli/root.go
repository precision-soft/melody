package cli

import (
    "context"
    "io"
    "strings"

    urfavecli "github.com/urfave/cli/v3"
)

/* Root is the command tree an application dispatches from: it holds the registered commands and runs one of them against a command line. It owns its command list rather than exposing it, which is what lets the registration trust what it walks — the list it scans for a duplicate name is one nothing outside this package can write into. */
type Root struct {
    command     *urfavecli.Command
    writer      io.Writer
    errorWriter io.Writer
}

/* NewRoot builds the tree. The engine's exit handler is replaced with an inert one on purpose and with no door to put it back: left at its default it ends the process itself, through os.Exit, on any error a command returns, and melody owns the process exit — the recover handler of the cli run resolves the final record's logger and closes the container between that record and the exit. An engine that exits first would take the journal with it. */
func NewRoot(applicationName string, applicationDescription string) *Root {
    return &Root{
        command: &urfavecli.Command{
            Name:  applicationName,
            Usage: applicationDescription,
            ExitErrHandler: inertExitHandler,
        },
    }
}

/* SetWriter points the tree's output at a stream — the help and usage the tree itself writes, and the output of every command registered on it, whenever it was registered. The engine defaults each command's stream separately, to the process's standard output, so a writer set only on the tree would leave every command writing somewhere else; this is the door that means what it says. Left unset, the stream is the process's standard output. */
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
