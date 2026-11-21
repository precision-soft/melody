package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* TtlRule accepts a Save ttl of at least 1s, or unlimited; it has no default, so ResolveTtl refuses an absent ttl. */
var TtlRule = bound.Rule[time.Duration]{
    Name:              "ttl",
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

/* ResolveTtl decides a Save ttl against TtlRule and answers the expiry to apply, 0 for none. */
func ResolveTtl(ttl bound.Duration) (time.Duration, error) {
    if true == ttl.IsAbsent() {
        return 0, exception.NewError(
            "the session ttl is required",
            exceptioncontract.Context{
                "field": TtlRule.Name,
            },
            nil,
        )
    }

    resolved, err := bound.Resolve(ttl, TtlRule)
    if nil != err {
        return 0, err
    }

    if true == resolved.Unlimited {
        return 0, nil
    }

    return resolved.Value, nil
}
