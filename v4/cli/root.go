package cli

import (
    "context"
    "io"
    "strings"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Root is the command tree an application dispatches from: it holds the registered commands and runs one of them against a command line. It owns its command list, so the registration can trust what it walks. */
type Root struct {
    command *clicontract.CommandContext
}

/* RootConfig is what the tree is built with; the zero value is an undeclared version and the process's standard streams. */
type RootConfig struct {
    /* Version is the application's version: --version prints it, <unknown> when empty, and the tree's commands read it through output.ApplicationVersionOf. */
    Version string
    /* Writer is the stream of the help and usage the tree writes and of every command registered on it; nil, or a typed nil, is the process's standard output. */
    Writer io.Writer
    /* ErrorWriter is the error stream of the tree and of every command registered on it; nil, or a typed nil, is the process's standard error. */
    ErrorWriter io.Writer
}

/* NewRoot builds the tree over NewCommandContext, whose inert exit handler it keeps: the tree's command is its own, so no caller can put the engine's default back. */
func NewRoot(applicationName string, applicationDescription string, rootConfig RootConfig) *Root {
    command := NewCommandContext(applicationName, applicationDescription)

    command.Version = rootConfig.Version
    if "" == rootConfig.Version {
        command.Version = output.UnknownApplicationVersion
    }

    if nil == command.Metadata {
        command.Metadata = make(map[string]any)
    }

    command.Metadata[output.MetadataApplicationVersion] = rootConfig.Version
    command.Writer = resolvedWriter(rootConfig.Writer)
    command.ErrWriter = resolvedWriter(rootConfig.ErrorWriter)

    return &Root{
        command: command,
    }
}

/* Register appends a command to the tree, as cli.Register does on a root command, stamping the tree's streams on it. */
func (instance *Root) Register(command clicontract.Command, runtimeInstance containercontract.Runtime) {
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
