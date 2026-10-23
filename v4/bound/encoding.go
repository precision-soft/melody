package bound

import (
    "bytes"
    "encoding/json"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* unlimitedText is the one spelling of unlimited in configuration, environment, json and yaml. */
const unlimitedText = "unlimited"

/* MarshalText writes an absent limit as the empty text, unlimited as "unlimited" and a value in the form UnmarshalText reads back. */
func (instance Limit[T]) MarshalText() ([]byte, error) {
    switch instance.state {
    case limitStateUnlimited:
        return []byte(unlimitedText), nil
    case limitStateValue:
        return []byte(formatUnit(instance.value)), nil
    }

    return []byte{}, nil
}

/* UnmarshalText trims the surrounding whitespace, then reads the empty text as absent, "unlimited" as unlimited, and otherwise a value: a duration in time.ParseDuration's form, a count as strconv.Atoi reads it. Any other text is refused. The value is judged against the field's minimum when the component resolves it, not here. */
func (instance *Limit[T]) UnmarshalText(text []byte) error {
    trimmed := strings.TrimSpace(string(text))

    if "" == trimmed {
        *instance = Limit[T]{}

        return nil
    }

    if unlimitedText == trimmed {
        *instance = Limit[T]{state: limitStateUnlimited}

        return nil
    }

    value, err := parseUnit[T](trimmed)
    if nil != err {
        return err
    }

    *instance = Of(value)

    return nil
}

/* MarshalJSON writes an absent limit as null, unlimited as "unlimited", a duration as its text and a count as a number. */
func (instance Limit[T]) MarshalJSON() ([]byte, error) {
    switch instance.state {
    case limitStateUnlimited:
        return json.Marshal(unlimitedText)
    case limitStateValue:
        if count, ok := any(instance.value).(int); true == ok {
            return json.Marshal(count)
        }

        return json.Marshal(formatUnit(instance.value))
    }

    return []byte("null"), nil
}

/* UnmarshalJSON reads null as absent and a string as UnmarshalText does. A count also reads a json number; a duration does not, since a bare number names no unit. */
func (instance *Limit[T]) UnmarshalJSON(data []byte) error {
    trimmed := bytes.TrimSpace(data)

    if "null" == string(trimmed) {
        *instance = Limit[T]{}

        return nil
    }

    if 0 < len(trimmed) && '"' == trimmed[0] {
        var text string
        if err := json.Unmarshal(trimmed, &text); nil != err {
            return err
        }

        return instance.UnmarshalText([]byte(text))
    }

    var zero T
    if _, isCount := any(zero).(int); false == isCount {
        return exception.NewError(
            "a duration limit is written as a string, such as \"30s\" or \"unlimited\"",
            exceptioncontract.Context{
                "value": string(trimmed),
            },
            nil,
        )
    }

    return instance.UnmarshalText(trimmed)
}

func parseUnit[T Unit](text string) (T, error) {
    var zero T

    switch any(zero).(type) {
    case time.Duration:
        parsed, err := time.ParseDuration(text)
        if nil != err {
            return zero, exception.NewError(
                "the duration limit does not parse",
                exceptioncontract.Context{
                    "value": text,
                },
                err,
            )
        }

        return T(parsed), nil
    case int:
        parsed, err := strconv.Atoi(text)
        if nil != err {
            return zero, exception.NewError(
                "the count limit does not parse",
                exceptioncontract.Context{
                    "value": text,
                },
                err,
            )
        }

        return T(parsed), nil
    }

    return zero, nil
}
