/* The shared test material of this package: the doubles every test file of it reaches for, the helpers
that build them, and — where a contract spans every source rather than any one of them — the test that
asserts it. It carries no mirror of its own on purpose: it is the ONE test file of a package allowed to
exist without a matching source, which is what keeps every other one honest. A test provable from a
single source belongs in that source's own mirror, not here. */
package debug

import (
    "bytes"
    "context"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v3/http/middleware/pipeline"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newTestMiddlewareCommandWithEmptyProviders() *MiddlewareCommand {
    return NewMiddlewareCommand(
        func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
            return nil, nil, nil
        },
        func() ([]httpcontract.Middleware, error) {
            return nil, nil
        },
    )
}

func newTestRuntime(serviceContainer containercontract.Container) *testRuntime {
    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     serviceContainer.NewScope(),
        containerValue: serviceContainer,
    }
}

type testRuntime struct {
    contextValue   context.Context
    scopeValue     containercontract.Scope
    containerValue containercontract.Container
}

func (instance *testRuntime) Context() context.Context {
    return instance.contextValue
}

func (instance *testRuntime) Scope() containercontract.Scope {
    return instance.scopeValue
}

func (instance *testRuntime) Container() containercontract.Container {
    return instance.containerValue
}

var _ runtimecontract.Runtime = (*testRuntime)(nil)

func runDebugCommand(
    command clicontract.Command,
    runtimeInstance runtimecontract.Runtime,
    arguments []string,
) (string, error) {
    buffer := &bytes.Buffer{}

    commandContext := &clicontract.CommandContext{
        Name:      command.Name(),
        Flags:     command.Flags(),
        Writer:    buffer,
        ErrWriter: buffer,
        ExitErrHandler: func(
            handlerContext context.Context,
            handlerCommandContext *clicontract.CommandContext,
            handlerErr error,
        ) {
        },
        Action: func(
            actionContext context.Context,
            actionCommandContext *clicontract.CommandContext,
        ) error {
            return command.Run(runtimeInstance, actionCommandContext)
        },
    }

    commandArguments := make([]string, 0, len(arguments)+1)
    commandArguments = append(commandArguments, command.Name())
    commandArguments = append(commandArguments, arguments...)

    runErr := commandContext.Run(context.Background(), commandArguments)

    return buffer.String(), runErr
}

func debugTableBlockRow(rendered string, blockTitle string) [][]string {
    row := [][]string{}

    isInsideBlock := false
    isHeaderConsumed := false

    for _, line := range strings.Split(rendered, "\n") {
        if blockTitle == strings.TrimSpace(line) {
            isInsideBlock = true
            isHeaderConsumed = false

            continue
        }

        if false == isInsideBlock {
            continue
        }

        if false == strings.HasPrefix(line, "|") {
            if "" != strings.TrimSpace(line) {
                isInsideBlock = false
            }

            continue
        }

        cell := []string{}
        for _, value := range strings.Split(strings.Trim(line, "|"), "|") {
            cell = append(cell, strings.TrimSpace(value))
        }

        if false == isHeaderConsumed {
            isHeaderConsumed = true

            continue
        }

        if true == isDebugTableSeparatorRow(cell) {
            continue
        }

        row = append(row, cell)
    }

    return row
}

func isDebugTableSeparatorRow(cell []string) bool {
    for _, value := range cell {
        if "" == value {
            return false
        }

        if "" != strings.Trim(value, "-") {
            return false
        }
    }

    return true
}

/* the name is what an operator types and the description is what `melody list` prints beside it; both are read from these accessors by the cli registrar, and an empty description leaves a command undiscoverable in the only place it is advertised */
func TestDebugCommands_CarryTheirNameAndDescription(t *testing.T) {
    commandList := []struct {
        command      clicontract.Command
        expectedName string
    }{
        {&ContainerCommand{}, "debug:container"},
        {&EventCommand{}, "debug:events"},
        {&RouterCommand{}, "debug:router"},
        {&ParameterCommand{}, "debug:parameters"},
        {newTestMiddlewareCommandWithEmptyProviders(), "debug:middleware"},
        {&VersionCommand{}, "debug:version"},
    }

    seenNameList := map[string]bool{}

    for _, commandEntry := range commandList {
        if commandEntry.expectedName != commandEntry.command.Name() {
            t.Fatalf("expected name %q, got %q", commandEntry.expectedName, commandEntry.command.Name())
        }

        if "" == commandEntry.command.Description() {
            t.Fatalf("%s: expected a description", commandEntry.expectedName)
        }

        if true == seenNameList[commandEntry.command.Name()] {
            t.Fatalf("two commands answer to %q", commandEntry.command.Name())
        }

        seenNameList[commandEntry.command.Name()] = true
    }
}
