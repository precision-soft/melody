package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        melodycron.NewGenerateCommand(newCronConfiguration(kernelInstance)),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
