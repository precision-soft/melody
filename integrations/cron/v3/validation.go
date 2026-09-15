package cron

import (
    "fmt"
    "strconv"
    "strings"
    "unicode"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

var cronMonthNameValues = map[string]int{
    "jan": 1,
    "feb": 2,
    "mar": 3,
    "apr": 4,
    "may": 5,
    "jun": 6,
    "jul": 7,
    "aug": 8,
    "sep": 9,
    "oct": 10,
    "nov": 11,
    "dec": 12,
}

var cronDayOfWeekNameValues = map[string]int{
    "sun": 0,
    "mon": 1,
    "tue": 2,
    "wed": 3,
    "thu": 4,
    "fri": 5,
    "sat": 6,
}

func normalizeCronNameTokens(value string, nameValues map[string]int) string {
    if nil == nameValues {
        return value
    }

    items := strings.Split(value, ",")
    for itemIndex, item := range items {
        rangePart := item
        stepPart := ""

        if slashIndex := strings.Index(item, "/"); -1 != slashIndex {
            rangePart = item[:slashIndex]
            stepPart = item[slashIndex:]
        }

        bounds := strings.SplitN(rangePart, "-", 2)
        for boundIndex, bound := range bounds {
            number, known := nameValues[strings.ToLower(bound)]
            if true == known {
                bounds[boundIndex] = strconv.Itoa(number)
            }
        }

        items[itemIndex] = strings.Join(bounds, "-") + stepPart
    }

    return strings.Join(items, ",")
}

type ForbiddenCharacter struct {
    Char   rune
    Reason string
}

var CrontabForbiddenCharacters = []ForbiddenCharacter{
    {Char: '%', Reason: "reserved by crontab as a line-continuation character (translated to a newline before the shell sees it); remove it at the source"},
    {Char: '\n', Reason: "terminates the crontab line; a literal newline inside a token splits one entry into multiple invalid lines"},
    {Char: '\r', Reason: "terminates the crontab line on many cron daemons; remove it before passing the token to the generator"},
}

func ValidateNoForbiddenCharacters(tokens []string, forbidden []ForbiddenCharacter, context string) error {
    for _, token := range tokens {
        for _, character := range forbidden {
            if true == strings.ContainsRune(token, character.Char) {
                return exception.NewError(
                    fmt.Sprintf("cron: token %q in %s contains forbidden character %q: %s", token, context, character.Char, character.Reason),
                    exceptioncontract.Context{
                        "token":     token,
                        "context":   context,
                        "character": string(character.Char),
                        "reason":    character.Reason,
                    },
                    ErrForbiddenCharacter,
                )
            }
        }
    }

    return nil
}

/* ValidateUserField reads the same whitespace rule the schedule fields read: any unicode space, not the ascii four. The user column sits on the generated line beside them, and crond splits that line on a vertical tab or a no-break space exactly as it splits on a plain space — so a user carrying one either fails the daemon's user lookup, and the entry silently never runs, or shifts the column boundary and hands part of the name to the shell as the command. The value is then held to CrontabForbiddenCharacters. label names the field in the refusal. It is exported for a custom template whose dialect places the user on a crontab line beside the schedule — ansible.builtin.cron does — so that dialect refuses the user the builtin crontab dialect refuses. */
func ValidateUserField(label string, value string) error {
    if -1 != strings.IndexFunc(value, unicode.IsSpace) {
        return exception.NewError(
            fmt.Sprintf("cron: %s %q contains whitespace; user fields must be single tokens", label, value),
            exceptioncontract.Context{
                "field": label,
                "value": value,
            },
            ErrFieldContainsWhitespace,
        )
    }

    return ValidateNoForbiddenCharacters(
        []string{value},
        CrontabForbiddenCharacters,
        label,
    )
}

func steppedSingleValueItem(expression string) (string, bool) {
    for _, item := range strings.Split(expression, ",") {
        slashIndex := strings.Index(item, "/")
        if -1 == slashIndex {
            continue
        }

        base := item[:slashIndex]
        if "*" == base || true == strings.Contains(base, "-") {
            continue
        }

        return item, true
    }

    return "", false
}

func exampleSteppedRangeOf(item string, fieldName string) string {
    slashIndex := strings.Index(item, "/")
    if -1 == slashIndex {
        return item
    }

    maximums := map[string]string{
        "Minute":     "59",
        "Hour":       "23",
        "DayOfMonth": "31",
        "Month":      "12",
        "DayOfWeek":  "6",
    }

    maximum, known := maximums[fieldName]
    if false == known {
        return item
    }

    return item[:slashIndex] + "-" + maximum + item[slashIndex:]
}

func busyboxDayFieldsDiverge(dayOfMonthExpression string, dayOfWeekExpression string) bool {
    dayOfMonth, dayOfMonthErr := parseCronField(dayOfMonthExpression, cronFieldBounds{name: "DayOfMonth", minimum: dayOfMonthMinimum, maximum: dayOfMonthMaximum})
    if nil != dayOfMonthErr {
        return false
    }

    dayOfWeek, dayOfWeekErr := parseCronField(dayOfWeekExpression, cronFieldBounds{name: "DayOfWeek", minimum: dayOfWeekMinimum, maximum: dayOfWeekMaximum})
    if nil != dayOfWeekErr {
        return false
    }

    if true == dayOfWeek.allowed[dayOfWeekMaximum] {
        dayOfWeek.allowed[dayOfWeekSunday] = true
    }

    dayOfMonthUnusedForBusybox := fieldCoversWholeRange(dayOfMonth, dayOfMonthMinimum, dayOfMonthMaximum)
    dayOfWeekUnusedForBusybox := fieldCoversWholeRange(dayOfWeek, dayOfWeekSunday, dayOfWeekMaximumKubernetes)

    vixieCombinesWithOr := false == dayOfMonth.starBased && false == dayOfWeek.starBased

    for dayValue := dayOfMonthMinimum; dayValue <= dayOfMonthMaximum; dayValue++ {
        for weekdayValue := dayOfWeekSunday; weekdayValue <= dayOfWeekMaximumKubernetes; weekdayValue++ {
            dayMatches := dayOfMonth.allowed[dayValue]
            weekdayMatches := dayOfWeek.allowed[weekdayValue]

            vixieFires := dayMatches && weekdayMatches
            if true == vixieCombinesWithOr {
                vixieFires = dayMatches || weekdayMatches
            }

            busyboxFires := busyboxDayDecision(dayMatches, dayOfMonthUnusedForBusybox, weekdayMatches, dayOfWeekUnusedForBusybox)

            if vixieFires != busyboxFires {
                return true
            }
        }
    }

    return false
}

func fieldCoversWholeRange(field cronFieldMatcher, minimum int, maximum int) bool {
    for value := minimum; value <= maximum; value = value + 1 {
        if false == field.allowed[value] {
            return false
        }
    }

    return true
}

func busyboxDayDecision(dayMatches bool, dayUnused bool, weekdayMatches bool, weekdayUnused bool) bool {
    if true == dayUnused && true == weekdayUnused {
        return true
    }

    if true == dayUnused {
        return weekdayMatches
    }

    if true == weekdayUnused {
        return dayMatches
    }

    return dayMatches || weekdayMatches
}

/* ValidateScheduleFields refuses a schedule the scheduler behind dialect would read differently from the in-process matcher, or not at all: whitespace inside a field, a character of forbidden, a step on a single value, and a field outside the dialect's bounds — names folded onto their numbers first, and "?" read as the wildcard only under the kubernetes dialect. A nil Schedule is the wildcard on every field and passes. The builtin dialects call it before rendering anything; a custom template that writes the five fields into a crontab — ansible.builtin.cron does — calls it with CrontabForbiddenCharacters and RunnerDialectCrontab, so a field carrying a space cannot become a second crontab line and a field carrying % cannot end the line early. */
func ValidateScheduleFields(entry Entry, forbidden []ForbiddenCharacter, dialect RunnerDialect) error {
    if nil == entry.Schedule {
        return nil
    }

    dayOfWeekFieldMaximum := dayOfWeekMaximum
    if RunnerDialectKubernetes == dialect {
        dayOfWeekFieldMaximum = dayOfWeekMaximumKubernetes
    }

    questionMarkIsWildcard := RunnerDialectKubernetes == dialect

    fields := []struct {
        name               string
        value              string
        minimum            int
        maximum            int
        names              map[string]int
        allowsQuestionMark bool
    }{
        {"Minute", entry.Schedule.Minute, minuteMinimum, minuteMaximum, nil, false},
        {"Hour", entry.Schedule.Hour, hourMinimum, hourMaximum, nil, false},
        {"DayOfMonth", entry.Schedule.DayOfMonth, dayOfMonthMinimum, dayOfMonthMaximum, nil, questionMarkIsWildcard},
        {"Month", entry.Schedule.Month, monthMinimum, monthMaximum, cronMonthNameValues, false},
        {"DayOfWeek", entry.Schedule.DayOfWeek, dayOfWeekMinimum, dayOfWeekFieldMaximum, cronDayOfWeekNameValues, questionMarkIsWildcard},
    }

    for _, field := range fields {

        if -1 != strings.IndexFunc(field.value, unicode.IsSpace) {
            return exception.NewError(
                fmt.Sprintf("cron: entry %q has whitespace in Schedule.%s (%q); schedule fields must be single tokens", entry.Name, field.name, field.value),
                exceptioncontract.Context{
                    "entry": entry.Name,
                    "field": field.name,
                    "value": field.value,
                },
                ErrFieldContainsWhitespace,
            )
        }

        if steppedItem, stepped := steppedSingleValueItem(field.value); true == stepped {
            return exception.NewError(
                fmt.Sprintf(
                    "cron: entry %q steps a single value in Schedule.%s (%q); the target schedulers do not agree on that shape — vixie crond rejects %q outright as a bad field and refuses the whole crontab with it, busybox crond accepts it, and the robfig scheduler behind the k8s template reads it as the whole range from that value up — so write the range you mean explicitly (for example %q instead of %q), which every one of them reads the same way",
                    entry.Name,
                    field.name,
                    field.value,
                    steppedItem,
                    exampleSteppedRangeOf(steppedItem, field.name),
                    steppedItem,
                ),
                exceptioncontract.Context{
                    "entry": entry.Name,
                    "field": field.name,
                    "value": field.value,
                    "item":  steppedItem,
                },
                ErrSteppedSingleValue,
            )
        }

        forbiddenErr := ValidateNoForbiddenCharacters(
            []string{field.value},
            forbidden,
            fmt.Sprintf("entry %q Schedule.%s", entry.Name, field.name),
        )
        if nil != forbiddenErr {
            return forbiddenErr
        }

        if true == field.allowsQuestionMark && "?" == field.value {
            continue
        }

        fieldBounds := cronFieldBounds{name: field.name, minimum: field.minimum, maximum: field.maximum, dialect: dialect}

        if _, parseErr := parseCronField(fieldOrWildcard(normalizeCronNameTokens(field.value, field.names)), fieldBounds); nil != parseErr {
            return exception.NewError(
                fmt.Sprintf("cron: entry %q has an invalid Schedule.%s (%q)", entry.Name, field.name, field.value),
                exceptioncontract.Context{
                    "entry": entry.Name,
                    "field": field.name,
                    "value": field.value,
                },
                parseErr,
            )
        }
    }

    return nil
}
