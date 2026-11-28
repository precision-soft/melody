package clock

import (
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* ResolveOptionalClock answers the clock handed, the system clock when none is; a typed nil is refused as "<component> clock is a typed nil", naming field. */
func ResolveOptionalClock(clockInstance clockcontract.Clock, component string, field string) (clockcontract.Clock, error) {
    if nil == clockInstance {
        return NewSystemClock(), nil
    }

    if true == internal.IsNilInterface(clockInstance) {
        return nil, exception.NewError(
            component+" clock is a typed nil",
            exceptioncontract.Context{
                "field": field,
            },
            nil,
        )
    }

    return clockInstance, nil
}
