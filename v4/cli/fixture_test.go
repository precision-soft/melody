package cli

import (
    "context"
    "testing"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* closed through t.Cleanup, not a defer, which would hand every test an already closed scope */
func newTestRuntime(t *testing.T) *testRuntime {
    t.Helper()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    scope := serviceContainer.NewScope()
    t.Cleanup(func() {
        _ = scope.CloseWithContext(context.Background())
    })

    return &testRuntime{
        contextValue:   context.Background(),
        scopeValue:     scope,
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

type testCommand struct {
    nameValue        string
    descriptionValue string
    flagsValue       []clicontract.Flag
    runCallback      func(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error
}

var _ clicontract.Command = (*testCommand)(nil)

func (instance *testCommand) Name() string {
    return instance.nameValue
}

func (instance *testCommand) Description() string {
    return instance.descriptionValue
}

func (instance *testCommand) Flags() []clicontract.Flag {
    return instance.flagsValue
}

func (instance *testCommand) Run(runtimeInstance containercontract.Runtime, commandContext *clicontract.CommandContext) error {
    return instance.runCallback(runtimeInstance, commandContext)
}
