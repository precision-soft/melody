package config

import (
    "github.com/precision-soft/melody/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodyclicontract "github.com/precision-soft/melody/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron"
    melodykernelcontract "github.com/precision-soft/melody/kernel/contract"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        melodycron.NewGenerateCommand(newCronConfiguration(kernelInstance)),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
