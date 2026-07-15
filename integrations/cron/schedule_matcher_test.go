package cron

import (
    "testing"
    "time"
)

/** @info July 15 2026 is a Wednesday (weekday 3); the day-of-week cases below rely on that. */
func TestScheduleMatcher_Matches(t *testing.T) {
    reference := time.Date(2026, time.July, 15, 12, 30, 0, 0, time.UTC)

    cases := []struct {
        name     string
        schedule *Schedule
        at       time.Time
        expected bool
    }{
        {
            name:     "nil schedule is the every-minute wildcard",
            schedule: nil,
            at:       reference,
            expected: true,
        },
        {
            name:     "full wildcard matches any minute",
            schedule: &Schedule{Minute: "*", Hour: "*", DayOfMonth: "*", Month: "*", DayOfWeek: "*"},
            at:       reference,
            expected: true,
        },
        {
            name:     "stepped minute matches an aligned minute",
            schedule: &Schedule{Minute: "*/15"},
            at:       time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "stepped minute rejects an unaligned minute",
            schedule: &Schedule{Minute: "*/15"},
            at:       time.Date(2026, time.July, 15, 9, 31, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "minute zero on a six-hour step matches noon",
            schedule: &Schedule{Minute: "0", Hour: "*/6"},
            at:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "minute zero on a six-hour step rejects an off-hour",
            schedule: &Schedule{Minute: "0", Hour: "*/6"},
            at:       time.Date(2026, time.July, 15, 5, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "minute range matches inside the range",
            schedule: &Schedule{Minute: "10-12"},
            at:       time.Date(2026, time.July, 15, 8, 11, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "minute range rejects outside the range",
            schedule: &Schedule{Minute: "10-12"},
            at:       time.Date(2026, time.July, 15, 8, 13, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "minute list matches a listed value",
            schedule: &Schedule{Minute: "1,2,5"},
            at:       time.Date(2026, time.July, 15, 8, 5, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "day of month and month match a calendar day",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfMonth: "15", Month: "7"},
            at:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "day of week matches a weekday",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfWeek: "3"},
            at:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "day of week seven is Sunday",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfWeek: "7"},
            at:       time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "restricted day of month and day of week combine with or",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfMonth: "1", DayOfWeek: "3"},
            at:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "restricted day of month still matches on its day",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfMonth: "1", DayOfWeek: "3"},
            at:       time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "wildcard day of week keeps the day of month as an and",
            schedule: &Schedule{Minute: "0", Hour: "12", DayOfMonth: "1"},
            at:       time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
            expected: false,
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            matcher, matcherErr := newScheduleMatcher(testCase.schedule)
            if nil != matcherErr {
                t.Fatalf("unexpected parse error: %v", matcherErr)
            }

            if got := matcher.Matches(testCase.at); got != testCase.expected {
                t.Fatalf("Matches(%s) = %t, want %t", testCase.at.Format(time.RFC3339), got, testCase.expected)
            }
        })
    }
}

func TestScheduleMatcher_RejectsMalformedFields(t *testing.T) {
    cases := []struct {
        name     string
        schedule *Schedule
    }{
        {name: "minute out of range", schedule: &Schedule{Minute: "99"}},
        {name: "non-numeric value", schedule: &Schedule{Minute: "a"}},
        {name: "zero step", schedule: &Schedule{Minute: "*/0"}},
        {name: "inverted range", schedule: &Schedule{Minute: "5-2"}},
        {name: "empty list item", schedule: &Schedule{Minute: "1,,2"}},
        {name: "day of month below minimum", schedule: &Schedule{DayOfMonth: "0"}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, matcherErr := newScheduleMatcher(testCase.schedule)
            if nil == matcherErr {
                t.Fatalf("expected a parse error for %q", testCase.name)
            }
        })
    }
}
