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
        /* a bare integer carries no unit, and reading it as nanoseconds — the Go-native interpretation — is almost never what the caller meant: the same value spelled as a string is refused by time.ParseDuration for missing its unit, so the numeric spelling is refused the same way instead of becoming a timeout that fires instantly */
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
        /* the three refusals carry three distinct causes: collapsed into one "is not a 'int'" they blamed the type, which was never the problem — the same branch accepts 2.0 — and the operator could not tell whether to change the encoding, the value or the magnitude */
        if true == math.IsNaN(typedValue) || true == math.IsInf(typedValue, 0) {
            return 0, true, ParseError(name, "int", typedValue, exception.NewError("value is not finite", nil, nil))
        }

        if typedValue != math.Trunc(typedValue) {
            return 0, true, ParseError(name, "int", typedValue, exception.NewError("value is not an integral number", nil, nil))
        }

        /* a float64 outside the int64 range converts to the "indefinite" value (-9223372036854775808) with no signal at all, so range-check before the conversion; the upper bound is written as a float because math.MaxInt64 is not representable as one */
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
        parsedValue, err := strconv.ParseFloat(strings.TrimSpace(typedValue), 64)
        if nil != err {
            return 0, true, ParseError(name, "float64", typedValue, err)
        }

        return refuseNonFinite(parsedValue, name)
    default:
        return 0, true, ParseError(name, "float64", value, nil)
    }
}

/* refuseNonFinite rejects NaN and the infinities on every branch of Float64: strconv.ParseFloat parses "NaN", "Inf" and "Infinity" without an error, and a NaN that slips through disarms every threshold written the normal way — each ordered comparison against it is false — so a guard like `ratio < 0 || ratio > 1` silently stops guarding. The sibling Int refuses the same shapes explicitly. */
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
        /* a typed-nil map reads as the absence it is: reported as present it came back as an empty non-nil map, so a caller branching on the presence flag to apply a default silently got the empty map instead — the strict accessors beside this one already answer absent for a nil value */
        if nil == typedValue {
            return nil, false, nil
        }

        return CopyStringMap[string](typedValue), true, nil
    default:
        return nil, true, ParseError(name, "map[string]string", value, nil)
    }
}
