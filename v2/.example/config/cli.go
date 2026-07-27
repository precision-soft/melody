package config

import (
    "github.com/precision-soft/melody/v2/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        cli.NewCatalogNoteCommand(),
        melodycron.NewGenerateCommand(newCronConfiguration(kernelInstance)),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
