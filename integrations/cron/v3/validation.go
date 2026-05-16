package cron

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

type ForbiddenChar struct {
    Char   rune
    Reason string
}

var CrontabForbiddenChars = []ForbiddenChar{
    {Char: '%', Reason: "reserved by crontab as a line-continuation character (translated to a newline before the shell sees it); remove it at the source"},
    {Char: '\n', Reason: "terminates the crontab line; a literal newline inside a token splits one entry into multiple invalid lines"},
    {Char: '\r', Reason: "terminates the crontab line on many cron daemons; remove it before passing the token to the generator"},
}

func ValidateNoForbiddenChars(tokens []string, forbidden []ForbiddenChar, context string) error {
    for _, token := range tokens {
        for _, char := range forbidden {
            if true == strings.ContainsRune(token, char.Char) {
                return exception.NewError(
                    fmt.Sprintf("cron: token %q in %s contains forbidden character %q: %s", token, context, char.Char, char.Reason),
                    exceptioncontract.Context{
                        "token":     token,
                        "context":   context,
                        "character": string(char.Char),
                        "reason":    char.Reason,
                    },
                    ErrForbiddenCharacter,
                )
            }
        }
    }

    return nil
}

func validateUserField(label string, value string) error {
    if true == strings.ContainsAny(value, " \t\n\r") {
        return exception.NewError(
            fmt.Sprintf("cron: %s %q contains whitespace; user fields must be single tokens", label, value),
            exceptioncontract.Context{
                "field": label,
                "value": value,
            },
            ErrFieldContainsWhitespace,
        )
    }

    return nil
}

func validateScheduleFields(entry Entry) error {
    if nil == entry.Schedule {
        return nil
    }

    fields := []struct {
        name  string
        value string
    }{
        {"Minute", entry.Schedule.Minute},
        {"Hour", entry.Schedule.Hour},
        {"DayOfMonth", entry.Schedule.DayOfMonth},
        {"Month", entry.Schedule.Month},
        {"DayOfWeek", entry.Schedule.DayOfWeek},
    }

    for _, field := range fields {
        if true == strings.ContainsAny(field.value, " \t\n\r") {
            return exception.NewError(
                fmt.Sprintf("cron: entry %q has whitespace in Schedule.%s (%q); crontab fields must be single tokens", entry.Name, field.name, field.value),
                exceptioncontract.Context{
                    "entry": entry.Name,
                    "field": field.name,
                    "value": field.value,
                },
                ErrFieldContainsWhitespace,
            )
        }
    }

    return nil
}
