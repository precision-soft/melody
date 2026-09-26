package internal

import (
    "math"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func Duration(value any, name string) (time.Duration, bool, error) {
    if nil == value {
        return 0, false, nil
    }

    switch typedValue := value.(type) {
    case time.Duration:
        return typedValue, true, nil
    case int64, int:
        /* a bare integer carries no unit and is refused, as time.ParseDuration refuses the same value spelled as a string */
        return 0, true, ParseError(
            name,
            "duration",
            value,
            exception.NewError("a bare integer carries no unit", map[string]any{"hint": "pass a time.Duration or a string with a unit, for example \"30s\""}, nil),
        )
    case string:
        trimmedValue := strings.TrimSpace(typedValue)
        parsedValue, err := time.ParseDuration(trimmedValue)
        if nil != err {
            return 0, true, ParseError(name, "duration", typedValue, err)
        }

        return parsedValue, true, nil
    default:
        return 0, true, ParseError(name, "duration", value, nil)
    }
}

func Int(value any, name string) (int64, bool, error) {
    if nil == value {
        return 0, false, nil
    }

    switch typedValue := value.(type) {
    case int:
        return int64(typedValue), true, nil
    case int64:
        return typedValue, true, nil
    case float64:
        if true == math.IsNaN(typedValue) || true == math.IsInf(typedValue, 0) {
            return 0, true, ParseError(name, "int", typedValue, exception.NewError("value is not finite", nil, nil))
        }

        if typedValue != math.Trunc(typedValue) {
            return 0, true, ParseError(name, "int", typedValue, exception.NewError("value is not an integral number", nil, nil))
        }

        /* a float64 outside the int64 range converts to the indefinite value with no signal, so the range is checked first */
        if typedValue < math.MinInt64 || typedValue >= 9223372036854775808.0 {
            return 0, true, ParseError(name, "int", typedValue, exception.NewError("value is outside the int64 range", nil, nil))
        }

        return int64(typedValue), true, nil
    case string:
        parsedValue, err := strconv.ParseInt(strings.TrimSpace(typedValue), 10, 64)
        if nil != err {
            return 0, true, ParseError(name, "int", typedValue, err)
        }

        return parsedValue, true, nil
    default:
        return 0, true, ParseError(name, "int", value, nil)
    }
}

func Bool(value any, name string) (bool, bool, error) {
    if nil == value {
        return false, false, nil
    }

    switch typedValue := value.(type) {
    case bool:
        return typedValue, true, nil
    case string:
        parsedValue, err := BoolFromString(typedValue)
        if nil != err {
            return false, true, ParseError(name, "bool", typedValue, err)
        }

        return parsedValue, true, nil
    default:
        return false, true, ParseError(name, "bool", value, nil)
    }
}

func BoolFromString(value string) (bool, error) {
    lower := strings.ToLower(strings.TrimSpace(value))

    switch lower {
    case "1", "true", "yes", "y", "on":
        return true, nil

    case "0", "false", "no", "n", "off":
        return false, nil
    }

    return false, exception.NewError("cannot parse as bool", map[string]any{"value": value}, nil)
}

func Float64(value any, name string) (float64, bool, error) {
    if nil == value {
        return 0, false, nil
    }

    switch typedValue := value.(type) {
    case float64:
        return refuseNonFinite(typedValue, name)
    case float32:
        return refuseNonFinite(float64(typedValue), name)
    case int:
        return float64(typedValue), true, nil
    case int64:
        return float64(typedValue), true, nil
    case string:
        trimmedValue := strings.TrimSpace(typedValue)
        if false == isDecimalFloatSpelling(trimmedValue) {
            return 0, true, ParseError(
                name,
                "float64",
                typedValue,
                exception.NewError("the float grammar is decimal: an optional sign, digits and at most one decimal point", nil, nil),
            )
        }

        parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
        if nil != err {
            return 0, true, ParseError(name, "float64", typedValue, err)
        }

        return refuseNonFinite(parsedValue, name)
    default:
        return 0, true, ParseError(name, "float64", value, nil)
    }
}

/* isDecimalFloatSpelling admits a plain decimal only — an optional sign, digits and at most one decimal point — where strconv.ParseFloat also reads underscores, hexadecimal floats and exponents. */
func isDecimalFloatSpelling(value string) bool {
    if "" == value {
        return false
    }

    remainder := value
    if '+' == remainder[0] || '-' == remainder[0] {
        remainder = remainder[1:]
    }

    digitSeen := false
    pointSeen := false

    for _, character := range remainder {
        if '0' <= character && '9' >= character {
            digitSeen = true

            continue
        }

        if '.' == character {
            if true == pointSeen {
                return false
            }

            pointSeen = true

            continue
        }

        return false
    }

    return digitSeen
}

/* refuseNonFinite rejects NaN and the infinities, which strconv.ParseFloat parses without an error and which disarm every ordered comparison. */
func refuseNonFinite(value float64, name string) (float64, bool, error) {
    if true == math.IsNaN(value) || true == math.IsInf(value, 0) {
        return 0, true, ParseError(name, "float64", value, exception.NewError("value is not finite", nil, nil))
    }

    return value, true, nil
}

func MapStringString(value any, name string) (map[string]string, bool, error) {
    if nil == value {
        return nil, false, nil
    }

    switch typedValue := value.(type) {
    case map[string]string:
        /* a typed-nil map reads as absent, as the strict accessors answer it */
        if nil == typedValue {
            return nil, false, nil
        }

        return CopyStringMap[string](typedValue), true, nil
    default:
        return nil, true, ParseError(name, "map[string]string", value, nil)
    }
}
