package config

import (
    "sort"
    "strings"
    "testing"
    "time"

    melodycron "github.com/precision-soft/melody/integrations/cron/v4"
    "github.com/precision-soft/melody/v4/.example/cli"
    "github.com/precision-soft/melody/v4/bound"
)

func TestCronConfiguration_SchedulesEachCommandAtItsOwnCadenceUnderTheCatalogueAccount(t *testing.T) {
    expected := map[string]string{
        melodycron.CommandName(cli.NewCatalogReportRefreshCommand):   "0 * user=catalogue args=",
        melodycron.CommandName(cli.NewProductListCommand):            "0 */6 user=catalogue args=--limit=2",
        melodycron.CommandName(cli.NewCurrencyRefreshRatesCommand):   "*/30 * user=catalogue args=",
        melodycron.CommandName(cli.NewApplicationInformationCommand): "* * user= args=",
    }

    entryList := cronConfiguration("catalogue").Entries()
    if len(expected) != len(entryList) {
        t.Fatalf("expected %d scheduled commands, got %d", len(expected), len(entryList))
    }

    for _, entry := range entryList {
        want, known := expected[entry.CommandName]
        if false == known {
            t.Fatalf("expected no schedule for %q", entry.CommandName)
        }

        schedule := entry.Config.Schedule
        got := schedule.Minute + " " + schedule.Hour + " user=" + entry.Config.User + " args=" + strings.Join(entry.Config.Arguments, ",")
        if want != got || "" != schedule.DayOfMonth+schedule.Month+schedule.DayOfWeek {
            t.Fatalf("expected %q scheduled as %q, got %q (day %q month %q weekday %q)", entry.CommandName, want, got, schedule.DayOfMonth, schedule.Month, schedule.DayOfWeek)
        }
    }
}

func TestCronRunnerCommands_AreTheCommandsTheConfigurationSchedules(t *testing.T) {
    scheduled := []string{}
    for _, entry := range cronConfiguration("catalogue").Entries() {
        scheduled = append(scheduled, entry.CommandName)
    }

    handed := []string{}
    for _, command := range cronRunnerCommands() {
        handed = append(handed, command.Name())
    }

    sort.Strings(scheduled)
    sort.Strings(handed)

    if strings.Join(scheduled, ",") != strings.Join(handed, ",") {
        t.Fatalf("expected the runner handed exactly the scheduled commands, scheduled %v and handed %v", scheduled, handed)
    }
}

func TestCronConfiguration_EvaluatesOnTheBucharestDayAndBoundsTheRatesRefresh(t *testing.T) {
    configuration := cronConfiguration("catalogue")
    if "Europe/Bucharest" != configuration.TimezoneName() {
        t.Fatalf("expected the schedules evaluated in Europe/Bucharest, got %q", configuration.TimezoneName())
    }

    for _, entry := range configuration.Entries() {
        expected := bound.Duration{}
        if melodycron.CommandName(cli.NewCurrencyRefreshRatesCommand) == entry.CommandName {
            expected = bound.Of(5 * time.Minute)
        }

        if expected != entry.Config.Timeout {
            t.Fatalf("expected %q bounded by %s, got %s", entry.CommandName, expected, entry.Config.Timeout)
        }
    }
}

func TestCronConfiguration_OnlyTheHeartbeatRunsOnEveryReplica(t *testing.T) {
    for _, entry := range cronConfiguration("catalogue").Entries() {
        heartbeat := melodycron.CommandName(cli.NewApplicationInformationCommand) == entry.CommandName
        if heartbeat != entry.Config.TickDedup.Disabled {
            t.Fatalf("expected only the heartbeat to disable the tick dedup, %q has %v", entry.CommandName, entry.Config.TickDedup.Disabled)
        }
    }
}
