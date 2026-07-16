package cron

import (
    "errors"
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
            matcher, matcherErr := newScheduleMatcher(testCase.schedule, "")
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
        {name: "embedded whitespace in a list", schedule: &Schedule{Minute: "1, 5"}},
        {name: "embedded tab in a range", schedule: &Schedule{Minute: "1\t-5"}},
        {name: "step on a single value", schedule: &Schedule{Minute: "5/2"}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, matcherErr := newScheduleMatcher(testCase.schedule, "")
            if nil == matcherErr {
                t.Fatalf("expected a parse error for %q", testCase.name)
            }
        })
    }
}

/** @info the accepted shapes mirror what the generator's field validation lets through, so a Configuration that runs in-process also generates. */
func TestScheduleMatcher_AcceptsWellFormedFields(t *testing.T) {
    cases := []struct {
        name     string
        schedule *Schedule
    }{
        {name: "list without whitespace", schedule: &Schedule{Minute: "1,5"}},
        {name: "stepped range", schedule: &Schedule{Minute: "5-59/2"}},
        {name: "stepped wildcard", schedule: &Schedule{Minute: "*/2"}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, matcherErr := newScheduleMatcher(testCase.schedule, "")
            if nil != matcherErr {
                t.Fatalf("unexpected parse error: %v", matcherErr)
            }
        })
    }
}

/** @info the entry classification follows vixie cron: an entry is fixed-time only when both the minute and the hour pin concrete values, and a wildcard that carries a step still counts as a wildcard. The classification models the runner's own wall-clock reconciliation, so it is independent of the day-combination dialect. */
func TestScheduleMatcher_FixedTimeClassification(t *testing.T) {
    cases := []struct {
        name      string
        schedule  *Schedule
        fixedTime bool
    }{
        {name: "pinned minute and hour", schedule: &Schedule{Minute: "30", Hour: "3"}, fixedTime: true},
        {name: "nil schedule", schedule: nil, fixedTime: false},
        {name: "wildcard minute", schedule: &Schedule{Minute: "*", Hour: "3"}, fixedTime: false},
        {name: "wildcard hour", schedule: &Schedule{Minute: "30", Hour: "*"}, fixedTime: false},
        {name: "blank hour defaults to the wildcard", schedule: &Schedule{Minute: "30"}, fixedTime: false},
        {name: "stepped wildcard minute", schedule: &Schedule{Minute: "*/5", Hour: "3"}, fixedTime: false},
        {name: "stepped wildcard hour", schedule: &Schedule{Minute: "30", Hour: "*/2"}, fixedTime: false},
        {name: "explicit range is not a wildcard", schedule: &Schedule{Minute: "0-59", Hour: "3"}, fixedTime: true},
        {name: "restricted day fields do not affect the class", schedule: &Schedule{Minute: "30", Hour: "3", DayOfMonth: "1", DayOfWeek: "1"}, fixedTime: true},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            for _, dialect := range []RunnerDialect{RunnerDialectCrontab, RunnerDialectKubernetes} {
                matcher, matcherErr := newScheduleMatcher(testCase.schedule, dialect)
                if nil != matcherErr {
                    t.Fatalf("unexpected parse error under the %q dialect: %v", dialect, matcherErr)
                }

                if testCase.fixedTime != matcher.fixedTime() {
                    t.Fatalf("expected fixedTime %v under the %q dialect, got %v", testCase.fixedTime, dialect, matcher.fixedTime())
                }
            }
        })
    }
}

/** @info calendar facts the day-dialect cases rely on, asserted so the expectations stay self-verifying: in July 2026 the 13th and the 27th are odd-numbered Mondays, the 15th is an odd-numbered Wednesday, the 20th is an even-numbered Monday, and the 14th and the 21st are Tuesdays. */
func assertJuly2026Weekdays(t *testing.T) {
    t.Helper()

    expected := map[int]time.Weekday{
        13: time.Monday,
        14: time.Tuesday,
        15: time.Wednesday,
        20: time.Monday,
        21: time.Tuesday,
        27: time.Monday,
    }

    for day, weekday := range expected {
        at := time.Date(2026, time.July, day, 0, 0, 0, 0, time.UTC)
        if weekday != at.Weekday() {
            t.Fatalf("expected 2026-07-%02d to be a %s, got a %s", day, weekday, at.Weekday())
        }
    }
}

/** @info the schedule pins midnight, steps the day of month across the odd days and restricts the day of week to Monday. Under the crontab dialect the stepped-wildcard day of month is star-based and therefore unrestricted, so the day fields combine with and and only odd-numbered Mondays match — vixie crond's rule; under the kubernetes dialect only the plain wildcard is unrestricted, so the day fields combine with or and any odd day or any Monday matches — the robfig rule. */
func TestScheduleMatcher_DayDialectClassifiesTheSteppedWildcard(t *testing.T) {
    assertJuly2026Weekdays(t)

    schedule := &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/2", DayOfWeek: "1"}

    cases := []struct {
        name     string
        dialect  RunnerDialect
        at       time.Time
        expected bool
    }{
        {
            name:     "zero-value default matches an odd-numbered Monday",
            dialect:  "",
            at:       time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "zero-value default matches a later odd-numbered Monday",
            dialect:  "",
            at:       time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "zero-value default rejects an odd-numbered Wednesday",
            dialect:  "",
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "zero-value default rejects an even-numbered Monday",
            dialect:  "",
            at:       time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "named crontab dialect matches an odd-numbered Monday",
            dialect:  RunnerDialectCrontab,
            at:       time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "named crontab dialect rejects an odd-numbered Wednesday",
            dialect:  RunnerDialectCrontab,
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "kubernetes dialect matches an odd-numbered Wednesday",
            dialect:  RunnerDialectKubernetes,
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "kubernetes dialect matches an even-numbered Monday",
            dialect:  RunnerDialectKubernetes,
            at:       time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "kubernetes dialect matches an odd-numbered Monday",
            dialect:  RunnerDialectKubernetes,
            at:       time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "kubernetes dialect rejects an even-numbered Tuesday",
            dialect:  RunnerDialectKubernetes,
            at:       time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            matcher, matcherErr := newScheduleMatcher(schedule, testCase.dialect)
            if nil != matcherErr {
                t.Fatalf("unexpected parse error: %v", matcherErr)
            }

            if got := matcher.Matches(testCase.at); got != testCase.expected {
                t.Fatalf("Matches(%s) = %t, want %t", testCase.at.Format(time.RFC3339), got, testCase.expected)
            }
        })
    }
}

/** @info a plain wildcard day field is unrestricted in both dialects, so the remaining restricted day field applies on its own either way. */
func TestScheduleMatcher_PlainWildcardDayFieldsBehaveIdenticallyAcrossDialects(t *testing.T) {
    assertJuly2026Weekdays(t)

    for _, dialect := range []RunnerDialect{RunnerDialectCrontab, RunnerDialectKubernetes} {
        wildcardDayOfMonth, dayOfMonthErr := newScheduleMatcher(&Schedule{Minute: "0", Hour: "0", DayOfMonth: "*", DayOfWeek: "1"}, dialect)
        if nil != dayOfMonthErr {
            t.Fatalf("unexpected parse error under the %q dialect: %v", dialect, dayOfMonthErr)
        }

        if false == wildcardDayOfMonth.Matches(time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected a Monday to match a wildcard day of month under the %q dialect", dialect)
        }

        if true == wildcardDayOfMonth.Matches(time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected a Tuesday not to match a Monday-only day of week under the %q dialect", dialect)
        }

        wildcardDayOfWeek, dayOfWeekErr := newScheduleMatcher(&Schedule{Minute: "0", Hour: "0", DayOfMonth: "15", DayOfWeek: "*"}, dialect)
        if nil != dayOfWeekErr {
            t.Fatalf("unexpected parse error under the %q dialect: %v", dialect, dayOfWeekErr)
        }

        if false == wildcardDayOfWeek.Matches(time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected the 15th to match a wildcard day of week under the %q dialect", dialect)
        }

        if true == wildcardDayOfWeek.Matches(time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected the 14th not to match a 15th-only day of month under the %q dialect", dialect)
        }
    }
}

/** @info two genuinely restricted day fields — a range without a step and a pinned weekday — combine with or in both dialects: the dialect changes only how a star-based field is classified, never the or over two restricted fields, matching vixie crond, whose day-star flags read solely a leading star. */
func TestScheduleMatcher_TwoRestrictedDayFieldsCombineWithOrInBothDialects(t *testing.T) {
    assertJuly2026Weekdays(t)

    schedule := &Schedule{Minute: "0", Hour: "0", DayOfMonth: "1-15", DayOfWeek: "1"}

    for _, dialect := range []RunnerDialect{RunnerDialectCrontab, RunnerDialectKubernetes} {
        matcher, matcherErr := newScheduleMatcher(schedule, dialect)
        if nil != matcherErr {
            t.Fatalf("unexpected parse error under the %q dialect: %v", dialect, matcherErr)
        }

        if false == matcher.Matches(time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected a Monday outside the day-of-month range to match through the day of week under the %q dialect", dialect)
        }

        if false == matcher.Matches(time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected a Tuesday inside the day-of-month range to match through the day of month under the %q dialect", dialect)
        }

        if true == matcher.Matches(time.Date(2026, time.July, 21, 0, 0, 0, 0, time.UTC)) {
            t.Fatalf("expected a Tuesday outside the day-of-month range not to match under the %q dialect", dialect)
        }
    }
}

/** @info the robfig scheduler keeps its star bit on the plain and the unit-stepped wildcard and on any list containing them, so those shapes are unrestricted under the kubernetes dialect and the day fields combine with and; only a stepped wildcard with a step above one stays restricted and combines with or. */
func TestScheduleMatcher_KubernetesDialectKeepsTheStarBitShapesUnrestricted(t *testing.T) {
    assertJuly2026Weekdays(t)

    cases := []struct {
        name     string
        schedule *Schedule
        at       time.Time
        expected bool
    }{
        {
            name:     "unit-stepped wildcard day of month keeps the and rule on a Monday",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/1", DayOfWeek: "1"},
            at:       time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "unit-stepped wildcard day of month keeps the and rule off a Monday",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/1", DayOfWeek: "1"},
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "list containing the wildcard keeps the and rule on a Monday",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "5,*", DayOfWeek: "1"},
            at:       time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "list containing the wildcard keeps the and rule off a Monday",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "5,*", DayOfWeek: "1"},
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "unit-stepped wildcard day of week keeps the and rule on the pinned day",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "15", DayOfWeek: "*/1"},
            at:       time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
        {
            name:     "unit-stepped wildcard day of week keeps the and rule off the pinned day",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "15", DayOfWeek: "*/1"},
            at:       time.Date(2026, time.July, 14, 0, 0, 0, 0, time.UTC),
            expected: false,
        },
        {
            name:     "wide-stepped wildcard day of month stays restricted and combines with or",
            schedule: &Schedule{Minute: "0", Hour: "0", DayOfMonth: "*/2", DayOfWeek: "1"},
            at:       time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC),
            expected: true,
        },
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            matcher, matcherErr := newScheduleMatcher(testCase.schedule, RunnerDialectKubernetes)
            if nil != matcherErr {
                t.Fatalf("unexpected parse error: %v", matcherErr)
            }

            if got := matcher.Matches(testCase.at); got != testCase.expected {
                t.Fatalf("Matches(%s) = %t, want %t", testCase.at.Format(time.RFC3339), got, testCase.expected)
            }
        })
    }
}

/** @info the robfig scheduler bounds day of week at 6, so the kubernetes dialect must reject the Sunday alias 7 a crontab accepts — otherwise an in-process-green schedule renders a CronJob manifest the cluster rejects. */
func TestScheduleMatcher_KubernetesDialectRejectsDayOfWeekSeven(t *testing.T) {
    schedule := &Schedule{Minute: "0", Hour: "12", DayOfWeek: "7"}

    if _, crontabErr := newScheduleMatcher(schedule, RunnerDialectCrontab); nil != crontabErr {
        t.Fatalf("expected the crontab dialect to keep accepting the Sunday alias 7, got %v", crontabErr)
    }

    if _, kubernetesErr := newScheduleMatcher(schedule, RunnerDialectKubernetes); nil == kubernetesErr {
        t.Fatal("expected the kubernetes dialect to reject day of week 7")
    }
}

/** @info shapes no target scheduler runs must fail at parse: a sign-prefixed number parses under strconv but not under crond, unicode whitespace survives per-item trimming into an unrunnable crontab token, and a step beyond the field's cardinality would overflow the expansion loop into minutes the range never allowed. */
func TestScheduleMatcher_RejectsUngeneratableShapes(t *testing.T) {
    cases := []struct {
        name     string
        schedule *Schedule
    }{
        {name: "sign-prefixed single value", schedule: &Schedule{Minute: "+5"}},
        {name: "sign-prefixed range bound", schedule: &Schedule{Minute: "+1-5"}},
        {name: "sign-prefixed step", schedule: &Schedule{Minute: "*/+2"}},
        {name: "no-break space in a list", schedule: &Schedule{Minute: "1,\u00a05"}},
        {name: "vertical tab in a range", schedule: &Schedule{Minute: "1\v-5"}},
        {name: "form feed in a list", schedule: &Schedule{Minute: "1,\f5"}},
        {name: "step beyond the minute cardinality", schedule: &Schedule{Minute: "*/61"}},
        {name: "huge step over a narrow range", schedule: &Schedule{Minute: "50-59/9223372036854775800"}},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, matcherErr := newScheduleMatcher(testCase.schedule, "")
            if nil == matcherErr {
                t.Fatalf("expected a parse error for %q", testCase.name)
            }
        })
    }
}

/** @info a step of exactly the field's cardinality is every scheduler's degenerate "just the low value" and stays accepted, admitting only the range's low value. */
func TestScheduleMatcher_StepAtTheFieldCardinalityAdmitsOnlyTheLowValue(t *testing.T) {
    matcher, matcherErr := newScheduleMatcher(&Schedule{Minute: "*/60"}, "")
    if nil != matcherErr {
        t.Fatalf("unexpected parse error: %v", matcherErr)
    }

    if false == matcher.Matches(time.Date(2026, time.July, 15, 9, 0, 0, 0, time.UTC)) {
        t.Fatal("expected minute 0 to match a cardinality-wide step")
    }

    if true == matcher.Matches(time.Date(2026, time.July, 15, 9, 30, 0, 0, time.UTC)) {
        t.Fatal("expected minute 30 not to match a cardinality-wide step")
    }
}

func TestScheduleMatcher_UnknownDialectIsRejected(t *testing.T) {
    _, matcherErr := newScheduleMatcher(nil, RunnerDialect("solaris"))
    if nil == matcherErr {
        t.Fatal("expected a parse error for an unknown runner dialect")
    }

    if false == errors.Is(matcherErr, ErrUnknownRunnerDialect) {
        t.Fatalf("expected ErrUnknownRunnerDialect, got %v", matcherErr)
    }
}
