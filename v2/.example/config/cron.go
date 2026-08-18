package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    "github.com/precision-soft/melody/v2/.example/cli"
    melodyclicontract "github.com/precision-soft/melody/v2/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
)

func newCronConfiguration(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
    productUser := kernelInstance.Config().Get("app.cron.product_user").String()

    return melodycron.NewConfiguration().
        /* the report is what a request would otherwise compute on a cold cache, so it is taken on the hour and left warm for whoever asks next */
        Schedule(melodycron.CommandName(cli.NewCatalogReportRefreshCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "*"},
            User:     productUser,
        }).
        Schedule(melodycron.CommandName(cli.NewProductListCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "*/6"},
            User:     productUser,
        }).
        Schedule(melodycron.CommandName(cli.NewAppInfoCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "12"},
        })
}

/* cronRunnerCommands is the in-process half of the schedule: the same three commands newCronConfiguration schedules by name, instantiated for melody:cron:run to invoke when their minute comes. */
func cronRunnerCommands() []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewCatalogReportRefreshCommand(),
        cli.NewProductListCommand(),
        cli.NewAppInfoCommand(),
    }
}
