package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func newCronConfiguration(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
    return cronConfiguration(kernelInstance.Config().Get("app.cron.product_user").String())
}

/* cronConfiguration is the schedule itself, under the account the catalogue commands run as. */
func cronConfiguration(productUser string) *melodycron.Configuration {
    return melodycron.NewConfiguration().
        /* the reading is what a request would otherwise take on a cold cache, so it is taken on the hour and left warm for whoever asks next */
        Schedule(melodycron.CommandName(cli.NewCatalogReportRefreshCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "*"},
            User:     productUser,
        }).
        /* the entry declares its own arguments, and both halves honour them: the generator renders them into the manifest line and the in-process runner hands them to the child command */
        Schedule(melodycron.CommandName(cli.NewProductListCommand), &melodycron.EntryConfig{
            Schedule:  &melodycron.Schedule{Minute: "0", Hour: "*/6"},
            User:      productUser,
            Arguments: []string{"--limit=2"},
        }).
        /* the rates move all day, so the refresh runs on the half hour: a converted price is never a day old, and a provider refusing for a few minutes is answered by the next run rather than by a retry loop. It runs unattended, so the command exits non-zero when it cannot read the provider rather than leaving the catalogue quoting stale rates in silence. */
        Schedule(melodycron.CommandName(cli.NewCurrencyRefreshRatesCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "*/30", Hour: "*"},
            User:     productUser,
        }).
        Schedule(melodycron.CommandName(cli.NewAppInfoCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "12"},
        })
}

/* cronRunnerCommands are the commands the cron Configuration schedules by name, handed to the in-process melody:cron:run scheduler; the one Configuration drives both the generated manifest and the runner. */
func cronRunnerCommands() []clicontract.Command {
    return []clicontract.Command{
        cli.NewCatalogReportRefreshCommand(),
        cli.NewCurrencyRefreshRatesCommand(),
        cli.NewProductListCommand(),
        cli.NewAppInfoCommand(),
    }
}
