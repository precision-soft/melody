package cron

import (
    "testing"
)

func TestScheduleDefaultsFillsEmptyFieldsWithWildcard(t *testing.T) {
    schedule := &Schedule{}

    schedule.Defaults()

    if "*" != schedule.Minute {
        t.Fatalf("Minute = %q, want %q", schedule.Minute, "*")
    }
    if "*" != schedule.Hour {
        t.Fatalf("Hour = %q, want %q", schedule.Hour, "*")
    }
    if "*" != schedule.DayOfMonth {
        t.Fatalf("DayOfMonth = %q, want %q", schedule.DayOfMonth, "*")
    }
    if "*" != schedule.Month {
        t.Fatalf("Month = %q, want %q", schedule.Month, "*")
    }
    if "*" != schedule.DayOfWeek {
        t.Fatalf("DayOfWeek = %q, want %q", schedule.DayOfWeek, "*")
    }
}

func TestScheduleDefaultsPreservesUserProvidedFields(t *testing.T) {
    schedule := &Schedule{
        Minute: "0",
        Hour:   "3",
    }

    schedule.Defaults()

    if "0" != schedule.Minute {
        t.Fatalf("Minute = %q, want %q", schedule.Minute, "0")
    }
    if "3" != schedule.Hour {
        t.Fatalf("Hour = %q, want %q", schedule.Hour, "3")
    }
    if "*" != schedule.DayOfMonth {
        t.Fatalf("DayOfMonth = %q, want %q", schedule.DayOfMonth, "*")
    }
}

func TestScheduleDefaultsOnNilReceiverIsSafe(t *testing.T) {
    var schedule *Schedule

    result := schedule.Defaults()

    if nil != result {
        t.Fatalf("Defaults on nil receiver should return nil, got %v", result)
    }
}

func TestScheduleExpressionBuildsFiveFieldString(t *testing.T) {
    schedule := Schedule{
        Minute:     "0",
        Hour:       "3",
        DayOfMonth: "*",
        Month:      "*",
        DayOfWeek:  "0",
    }

    if "0 3 * * 0" != schedule.Expression() {
        t.Fatalf("Expression() = %q, want %q", schedule.Expression(), "0 3 * * 0")
    }
}

func TestScheduleExpressionAutoFillsWildcardsForEmptyFields(t *testing.T) {
    schedule := Schedule{}

    expression := schedule.Expression()
    if "* * * * *" != expression {
        t.Fatalf("empty schedule expression should auto-fill wildcards, got %q", expression)
    }
}

func TestScheduleExpressionDoesNotMutateReceiver(t *testing.T) {
    schedule := Schedule{Minute: "0"}

    _ = schedule.Expression()

    if "" != schedule.Hour {
        t.Fatalf("Expression() must not mutate receiver; Hour = %q, want empty", schedule.Hour)
    }
    if "" != schedule.DayOfMonth {
        t.Fatalf("Expression() must not mutate receiver; DayOfMonth = %q, want empty", schedule.DayOfMonth)
    }
}

func TestScheduleExpressionMixesProvidedAndWildcards(t *testing.T) {
    schedule := Schedule{Minute: "0", Hour: "3"}

    if "0 3 * * *" != schedule.Expression() {
        t.Fatalf("Expression() = %q, want %q", schedule.Expression(), "0 3 * * *")
    }
}
