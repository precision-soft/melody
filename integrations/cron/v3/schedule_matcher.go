package cron

import (
    "strconv"
    "strings"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

/* RunnerDialect selects how the in-process runner combines a schedule's day-of-month and day-of-week fields. Two genuinely restricted day fields always combine with or — the rule both target schedulers share — and the dialect decides only how a star-based day field (the plain or the stepped wildcard) is classified. The crontab dialect follows vixie crond, whose day-star flag reads just the field's first character: a star-based day field counts as unrestricted, so the day fields combine with and and only the restricted one constrains the day. The kubernetes dialect follows the robfig scheduler behind the k8s template, whose star bit survives only the unit step: the plain and the unit-stepped wildcard (alone or inside a list) are unrestricted, while a stepped wildcard with a step above one stays restricted and combines with or. The kubernetes dialect also bounds day of week at 6, as robfig does — a 7 would render a CronJob manifest the cluster rejects. The two real schedulers genuinely diverge on those shapes, so the dialect should name the scheduler the generated manifests target; every other matching rule is dialect-independent. The zero value selects the crontab dialect. */
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

/* cronFieldMatcher is the set of values one field admits. starUnrestricted records whether any list item is the wildcard with the unit step, alone or inside a list — exactly the shapes on which the robfig scheduler keeps its star bit, which the kubernetes day rule reads: a stepped wildcard with a step above one loses the bit and stays restricted. starBased records whether the expression begins with the wildcard, plain or stepped — the flag vixie cron reads both for the crontab day rule and when it classifies an entry for wall-clock reconciliation. */
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

    /* the robfig scheduler reads a whole-field "?" in a day field as the wildcard with its star bit intact (the Quartz convention the kubernetes template renders), so that dialect matches it as "*"; the crontab dialect keeps refusing it through the numeric parse, as crond has no "?". */
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

    /* the robfig scheduler bounds day of week at 6, so under the kubernetes dialect a schedule naming Sunday as 7 must fail here rather than render a CronJob manifest the cluster rejects. */
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

    /* two genuinely restricted day fields combine with or in every dialect; the dialect decides only whether any star-based day field counts as unrestricted (crontab) or just the star-bit shapes the robfig scheduler keeps do (the plain and the unit-stepped wildcard, alone or inside a list — kubernetes). */
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

/* fixedTime reports whether the schedule pins both the minute and the hour — neither is a plain or stepped wildcard. This is the vixie-cron entry classification the runner's wall-clock reconciliation reads: fixed-time entries are caught up or suppressed across a clock jump, wildcard entries simply follow the current minute. */
func (instance *scheduleMatcher) fixedTime() bool {
    return false == instance.minute.starBased && false == instance.hour.starBased
}

/* cronFieldBounds names the field being parsed and the limits it is judged against, so that a refusal can say which of the five positions failed and what it was measured with. The dialect belongs here because it is what chose the limits on the day-of-week field: "7" is a perfectly legal Sunday under the crontab dialect and out of range under kubernetes, and a refusal that names neither the field nor the dialect leaves the operator reading "value is out of range" about an expression that is not wrong anywhere else. It is left empty where no dialect chose the bounds — the busybox divergence check reads the crontab limits directly — rather than filled with a default that would name a chooser that never chose. */
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

    /* whitespace anywhere in a field — leading, trailing or embedded — is rejected rather than trimmed away, for two different reasons. Embedded whitespace is a correctness matter: the generated crontab line splits on it, so the field crond reads is not the field that was written, and it refuses the whole file with a parse error — every entry in it stops, not just this one. Any unicode space counts there, a vertical tab and a no-break space failing crond exactly as a plain space does. Leading and trailing whitespace crond would itself tolerate, but the generator refuses the field, and the two halves are one rule: repairing it here would admit a schedule that runs in-process yet cannot be generated. */
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

            /* a step wider than the field is clamped to the field's cardinality rather than rejected: crond accepts such a step and simply admits the range's low value alone (its expansion strides past the high bound on the first hop), so rejecting it would refuse a schedule the generator renders and crond runs. The clamp keeps the expansion loop below the overflow a step near the integer maximum would otherwise cause. */
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
                /* a step only makes sense over a range or the wildcard; classic cron rejects a step on a single value, so accepting one here would admit a schedule the generated crontab cannot run. */
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

/* parseCronNumber parses one field number as plain digits: strconv accepts a sign prefix ("+5"), which neither vixie crond nor the robfig scheduler does, so admitting one here would run a schedule the generated manifests cannot. */
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

/* invalidScheduleError names the field, the limits it was judged against and — where a dialect chose those limits — which dialect, beside the expression and the reason. The expression alone leaves an operator holding "value is out of range" about a schedule that is out of range in one position, under one dialect, for a reason none of the five words says: DayOfWeek "7" is a legal Sunday under the crontab dialect and refused under kubernetes, and only the record can tell the two apart. */
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
