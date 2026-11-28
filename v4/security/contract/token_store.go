package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

type TokenStore interface {
    Lookup(runtimeInstance containercontract.Runtime, tokenString string) (Claims, bool, error)
}

/* TokenTtlRule is the reading every token store gives the ttl of a Put: a value of at least 1s, or unlimited for a token kept until it is deleted or revoked. It has no default, since a stored token declares how long it is honoured, so ResolveTokenTtl refuses an absent ttl before the rule is consulted. */
func TokenTtlRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:              "RevocableTokenStore.Put.ttl",
        Minimum:           bound.TtlMinimum,
        UnlimitedAdmitted: true,
    }
}

/* ResolveTokenTtl decides the ttl of a Put against TokenTtlRule and answers the expiry to apply, 0 for a token with none; an absent ttl and a value below the minimum are refused, naming the ttl. */
func ResolveTokenTtl(ttl bound.Duration) (time.Duration, error) {
    if true == ttl.IsAbsent() {
        return 0, exception.NewError(
            "the token ttl is required",
            exceptioncontract.Context{
                "field": TokenTtlRule().Name,
            },
            nil,
        )
    }

    resolved, err := bound.Resolve(ttl, TokenTtlRule())
    if nil != err {
        return 0, err
    }

    if true == resolved.Unlimited {
        return 0, nil
    }

    return resolved.Value, nil
}

/* RevocableTokenStore stores tokens that can be individually withdrawn. Every door takes the caller's runtime first and refuses a nil one; a store that could not answer returns an error carrying exception.MarkInfrastructureFailure, an argument the door refuses an unmarked one. Every door runs under the runtime's context and refuses one already done with an error carrying it, marked, so a compensation that must outlive the request, a revocation or the withdraw of a token just minted, is called on a runtime whose context is detached from the request's cancellation by context.WithoutCancel, through a security.BoundTokenStore bound to that runtime. A user identifier and a device identifier must be valid UTF-8 and a revocation instant must lie between 1970 and 2262, the limits of the redis encoding every implementation applies. Put stores the claims under the ttl, read against TokenTtlRule: an absent ttl and one below the minimum are refused by every implementation, since a lapsed remaining lifetime must not become forever, and unlimited stores the token with no expiry. DeleteByUser and PurgeExpired answer what they removed before a failure beside the error. */
type RevocableTokenStore interface {
    TokenStore
    Put(runtimeInstance containercontract.Runtime, tokenString string, claims Claims, ttl bound.Duration) error
    Delete(runtimeInstance containercontract.Runtime, tokenString string) error
    DeleteByUser(runtimeInstance containercontract.Runtime, userIdentifier string) (int, error)
    PurgeExpired(runtimeInstance containercontract.Runtime) (int, error)
}

/* RevocationOptions scopes a revocation boundary beside its user. */
type RevocationOptions struct {
    /* DeviceIdentifier names one device of the user, valid UTF-8 as the user is; left out, the boundary is the whole user's. */
    DeviceIdentifier string
}

type RevocationEpochStore interface {
    RevokeBefore(runtimeInstance containercontract.Runtime, userIdentifier string, instant time.Time, options RevocationOptions) error
    RevocationEpoch(runtimeInstance containercontract.Runtime, userIdentifier string, options RevocationOptions) (time.Time, error)
}

type EpochRevocableTokenStore interface {
    RevocableTokenStore
    RevocationEpochStore
}
