package config

import (
    "sort"
    "strings"
    "testing"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
)

/* the schedule is a set of decisions each entry's comment gives the reason for — the reading on the hour, the
   listing every six hours with its own arguments, the rates on the half hour, the daily information at noon —
   and the catalogue commands run as the configured account while the information runs as none. Each entry is
   pinned whole, so a schedule, an argument or the account moved on any one of them is named by the command it
   moved on. */
func TestCronConfiguration_SchedulesEachCommandAtItsOwnCadenceUnderTheCatalogueAccount(t *testing.T) {
    expected := map[string]string{
        melodycron.CommandName(cli.NewCatalogReportRefreshCommand): "0 * user=catalogue args=",
        melodycron.CommandName(cli.NewProductListCommand):          "0 */6 user=catalogue args=--limit=2",
        melodycron.CommandName(cli.NewCurrencyRefreshRatesCommand): "*/30 * user=catalogue args=",
        melodycron.CommandName(cli.NewAppInfoCommand):              "0 12 user= args=",
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

/* the in-process runner is handed the commands the Configuration schedules by name, and the two lists are
   written apart: a command scheduled and not handed to the runner is one a single-binary deployment never runs,
   and one handed and not scheduled is dead weight. Compared as sets — the runner does not read an order. */
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
