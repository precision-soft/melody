package debug

import (
    "bytes"
    "context"
    "strings"

    clicontract "github.com/precision-soft/melody/cli/contract"
    containercontract "github.com/precision-soft/melody/container/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

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
