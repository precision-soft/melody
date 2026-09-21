package bag

import (
    "strings"

    bagcontract "github.com/precision-soft/melody/bag/contract"
    "github.com/precision-soft/melody/internal"
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

    /* the request bags keep the single and the repeated key apart by type, so a []string landing here is a genuinely repeated key. It answers its FIRST value, the way Input and url.Values.Get answer a repeated key: the shape of a request parameter is chosen by the client, and a refusal here turned every documented read through StringOrDefault or HasNonEmptyString into a panic — a 500 an unauthenticated client could raise with one duplicated query key. The whole list is read with StringSlice or StringAt; an empty list is a key with no value, reported unset like nil. */
    if sliceValue, isSlice := value.([]string); true == isSlice {
        if 0 == len(sliceValue) {
            return "", false
        }

        return sliceValue[0], true
    }

    /* a present value that is neither a string nor a string slice — an int, a bool, a float — reports absent rather than present-but-empty: returning ("", true) defeated StringOrDefault, which substitutes the default only when the value is absent, so an int parameter read through it came back "" instead of the default. Absent is the honest answer for "there is no string here", and it restores the default-fallback contract the sibling accessors keep. */
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
