package config

import (
    "github.com/precision-soft/melody/v2/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

/* the cron commands are absent on purpose: melody:cron:generate and melody:cron:run come from the cron module Configure registers, and the db:* family from the bunorm/migrate module beside it. */
func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        cli.NewCatalogJournalCommand(),
        cli.NewCatalogReportRefreshCommand(),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
