package cron

import (
    "strconv"
    "strings"
    "time"
    "unicode"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

/* the bounds of each cron field; day of week accepts 7 as an alias for Sunday, which time.Weekday reports as 0, except under the kubernetes dialect, where the robfig scheduler bounds the field at 6 and a 7 would render a CronJob manifest the cluster rejects. */
const (
    minuteMinimum              = 0
    minuteMaximum              = 59
    hourMinimum                = 0
    hourMaximum                = 23
    dayOfMonthMinimum          = 1
    dayOfMonthMaximum          = 31
    monthMinimum               = 1
    monthMaximum               = 12
    dayOfWeekMinimum           = 0
    dayOfWeekMaximum           = 7
    dayOfWeekMaximumKubernetes = 6
    dayOfWeekSunday            = 0
)

/* RunnerDialect selects how the in-process runner combines a schedule's day-of-month and day-of-week fields; the zero value is the crontab dialect. Two restricted day fields always combine with or. The crontab dialect follows vixie crond, where a star-based day field, plain or stepped, is unrestricted and the day fields combine with and; the kubernetes dialect follows the robfig scheduler behind the k8s template, where only the plain and unit-stepped wildcard, alone or in a list, are unrestricted, a wildcard stepped above one combines with or, and day of week is bounded at 6. Name the scheduler the generated manifests target. */
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

/* scheduleMatcher evaluates a five-field cron schedule against a wall-clock time to the minute. It parses the same Schedule the generator renders, so one Configuration drives both the manifests and the in-process runner; dayFieldsCombineWithOr carries the dialect's day rule, resolved at parse time. */
type scheduleMatcher struct {
    minute                 cronFieldMatcher
    hour                   cronFieldMatcher
    dayOfMonth             cronFieldMatcher
    month                  cronFieldMatcher
    dayOfWeek              cronFieldMatcher
    dayFieldsCombineWithOr bool
}

/* cronFieldMatcher is the set of values one field admits. starUnrestricted records whether any list item is the wildcard with the unit step, the shapes on which the robfig scheduler keeps its star bit; starBased records whether the expression begins with the wildcard, plain or stepped, which vixie cron reads for the crontab day rule and the wall-clock entry class. */
type cronFieldMatcher struct {
    allowed          map[int]bool
    starUnrestricted bool
    starBased        bool
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
        /* the target schedulers read three-letter names in these two fields, so the matcher folds them onto their numbers and runs the same schedule the generated manifests do */
        monthExpression = normalizeCronNameTokens(fieldOrWildcard(schedule.Month), cronMonthNameValues)
        dayOfWeekExpression = normalizeCronNameTokens(fieldOrWildcard(schedule.DayOfWeek), cronDayOfWeekNameValues)
    }

    /* the robfig scheduler reads a whole-field "?" in a day field as the wildcard with its star bit, so that dialect matches it as "*"; crond has no "?", so the crontab dialect refuses it */
    if RunnerDialectKubernetes == resolvedDialect {
        if "?" == dayOfMonthExpression {
            dayOfMonthExpression = "*"
        }

        if "?" == dayOfWeekExpression {
            dayOfWeekExpression = "*"
        }
    }

    minute, minuteErr := parseCronField(minuteExpression, cronFieldBounds{name: "Minute", minimum: minuteMinimum, maximum: minuteMaximum, dialect: resolvedDialect})
    if nil != minuteErr {
        return nil, minuteErr
    }

    hour, hourErr := parseCronField(hourExpression, cronFieldBounds{name: "Hour", minimum: hourMinimum, maximum: hourMaximum, dialect: resolvedDialect})
    if nil != hourErr {
        return nil, hourErr
    }

    dayOfMonth, dayOfMonthErr := parseCronField(dayOfMonthExpression, cronFieldBounds{name: "DayOfMonth", minimum: dayOfMonthMinimum, maximum: dayOfMonthMaximum, dialect: resolvedDialect})
    if nil != dayOfMonthErr {
        return nil, dayOfMonthErr
    }

    month, monthErr := parseCronField(monthExpression, cronFieldBounds{name: "Month", minimum: monthMinimum, maximum: monthMaximum, dialect: resolvedDialect})
    if nil != monthErr {
        return nil, monthErr
    }

    /* the robfig scheduler bounds day of week at 6, so under the kubernetes dialect a Sunday written as 7 fails here rather than render a manifest the cluster rejects */
    dayOfWeekFieldMaximum := dayOfWeekMaximum
    if RunnerDialectKubernetes == resolvedDialect {
        dayOfWeekFieldMaximum = dayOfWeekMaximumKubernetes
    }

    dayOfWeek, dayOfWeekErr := parseCronField(dayOfWeekExpression, cronFieldBounds{name: "DayOfWeek", minimum: dayOfWeekMinimum, maximum: dayOfWeekFieldMaximum, dialect: resolvedDialect})
    if nil != dayOfWeekErr {
        return nil, dayOfWeekErr
    }

    /* time.Weekday reports Sunday as 0, so a schedule that named Sunday as 7 must also match 0. */
    if true == dayOfWeek.allowed[dayOfWeekMaximum] {
        dayOfWeek.allowed[dayOfWeekSunday] = true
    }

    /* two restricted day fields combine with or in every dialect; the dialect decides only which star-based shapes count as unrestricted */
    dayFieldsCombineWithOr := false == dayOfMonth.starBased && false == dayOfWeek.starBased
    if RunnerDialectKubernetes == resolvedDialect {
        dayFieldsCombineWithOr = false == dayOfMonth.starUnrestricted && false == dayOfWeek.starUnrestricted
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

/* Matches reports whether the schedule fires at the given minute. Two restricted day-of-month / day-of-week fields fire when either matches; when either is unrestricted under the configured RunnerDialect the two combine with and, like the other fields. */
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

/* fixedTime reports whether the schedule pins both the minute and the hour, the vixie-cron entry class the wall-clock reconciliation reads: fixed-time entries are caught up or suppressed across a clock jump, wildcard entries follow the current minute. */
func (instance *scheduleMatcher) fixedTime() bool {
    return false == instance.minute.starBased && false == instance.hour.starBased
}

/* cronFieldBounds names the field being parsed, the limits it is judged against and the dialect that chose them, so a refusal says which position failed under which rule: "7" is a legal Sunday under crontab and out of range under kubernetes. The dialect is empty where the bounds are dialect-independent, in the generator's own validation. */
type cronFieldBounds struct {
    name    string
    minimum int
    maximum int
    dialect RunnerDialect
}

/* parseCronField expands one field into the set of values it admits, bounded to [bounds.minimum, bounds.maximum]. It supports the wildcard, a stepped wildcard, single values, low-high ranges, stepped ranges and comma-separated lists of those. */
func parseCronField(expression string, bounds cronFieldBounds) (cronFieldMatcher, error) {
    minimum := bounds.minimum
    maximum := bounds.maximum

    if "" == expression {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field is empty", bounds)
    }

    /* whitespace anywhere in a field is refused rather than trimmed: embedded whitespace, any unicode space, splits the generated crontab line and makes crond refuse the whole file, and the generator refuses leading and trailing whitespace too, so trimming here would admit a schedule that runs in-process but cannot be generated */
    if -1 != strings.IndexFunc(expression, unicode.IsSpace) {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field contains whitespace", bounds)
    }

    matcher := cronFieldMatcher{
        allowed:   make(map[int]bool),
        starBased: strings.HasPrefix(expression, "*"),
    }

    for _, part := range strings.Split(expression, ",") {
        if "" == part {
            return cronFieldMatcher{}, invalidScheduleError(expression, "list contains an empty item", bounds)
        }

        rangeExpression := part
        step := 1
        stepped := false

        if slashIndex := strings.Index(part, "/"); -1 != slashIndex {
            rangeExpression = part[:slashIndex]
            stepValue, stepParsed := parseCronNumber(part[slashIndex+1:])
            if false == stepParsed || 0 >= stepValue {
                return cronFieldMatcher{}, invalidScheduleError(expression, "step must be a positive integer", bounds)
            }

            /* a step wider than the field is clamped to the field's cardinality rather than refused, since crond accepts it and admits the low value alone; the clamp also keeps the expansion loop clear of integer overflow */
            step = stepValue
            if step > maximum-minimum+1 {
                step = maximum - minimum + 1
            }

            stepped = true
        }

        if "*" == rangeExpression && 1 == step {
            matcher.starUnrestricted = true
        }

        low := minimum
        high := maximum

        if "*" != rangeExpression {
            if dashIndex := strings.Index(rangeExpression, "-"); -1 != dashIndex {
                lowValue, lowParsed := parseCronNumber(rangeExpression[:dashIndex])
                highValue, highParsed := parseCronNumber(rangeExpression[dashIndex+1:])
                if false == lowParsed || false == highParsed {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "range bounds must be integers", bounds)
                }
                low = lowValue
                high = highValue
            } else {
                /* classic cron refuses a step on a single value, so accepting one would admit a schedule the generated crontab cannot run */
                if true == stepped {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "step requires a range or the wildcard as its base", bounds)
                }

                singleValue, singleParsed := parseCronNumber(rangeExpression)
                if false == singleParsed {
                    return cronFieldMatcher{}, invalidScheduleError(expression, "value must be an integer", bounds)
                }
                low = singleValue
                high = singleValue
            }
        }

        if low < minimum || high > maximum || low > high {
            return cronFieldMatcher{}, invalidScheduleError(expression, "value is out of range", bounds)
        }

        for value := low; value <= high; value += step {
            matcher.allowed[value] = true
        }
    }

    return matcher, nil
}

/* parseCronNumber parses a field number as plain digits, since strconv accepts a sign prefix that neither vixie crond nor the robfig scheduler does. */
func parseCronNumber(text string) (int, bool) {
    if "" == text {
        return 0, false
    }

    for _, character := range text {
        if character < '0' || character > '9' {
            return 0, false
        }
    }

    value, parseErr := strconv.Atoi(text)
    if nil != parseErr {
        return 0, false
    }

    return value, true
}

/* invalidScheduleError names the field, the limits it was judged against and, where a dialect chose them, the dialect, beside the expression and the reason, since one expression can be valid under one dialect and refused under another. */
func invalidScheduleError(expression string, reason string, bounds cronFieldBounds) error {
    context := exceptioncontract.Context{
        "expression": expression,
        "reason":     reason,
    }

    if "" != bounds.name {
        context["field"] = bounds.name
        context["minimum"] = bounds.minimum
        context["maximum"] = bounds.maximum
    }

    if "" != bounds.dialect {
        context["dialect"] = string(bounds.dialect)
    }

    return exception.NewError(
        "cron: invalid schedule field",
        context,
        ErrInvalidSchedule,
    )
}
