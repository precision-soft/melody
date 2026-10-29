package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* NonceTtlRule is the reading every guard gives the ttl of a Remember: the residual lifetime of the envelope or the code, at least 1ms, the clock's resolution, rather than the 1s of a stored ttl, since an envelope presented in its last second is still guarded. It has no default and no unlimited: a nonce that is not recorded is replayable, and one recorded forever is a guard that only grows. */
var NonceTtlRule = bound.Rule[time.Duration]{
    Name:    "NonceGuard.Remember.ttl",
    Minimum: bound.TimeoutMinimum,
}

/* ResolveNonceTtl decides the ttl of a Remember against NonceTtlRule; an absent ttl, a value below the minimum and unlimited are refused, naming the ttl. */
func ResolveNonceTtl(ttl bound.Duration) (time.Duration, error) {
    if true == ttl.IsAbsent() {
        return 0, exception.NewError(
            "the nonce ttl is required",
            exceptioncontract.Context{
                "field": NonceTtlRule.Name,
            },
            nil,
        )
    }

    resolved, err := bound.Resolve(ttl, NonceTtlRule)
    if nil != err {
        return 0, err
    }

    return resolved.Value, nil
}

/* NonceGuard provides replay protection for single-use values (the nonce carried by the internal-auth envelope). Remember atomically records a nonce for the ttl, read against NonceTtlRule, and reports whether it had already been recorded and not yet expired; a true result means the nonce is a replay and the request must be rejected; a refused ttl is answered as the error. Implementations are expected to be safe for concurrent use and, in a multi-instance deployment, to share state (for example through Redis). */
type NonceGuard interface {
    Remember(runtimeInstance runtimecontract.Runtime, nonce string, ttl bound.Duration) (bool, error)
}
