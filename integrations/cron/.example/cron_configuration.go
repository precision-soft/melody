package example

import (
    melodycron "github.com/precision-soft/melody/integrations/cron"
)

func newCronConfiguration() *melodycron.Configuration {
    return melodycron.NewConfiguration().
        Schedule(melodycron.CommandName(NewBillingCleanupCommand), &melodycron.EntryConfig{
            Schedule: &melodycron.Schedule{Minute: "*/15"},
        })
}
