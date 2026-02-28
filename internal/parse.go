package internal

import (
    "math"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/exception"
)

func Duration(value any, name string) (time.Duration, bool, error) {
    if nil == value {
        return 0, false, nil
    }

    switch typedValue := value.(type) {
    case time.Duration:
        return typedValue, true, nil
    case int64:
        return time.Duration(typedValue), true, nil
    case int:
        return time.Duration(typedValue), true, nil
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
        if typedValue != math.Trunc(typedValue) {
            return 0, true, ParseError(name, "int", typedValue, nil)
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
        return typedValue, true, nil
    case float32:
        return float64(typedValue), true, nil
    case int:
        return float64(typedValue), true, nil
    case string:
        parsedValue, err := strconv.ParseFloat(strings.TrimSpace(typedValue), 64)
        if nil != err {
            return 0, true, ParseError(name, "float64", typedValue, err)
        }

        return parsedValue, true, nil
    default:
        return 0, true, ParseError(name, "float64", value, nil)
    }
}

func MapStringString(value any, name string) (map[string]string, bool, error) {
    if nil == value {
        return nil, false, nil
    }

    switch typedValue := value.(type) {
    case map[string]string:
        return CopyStringMap[string](typedValue), true, nil
    default:
        return nil, true, ParseError(name, "map[string]string", value, nil)
    }
}
