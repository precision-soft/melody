package bag

import (
    "strings"

    bagcontract "github.com/precision-soft/melody/v3/bag/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func String(parameterBag bagcontract.ParameterBag, name string) (string, bool) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return "", false
    }

    /* a present nil value reports unset, as the typed accessors report it */
    if nil == value {
        return "", false
    }

    stringValue, isString := value.(string)
    if true == isString {
        return stringValue, true
    }

    /* a []string is a repeated key and answers its first value, as Input and url.Values.Get do; an empty list reports unset */
    if sliceValue, isSlice := value.([]string); true == isSlice {
        if 0 == len(sliceValue) {
            return "", false
        }

        return sliceValue[0], true
    }

    /* a value that is neither a string nor a string slice reports absent, so StringOrDefault substitutes its default */
    return "", false
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
