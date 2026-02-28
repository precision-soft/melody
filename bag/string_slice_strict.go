package bag

import (
    bagcontract "github.com/precision-soft/melody/bag/contract"
    "github.com/precision-soft/melody/internal"
)

func StringSliceStrict(parameterBag bagcontract.ParameterBag, name string) ([]string, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return nil, false, nil
    }

    if nil == value {
        return nil, true, nil
    }

    switch typedValue := value.(type) {
    case []string:
        copied := make([]string, len(typedValue))
        copy(copied, typedValue)

        return copied, true, nil
    case string:
        return []string{typedValue}, true, nil
    default:
        return nil, true, internal.ParseError(name, "stringSlice", value, nil)
    }
}
