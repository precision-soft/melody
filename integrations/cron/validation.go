package cron

import (
    "fmt"
    "strconv"
    "strings"
    "unicode"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

/* crond reads three-letter names in the month and day-of-week fields; the bounds validation folds them onto their numbers so it reads the same schedule crond does. An unknown alphabetic token stays in place and fails the numeric parse. */
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

/* normalizeCronNameTokens folds a name only where the target schedulers read one — as a complete range endpoint. A name glued to digits ("1jan") or in step position (behind the slash) stays put and fails the numeric parse, exactly as crond and the robfig scheduler refuse it. */
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

/* Deprecated: use ForbiddenCharacter. */
type ForbiddenChar = ForbiddenCharacter

var CrontabForbiddenCharacters = []ForbiddenCharacter{
    {Char: '%', Reason: "reserved by crontab as a line-continuation character (translated to a newline before the shell sees it); remove it at the source"},
    {Char: '\n', Reason: "terminates the crontab line; a literal newline inside a token splits one entry into multiple invalid lines"},
    {Char: '\r', Reason: "terminates the crontab line on many cron daemons; remove it before passing the token to the generator"},
}

/* Deprecated: use CrontabForbiddenCharacters. */
var CrontabForbiddenChars = CrontabForbiddenCharacters

/* Deprecated: use ValidateNoForbiddenCharacters. */
func ValidateNoForbiddenChars(tokens []string, forbidden []ForbiddenCharacter, context string) error {
    return ValidateNoForbiddenCharacters(tokens, forbidden, context)
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

/* validateUserField reads the same whitespace rule the schedule fields read: any unicode space, not the ascii four. The user column sits on the generated line beside them, and crond splits that line on a vertical tab or a no-break space exactly as it splits on a plain space — so a user carrying one either fails the daemon's user lookup, and the entry silently never runs, or shifts the column boundary and hands part of the name to the shell as the command. */
func validateUserField(label string, value string) error {
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

/* steppedSingleValueItem reports the first list item that puts a step on a single value ("5/15"), the one shape no two target schedulers agree on: vixie crond rejects it as a bad field and refuses the entire crontab, taking every other entry in the file down with it; busybox crond accepts it; the robfig scheduler behind the k8s template reads it as the range from that value to the field maximum. The in-process matcher rejects it rather than pick a meaning, so the generator rejects it too — the explicit range the error names ("5-59/15") is read identically by all three. A step over a range or the wildcard is unambiguous and passes. */
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

/* exampleSteppedRangeOf renders the unambiguous rewrite the error suggests: the stepped item's own base and step spread over the field's range, which every target scheduler reads identically. */
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

func validateScheduleFields(entry Entry, forbidden []ForbiddenCharacter) error {
    if nil == entry.Schedule {
        return nil
    }

    fields := []struct {
        name    string
        value   string
        minimum int
        maximum int
        names   map[string]int
    }{
        {"Minute", entry.Schedule.Minute, minuteMinimum, minuteMaximum, nil},
        {"Hour", entry.Schedule.Hour, hourMinimum, hourMaximum, nil},
        {"DayOfMonth", entry.Schedule.DayOfMonth, dayOfMonthMinimum, dayOfMonthMaximum, nil},
        {"Month", entry.Schedule.Month, monthMinimum, monthMaximum, cronMonthNameValues},
        {"DayOfWeek", entry.Schedule.DayOfWeek, dayOfWeekMinimum, dayOfWeekMaximum, cronDayOfWeekNameValues},
    }

    for _, field := range fields {
        /* any unicode space counts, not just the ascii four: crond splits the line on a vertical tab or a no-break space the same way it splits on a plain one and then refuses the whole file with a parse error, dropping every entry in it — measured against vixie crond, which fails on each of the three alike. A leading or trailing space crond would tolerate; it is refused here because this rule and the in-process matcher's are one rule, and the matcher must not admit a schedule the generator cannot render. */
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

        /* the rendered field must parse under the bounds crond enforces: crond treats one bad field as a parse error and refuses the whole crontab file with it — so an out-of-range value fails generation instead. */
        /* the bounds here name no dialect on purpose: the generator judges every field against the crontab limits whatever runner dialect the same Configuration drives, so filling one in would name a chooser that never chose */
        fieldBounds := cronFieldBounds{name: field.name, minimum: field.minimum, maximum: field.maximum}

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
