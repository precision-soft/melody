package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type TokenStore interface {
    Lookup(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, bool, error)
}

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
