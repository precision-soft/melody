package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type TokenStore interface {
    Lookup(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, bool, error)
}

/* RevocableTokenStore stores tokens that can be individually withdrawn. Put stores with no expiry — that is the contract's only spelling of "forever" — and PutWithTtl requires a POSITIVE ttl, refusing zero and negative values loudly: the likeliest caller of a non-positive ttl computed a remaining lifetime that had already elapsed, and storing that token forever would be the exact inversion of what was asked. */
type RevocableTokenStore interface {
    TokenStore
    Put(tokenString string, claims Claims)
    PutWithTtl(tokenString string, claims Claims, ttl time.Duration)
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
