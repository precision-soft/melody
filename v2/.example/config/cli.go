package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    "github.com/precision-soft/melody/v2/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        melodycron.NewGenerateCommand(newCronConfiguration(kernelInstance)),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
