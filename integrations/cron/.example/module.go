package example

import (
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

type Module struct{}

func NewModule() *Module {
    return &Module{}
}

func (instance *Module) Name() string {
    return "billing"
}

func (instance *Module) Description() string {
    return "billing CLI commands with cron scheduling"
}

func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
    melodycron.RegisterDefaultParameters(registrar)
}

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    generateCommand := melodycron.NewGenerateCommand(newCronConfiguration())

    generateCommand.RegisterTemplate(&KubernetesCronjobTemplate{
        Namespace: "production",
        Image:     "myapp:latest",
    })

    return []melodyclicontract.Command{
        NewBillingCleanupCommand(),
        generateCommand,
    }
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)
var _ melodyapplicationcontract.CliModule = (*Module)(nil)
