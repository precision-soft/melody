package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* TtlRule is the reading every backend gives the ttl of a write: a value of at least 1s, or unlimited for an entry kept until it is deleted or evicted. It has no default, since a cached value declares how long it may be served, so ResolveTtl refuses an absent ttl before the rule is consulted. */
var TtlRule = bound.Rule[time.Duration]{
    Name:              "ttl",
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

/* ResolveTtl decides the ttl of a write against TtlRule and answers the expiry to apply, 0 for an entry with none; an absent ttl and a value below the minimum are refused, naming the ttl. */
func ResolveTtl(ttl bound.Duration) (time.Duration, error) {
    if true == ttl.IsAbsent() {
        return 0, exception.NewError(
            "the cache ttl is required",
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
