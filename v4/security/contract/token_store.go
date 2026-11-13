package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type TokenStore interface {
    Lookup(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, bool, error)
}

/* TokenTtlRule is the reading every token store gives the ttl of a Put: a value of at least 1s, or unlimited for a token kept until it is deleted or revoked. It has no default, since a stored token declares how long it is honoured, so ResolveTokenTtl refuses an absent ttl before the rule is consulted. */
var TokenTtlRule = bound.Rule[time.Duration]{
    Name:              "RevocableTokenStore.Put.ttl",
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

/* ResolveTokenTtl decides the ttl of a Put against TokenTtlRule and answers the expiry to apply, 0 for a token with none; an absent ttl and a value below the minimum are refused, naming the ttl. */
func ResolveTokenTtl(ttl bound.Duration) (time.Duration, error) {
    if true == ttl.IsAbsent() {
        return 0, exception.NewError(
            "the token ttl is required",
            exceptioncontract.Context{
                "field": TokenTtlRule.Name,
            },
            nil,
        )
    }

    resolved, err := bound.Resolve(ttl, TokenTtlRule)
    if nil != err {
        return 0, err
    }

    if true == resolved.Unlimited {
        return 0, nil
    }

    return resolved.Value, nil
}

/* RevocableTokenStore stores tokens that can be individually withdrawn. Put stores the claims under the ttl, read against TokenTtlRule: an absent ttl and one below the minimum are refused by every implementation, since a lapsed remaining lifetime must not become forever, and unlimited stores the token with no expiry. */
type RevocableTokenStore interface {
    TokenStore
    Put(tokenString string, claims Claims, ttl bound.Duration)
    Delete(tokenString string)
    DeleteByUser(userIdentifier string) int
    PurgeExpired() int
}

type RevocationEpochStore interface {
    RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time)
    RevocationEpoch(runtimeInstance runtimecontract.Runtime, userIdentifier string, deviceIdentifier string) (time.Time, error)
}

type EpochRevocableTokenStore interface {
    RevocableTokenStore
    RevocationEpochStore
}
