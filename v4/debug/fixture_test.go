package debug

import (
    "bytes"
    "context"
    "errors"
    "strings"
    "testing"

    melodycli "github.com/precision-soft/melody/v4/cli"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    middlewarepipeline "github.com/precision-soft/melody/v4/http/middleware/pipeline"
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

func (instance *testRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

var _ containercontract.Runtime = (*testRuntime)(nil)

func runContainerCommand(runtimeInstance *testRuntime, arguments []string) (string, error) {
    return runDebugCommand(NewContainerCommand(runtimeInstance.containerValue), runtimeInstance, arguments)
}

func runDebugCommand(
    command clicontract.Command,
    runtimeInstance containercontract.Runtime,
    arguments []string,
) (string, error) {
    buffer := &bytes.Buffer{}

    commandArguments := make([]string, 0, len(arguments)+1)
    commandArguments = append(commandArguments, command.Name())
    commandArguments = append(commandArguments, arguments...)

    runErr := melodycli.DispatchCommand(context.Background(), command, runtimeInstance, commandArguments, buffer, melodycli.DispatchOptions{})

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

func exceptionContextOf(t *testing.T, err error) exceptioncontract.Context {
    t.Helper()

    var exceptionErr *exception.Error
    if false == errors.As(err, &exceptionErr) {
        t.Fatalf("expected an exception error, got %T %v", err, err)
    }

    return exceptionErr.Context()
}
