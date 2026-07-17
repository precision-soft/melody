package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func newCronConfiguration(kernelInstance melodykernelcontract.Kernel) *melodycron.Configuration {
    productUser := kernelInstance.Config().Get("app.cron.product_user").String()

    return melodycron.NewConfiguration().
        Schedule(melodycron.CommandName(cli.NewProductListCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "*/6"},
            User:     productUser,
        }).
        Schedule(melodycron.CommandName(cli.NewAppInfoCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "0", Hour: "12"},
        })
}

/* cronRunnerCommands are the same commands the cron Configuration schedules by name, handed to the in-process melody:cron:run scheduler so a single-binary deployment can run its schedule without an external crontab. The one Configuration drives both the generated manifest and the runner. */
func cronRunnerCommands() []clicontract.Command {
    return []clicontract.Command{
        cli.NewProductListCommand(),
        cli.NewAppInfoCommand(),
    }
}
