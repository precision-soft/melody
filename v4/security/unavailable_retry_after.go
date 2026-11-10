package security

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

const (
    defaultUnavailableRetryAfter = 5 * time.Second
    maximumUnavailableRetryAfter = time.Hour
)

/* ResolveUnavailableRetryAfter decides the Retry-After delay a firewall answers an unavailable token with: absent is 5s, the value lies within 1s and 1h, is a whole number of seconds since the header carries whole seconds, and unlimited is refused; every refusal names field. */
func ResolveUnavailableRetryAfter(limit bound.Duration, field string) (time.Duration, error) {
    resolved, resolveErr := bound.Resolve(
        limit,
        bound.Rule[time.Duration]{
            Name:    field,
            Default: defaultUnavailableRetryAfter,
            Minimum: bound.TtlMinimum,
            Maximum: maximumUnavailableRetryAfter,
        },
    )
    if nil != resolveErr {
        return 0, resolveErr
    }

    if 0 != resolved.Value%time.Second {
        return 0, exception.NewError(
            "the limit is not a whole number of seconds, and Retry-After is served in whole seconds",
            exceptioncontract.Context{
                "field": field,
                "value": resolved.Value.String(),
            },
            nil,
        )
    }

    return resolved.Value, nil
}
