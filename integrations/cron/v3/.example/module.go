package example

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyconfig "github.com/precision-soft/melody/v3/config"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
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

    /* the custom dialect carries the application's name itself, read off the configuration the kernel already holds, so its ownership line names this application the way the builtin dialects' line does */
    generateCommand.RegisterTemplate(&AnsibleCronTemplate{
        TaskNamePrefix:  "billing cron: ",
        ApplicationName: melodyconfig.ConfigMustFromContainer(kernelInstance.ServiceContainer()).Cli().Name(),
    })

    return []melodyclicontract.Command{
        NewBillingCleanupCommand(),
        generateCommand,
    }
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)
var _ melodyapplicationcontract.CliModule = (*Module)(nil)
