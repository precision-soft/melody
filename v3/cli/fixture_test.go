/* The shared test material of this package: the runtime and command doubles every test file of it
reaches for. It carries no mirror of its own on purpose: it is the ONE test file of a package allowed
to exist without a matching source, which is what keeps every other one honest. A test provable from a
single source belongs in that source's own mirror, not here. */
package cli

import (
    "context"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func newTestRuntime() *testRuntime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()
    defer scope.Close()

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

func (instance *testRuntime) Container() containercontract.Container {
    return instance.containerValue
}

var _ runtimecontract.Runtime = (*testRuntime)(nil)

type testCommand struct {
    nameValue        string
    descriptionValue string
    flagsValue       []clicontract.Flag
    runCallback      func(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error
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

func (instance *testCommand) Run(runtimeInstance runtimecontract.Runtime, commandContext clicontract.Context) error {
    return instance.runCallback(runtimeInstance, commandContext)
}
