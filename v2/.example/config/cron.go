package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v2"
    "github.com/precision-soft/melody/v2/.example/cli"
    melodykernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
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
