package bag

import (
    bagcontract "github.com/precision-soft/melody/v2/bag/contract"
    "github.com/precision-soft/melody/v2/internal"
)

func StringMapStringString(parameterBag bagcontract.ParameterBag, name string) (map[string]string, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return nil, false, nil
    }

    return internal.MapStringString(value, name)
}
