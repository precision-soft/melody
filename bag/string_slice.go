package bag

import (
    "fmt"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"

    bagcontract "github.com/precision-soft/melody/bag/contract"
)

func appendStringTypeError(name string, value any) error {
    return exception.NewError(
        "parameter is not a string or string slice",
        exceptioncontract.Context{
            "parameterName": name,
            "actualType":    fmt.Sprintf("%T", value),
        },
        nil,
    )
}

func StringSlice(parameterBag bagcontract.ParameterBag, name string) ([]string, bool) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return nil, false
    }

    if nil == value {
        return nil, false
    }

    switch typedValue := value.(type) {
    case []string:
        copied := make([]string, len(typedValue))
        copy(copied, typedValue)

        return copied, true
    case string:
        return []string{typedValue}, true
    default:
        return nil, true
    }
}

func StringAt(parameterBag bagcontract.ParameterBag, name string, index int) (string, bool, error) {
    values, exists := StringSlice(parameterBag, name)
    if false == exists {
        return "", false, nil
    }

    if 0 > index {
        return "", true, exception.NewError(
            "index must be non negative",
            exceptioncontract.Context{
                "parameterName": name,
                "index":         index,
            },
            nil,
        )
    }

    if index >= len(values) {
        return "", true, exception.NewError(
            "index is out of range",
            exceptioncontract.Context{
                "parameterName": name,
                "index":         index,
                "length":        len(values),
            },
            nil,
        )
    }

    return values[index], true, nil
}

/* AppendString appends through the concrete bag's own critical section when it has one: the contract-level fallback reads and writes under two separate locks, and two concurrent appends through that window keep only one of the two values — a lost update no race detector reports, because each individual access is locked. */
func AppendString(parameterBag bagcontract.ParameterBag, name string, value string) error {
    concreteBag, isConcrete := parameterBag.(*ParameterBag)
    if true == isConcrete {
        return concreteBag.AppendString(name, value)
    }

    currentValue, exists := parameterBag.Get(name)
    if false == exists {
        parameterBag.Set(name, []string{value})
        return nil
    }

    if nil == currentValue {
        parameterBag.Set(name, []string{value})
        return nil
    }

    switch typedValue := currentValue.(type) {
    case []string:
        newValues := make([]string, 0, len(typedValue)+1)
        newValues = append(newValues, typedValue...)
        newValues = append(newValues, value)

        parameterBag.Set(name, newValues)

        return nil
    case string:
        parameterBag.Set(name, []string{typedValue, value})

        return nil
    default:
        return appendStringTypeError(name, currentValue)
    }
}

func AppendStringSlice(parameterBag bagcontract.ParameterBag, name string, values []string) error {
    for _, currentValue := range values {
        appendStringErr := AppendString(parameterBag, name, currentValue)
        if nil != appendStringErr {
            return appendStringErr
        }
    }

    return nil
}
