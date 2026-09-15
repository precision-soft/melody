package cron

import (
    "strconv"
    "strings"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

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

/* RunnerDialect selects how the in-process runner combines a schedule's day-of-month and day-of-week fields. Two genuinely restricted day fields always combine with or — the rule both target schedulers share — and the dialect decides only how a star-based day field (the plain or the stepped wildcard) is classified. The crontab dialect follows vixie crond, whose day-star flag reads just the field's first character: a star-based day field counts as unrestricted, so the day fields combine with and and only the restricted one constrains the day. The kubernetes dialect follows the robfig scheduler behind the k8s template, whose star bit survives only the unit step: the plain and the unit-stepped wildcard (alone or inside a list) are unrestricted, while a stepped wildcard with a step above one stays restricted and combines with or. The kubernetes dialect also bounds day of week at 6, as robfig does — a 7 would render a CronJob manifest the cluster rejects. The two real schedulers genuinely diverge on those shapes, so the dialect should name the scheduler the generated manifests target; every other matching rule is dialect-independent. The zero value selects the crontab dialect. */
type RunnerDialect string

const (
    RunnerDialectCrontab    RunnerDialect = "crontab"
    RunnerDialectKubernetes RunnerDialect = "kubernetes"
)

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

type scheduleMatcher struct {
    minute                 cronFieldMatcher
    hour                   cronFieldMatcher
    dayOfMonth             cronFieldMatcher
    month                  cronFieldMatcher
    dayOfWeek              cronFieldMatcher
    dayFieldsCombineWithOr bool
}

type cronFieldMatcher struct {
    allowed          map[int]bool
    starUnrestricted bool
    starBased        bool
}

func (instance cronFieldMatcher) matches(value int) bool {
    return instance.allowed[value]
}

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

        monthExpression = normalizeCronNameTokens(fieldOrWildcard(schedule.Month), cronMonthNameValues)
        dayOfWeekExpression = normalizeCronNameTokens(fieldOrWildcard(schedule.DayOfWeek), cronDayOfWeekNameValues)
    }

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

    dayOfWeekFieldMaximum := dayOfWeekMaximum
    if RunnerDialectKubernetes == resolvedDialect {
        dayOfWeekFieldMaximum = dayOfWeekMaximumKubernetes
    }

    dayOfWeek, dayOfWeekErr := parseCronField(dayOfWeekExpression, cronFieldBounds{name: "DayOfWeek", minimum: dayOfWeekMinimum, maximum: dayOfWeekFieldMaximum, dialect: resolvedDialect})
    if nil != dayOfWeekErr {
        return nil, dayOfWeekErr
    }

    if true == dayOfWeek.allowed[dayOfWeekMaximum] {
        dayOfWeek.allowed[dayOfWeekSunday] = true
    }

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

/* Matches reports whether the schedule fires at the given minute. Two restricted day-of-month / day-of-week fields fire when either matches — the classic Vixie-cron or, shared by both dialects; when either day field is unrestricted under the configured RunnerDialect (any star-based field in the crontab dialect, only the robfig star-bit shapes — the plain or the unit-stepped wildcard, alone or inside a list — in the kubernetes dialect) the two day fields combine with and, like the rest of the fields. */
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

func (instance *scheduleMatcher) fixedTime() bool {
    return false == instance.minute.starBased && false == instance.hour.starBased
}

type cronFieldBounds struct {
    name    string
    minimum int
    maximum int
    dialect RunnerDialect
}

func parseCronField(expression string, bounds cronFieldBounds) (cronFieldMatcher, error) {
    minimum := bounds.minimum
    maximum := bounds.maximum

    if "" == expression {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field is empty", bounds)
    }

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
