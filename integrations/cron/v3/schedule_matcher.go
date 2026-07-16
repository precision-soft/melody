package cron

import (
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* the bounds of each cron field; day of week accepts 7 as an alias for Sunday, which time.Weekday reports as 0. */
const (
    minuteMinimum     = 0
    minuteMaximum     = 59
    hourMinimum       = 0
    hourMaximum       = 23
    dayOfMonthMinimum = 1
    dayOfMonthMaximum = 31
    monthMinimum      = 1
    monthMaximum      = 12
    dayOfWeekMinimum  = 0
    dayOfWeekMaximum  = 7
    dayOfWeekSunday   = 0
)

/* RunnerDialect selects how the in-process runner combines a schedule's day-of-month and day-of-week fields. Two genuinely restricted day fields always combine with or — the rule both target schedulers share — and the dialect decides only how a star-based day field (the plain or the stepped wildcard) is classified. The crontab dialect follows vixie crond, whose day-star flag reads just the field's first character: a star-based day field counts as unrestricted, so the day fields combine with and and only the restricted one constrains the day. The kubernetes dialect follows the robfig scheduler behind the k8s template, which treats only the plain wildcard as unrestricted: a stepped wildcard day field stays restricted and combines with or. The two real schedulers genuinely diverge on that one shape, so the dialect should name the scheduler the generated manifests target; every other matching rule is dialect-independent. The zero value selects the crontab dialect. */
type RunnerDialect string

const (
    RunnerDialectCrontab    RunnerDialect = "crontab"
    RunnerDialectKubernetes RunnerDialect = "kubernetes"
)

/* resolveRunnerDialect normalizes the zero value to the crontab default and rejects a value naming no known dialect, so a misconfigured dialect surfaces at construction instead of silently matching days under the wrong rule. */
func resolveRunnerDialect(dialect RunnerDialect) (RunnerDialect, *exception.Error) {
    switch dialect {
    case "":
        return RunnerDialectCrontab, nil
    case RunnerDialectCrontab, RunnerDialectKubernetes:
        return dialect, nil
    }

    return "", exception.NewError(
        "cron: unknown runner dialect",
        exceptioncontract.Context{
            "dialect": string(dialect),
        },
        ErrUnknownRunnerDialect,
    )
}

/* scheduleMatcher evaluates a five-field cron schedule against a wall-clock time to the minute. It parses the same Schedule the generator renders, so one Configuration drives both the emitted manifests and the in-process runner; sub-minute cadences are not cron-expressible and are out of scope. dayFieldsCombineWithOr carries the RunnerDialect's day rule, resolved once at parse time. */
type scheduleMatcher struct {
    minute                 cronFieldMatcher
    hour                   cronFieldMatcher
    dayOfMonth             cronFieldMatcher
    month                  cronFieldMatcher
    dayOfWeek              cronFieldMatcher
    dayFieldsCombineWithOr bool
}

/* cronFieldMatcher is the set of values one field admits. wildcard records whether the field was the unrestricted plain "*", which the kubernetes day rule reads. starBased records whether the expression begins with the wildcard, plain or stepped — the flag vixie cron reads both for the crontab day rule and when it classifies an entry for wall-clock reconciliation. */
type cronFieldMatcher struct {
    allowed   map[int]bool
    wildcard  bool
    starBased bool
}

func (instance cronFieldMatcher) matches(value int) bool {
    return instance.allowed[value]
}

/* newScheduleMatcher parses a Schedule into a matcher under the given day-combination dialect; a nil schedule (or blank fields) is the every-minute wildcard, mirroring Schedule.Expression's nil handling and Schedule.Defaults, and the zero-value dialect is the crontab default. */
func newScheduleMatcher(schedule *Schedule, dialect RunnerDialect) (*scheduleMatcher, error) {
    resolvedDialect, dialectErr := resolveRunnerDialect(dialect)
    if nil != dialectErr {
        return nil, dialectErr
    }

    minuteExpression := "*"
    hourExpression := "*"
    dayOfMonthExpression := "*"
    monthExpression := "*"
    dayOfWeekExpression := "*"

    if nil != schedule {
        minuteExpression = fieldOrWildcard(schedule.Minute)
        hourExpression = fieldOrWildcard(schedule.Hour)
        dayOfMonthExpression = fieldOrWildcard(schedule.DayOfMonth)
        monthExpression = fieldOrWildcard(schedule.Month)
        dayOfWeekExpression = fieldOrWildcard(schedule.DayOfWeek)
    }

    minute, minuteErr := parseCronField(minuteExpression, minuteMinimum, minuteMaximum)
    if nil != minuteErr {
        return nil, minuteErr
    }

    hour, hourErr := parseCronField(hourExpression, hourMinimum, hourMaximum)
    if nil != hourErr {
        return nil, hourErr
    }

    dayOfMonth, dayOfMonthErr := parseCronField(dayOfMonthExpression, dayOfMonthMinimum, dayOfMonthMaximum)
    if nil != dayOfMonthErr {
        return nil, dayOfMonthErr
    }

    month, monthErr := parseCronField(monthExpression, monthMinimum, monthMaximum)
    if nil != monthErr {
        return nil, monthErr
    }

    dayOfWeek, dayOfWeekErr := parseCronField(dayOfWeekExpression, dayOfWeekMinimum, dayOfWeekMaximum)
    if nil != dayOfWeekErr {
        return nil, dayOfWeekErr
    }

    /* time.Weekday reports Sunday as 0, so a schedule that named Sunday as 7 must also match 0. */
    if true == dayOfWeek.allowed[dayOfWeekMaximum] {
        dayOfWeek.allowed[dayOfWeekSunday] = true
    }

    /* two genuinely restricted day fields combine with or in every dialect; the dialect decides only whether any star-based day field counts as unrestricted (crontab) or just the plain wildcard does (kubernetes). */
    dayFieldsCombineWithOr := false == dayOfMonth.starBased && false == dayOfWeek.starBased
    if RunnerDialectKubernetes == resolvedDialect {
        dayFieldsCombineWithOr = false == dayOfMonth.wildcard && false == dayOfWeek.wildcard
    }

    return &scheduleMatcher{
        minute:                 minute,
        hour:                   hour,
        dayOfMonth:             dayOfMonth,
        month:                  month,
        dayOfWeek:              dayOfWeek,
        dayFieldsCombineWithOr: dayFieldsCombineWithOr,
    }, nil
}

/* Matches reports whether the schedule fires at the given minute. Two restricted day-of-month / day-of-week fields fire when either matches — the classic Vixie-cron or, shared by both dialects; when either day field is unrestricted under the configured RunnerDialect (any star-based field in the crontab dialect, only the plain wildcard in the kubernetes dialect) the two day fields combine with and, like the rest of the fields. */
func (instance *scheduleMatcher) Matches(at time.Time) bool {
    if false == instance.minute.matches(at.Minute()) {
        return false
    }

    if false == instance.hour.matches(at.Hour()) {
        return false
    }

    if false == instance.month.matches(int(at.Month())) {
        return false
    }

    dayOfMonthMatches := instance.dayOfMonth.matches(at.Day())
    dayOfWeekMatches := instance.dayOfWeek.matches(int(at.Weekday()))

    if true == instance.dayFieldsCombineWithOr {
        return dayOfMonthMatches || dayOfWeekMatches
    }

    return dayOfMonthMatches && dayOfWeekMatches
}

/* fixedTime reports whether the schedule pins both the minute and the hour — neither is a plain or stepped wildcard. This is the vixie-cron entry classification the runner's wall-clock reconciliation reads: fixed-time entries are caught up or suppressed across a clock jump, wildcard entries simply follow the current minute. */
func (instance *scheduleMatcher) fixedTime() bool {
    return false == instance.minute.starBased && false == instance.hour.starBased
}

/* parseCronField expands one field into the set of values it admits, bounded to [minimum, maximum]. It supports the wildcard, a stepped wildcard, single values, low-high ranges, stepped ranges and comma-separated lists of those. */
func parseCronField(expression string, minimum int, maximum int) (cronFieldMatcher, error) {
    trimmed := strings.TrimSpace(expression)
    if "" == trimmed {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field is empty")
    }

    /* a field with embedded whitespace would be split into extra columns by the generated crontab line, so the generator hard-rejects it; the matcher rejects it too, keeping every in-process schedule generatable. */
    if true == strings.ContainsAny(trimmed, " \t\n\r") {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field contains embedded whitespace")
    }

    matcher := cronFieldMatcher{
        allowed:   make(map[int]bool),
        wildcard:  "*" == trimmed,
        starBased: strings.HasPrefix(trimmed, "*"),
    }

    for _, part := range strings.Split(trimmed, ",") {
        part = strings.TrimSpace(part)
        if "" == part {
            return cronFieldMatcher{}, invalidScheduleError(expression, "list contains an empty item")
        }

        rangeExpression := part
        step := 1
        stepped := false

        if slashIndex := strings.Index(part, "/"); -1 != slashIndex {
            rangeExpression = part[:slashIndex]
            stepValue, stepErr := strconv.Atoi(part[slashIndex+1:])
            if nil != stepErr || 0 >= stepValue {
                return cronFieldMatcher{}, invalidScheduleError(expression, "step must be a positive integer")
            }
            step = stepValue
            stepped = true
        }

        low := minimum
        high := maximum

        if "*" != rangeExpression {
            if dashIndex := strings.Index(rangeExpression, "-"); -1 != dashIndex {
                lowValue, lowErr := strconv.Atoi(strings.TrimSpace(rangeExpression[:dashIndex]))
                highValue, highErr := strconv.Atoi(strings.TrimSpace(rangeExpression[dashIndex+1:]))
                if nil != lowErr || nil != highErr {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "range bounds must be integers")
                }
                low = lowValue
                high = highValue
            } else {
                /* a step only makes sense over a range or the wildcard; classic cron rejects a step on a single value, so accepting one here would admit a schedule the generated crontab cannot run. */
                if true == stepped {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "step requires a range or the wildcard as its base")
                }

                singleValue, singleErr := strconv.Atoi(rangeExpression)
                if nil != singleErr {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "value must be an integer")
                }
                low = singleValue
                high = singleValue
            }
        }

        if low < minimum || high > maximum || low > high {
            return cronFieldMatcher{}, invalidScheduleError(expression, "value is out of range")
        }

        for value := low; value <= high; value += step {
            matcher.allowed[value] = true
        }
    }

    return matcher, nil
}

func invalidScheduleError(expression string, reason string) error {
    return exception.NewError(
        "cron: invalid schedule field",
        exceptioncontract.Context{
            "expression": expression,
            "reason":     reason,
        },
        ErrInvalidSchedule,
    )
}
