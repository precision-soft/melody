package bag

import (
    "time"

    bagcontract "github.com/precision-soft/melody/v4/bag/contract"
    "github.com/precision-soft/melody/v4/internal"
)

func Duration(parameterBag bagcontract.ParameterBag, name string) (time.Duration, bool, error) {
    value, exists := parameterBag.Get(name)
    if false == exists {
        return 0, false, nil
    }

    return internal.Duration(value, name)
}
