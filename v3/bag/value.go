package bag

import (
    "strings"

    bagcontract "github.com/precision-soft/melody/v3/bag/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func String(parameterBag bagcontract.ParameterBag, name string) (string, bool) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return "", false
    }

    /* a present-but-nil value reports as unset, matching what the typed accessors report for the same state; otherwise Has and String would agree while String and Int contradicted each other */
    if nil == value {
        return "", false
    }

    stringValue, isString := value.(string)
    if true == isString {
        return stringValue, true
    }

    /* a string slice read as one string is refused loudly, never guessed at: the request bags keep the single and the repeated key apart by type, so what lands here is a genuine array. The empty string would lose the value and one element would hide the rest; the slice is read with StringSlice or StringAt. */
    if _, isSlice := value.([]string); true == isSlice {
        exception.Panic(
            exception.NewError(
                "parameter holds a string slice and cannot be read as one string; read it with StringSlice or StringAt",
                exceptioncontract.Context{
                    "parameterName": name,
                },
                nil,
            ),
        )
    }

    return "", true
}

func StringOrDefault(parameterBag bagcontract.ParameterBag, name string, defaultValue string) string {
    value, exists := String(parameterBag, name)
    if false == exists {
        return defaultValue
    }

    return value
}

func HasNonEmptyString(parameterBag bagcontract.ParameterBag, name string) bool {
    value, exists := String(parameterBag, name)
    if false == exists {
        return false
    }

    if "" == strings.TrimSpace(value) {
        return false
    }

    return true
}

func Int(parameterBag bagcontract.ParameterBag, name string) (int64, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return 0, false, nil
    }

    return internal.Int(value, name)
}

func Bool(parameterBag bagcontract.ParameterBag, name string) (bool, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return false, false, nil
    }

    return internal.Bool(value, name)
}

func Float64(parameterBag bagcontract.ParameterBag, name string) (float64, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return 0, false, nil
    }

    return internal.Float64(value, name)
}
