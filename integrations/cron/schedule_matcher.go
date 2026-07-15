package cron

import (
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

/* scheduleMatcher evaluates a five-field cron schedule against a wall-clock time to the minute. It parses the same Schedule the generator renders, so one Configuration drives both the emitted manifests and the in-process runner; sub-minute cadences are not cron-expressible and are out of scope. */
type scheduleMatcher struct {
    minute     cronFieldMatcher
    hour       cronFieldMatcher
    dayOfMonth cronFieldMatcher
    month      cronFieldMatcher
    dayOfWeek  cronFieldMatcher
}

/* cronFieldMatcher is the set of values one field admits. wildcard records whether the field was an unrestricted "*", which the day-of-month / day-of-week rule below depends on. */
type cronFieldMatcher struct {
    allowed  map[int]bool
    wildcard bool
}

func (instance cronFieldMatcher) matches(value int) bool {
    return instance.allowed[value]
}

/* newScheduleMatcher parses a Schedule into a matcher; a nil schedule (or blank fields) is the every-minute wildcard, mirroring Schedule.Expression's nil handling and Schedule.Defaults. */
func newScheduleMatcher(schedule *Schedule) (*scheduleMatcher, error) {
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

    return &scheduleMatcher{
        minute:     minute,
        hour:       hour,
        dayOfMonth: dayOfMonth,
        month:      month,
        dayOfWeek:  dayOfWeek,
    }, nil
}

/* Matches reports whether the schedule fires at the given minute. When both the day-of-month and the day-of-week fields are restricted, the classic Vixie-cron rule applies and the entry fires when either matches; when one is the wildcard the two combine with the rest of the fields. */
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

    if true == instance.dayOfMonth.wildcard || true == instance.dayOfWeek.wildcard {
        return dayOfMonthMatches && dayOfWeekMatches
    }

    return dayOfMonthMatches || dayOfWeekMatches
}

/* parseCronField expands one field into the set of values it admits, bounded to [minimum, maximum]. It supports the wildcard, a stepped wildcard, single values, low-high ranges, stepped ranges and comma-separated lists of those. */
func parseCronField(expression string, minimum int, maximum int) (cronFieldMatcher, error) {
    trimmed := strings.TrimSpace(expression)
    if "" == trimmed {
        return cronFieldMatcher{}, invalidScheduleError(expression, "field is empty")
    }

    matcher := cronFieldMatcher{
        allowed:  make(map[int]bool),
        wildcard: "*" == trimmed,
    }

    for _, part := range strings.Split(trimmed, ",") {
        part = strings.TrimSpace(part)
        if "" == part {
            return cronFieldMatcher{}, invalidScheduleError(expression, "list contains an empty item")
        }

        rangeExpression := part
        step := 1

        if slashIndex := strings.Index(part, "/"); -1 != slashIndex {
            rangeExpression = part[:slashIndex]
            stepValue, stepErr := strconv.Atoi(part[slashIndex+1:])
            if nil != stepErr || 0 >= stepValue {
                return cronFieldMatcher{}, invalidScheduleError(expression, "step must be a positive integer")
            }
            step = stepValue
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
