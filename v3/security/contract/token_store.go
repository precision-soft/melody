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
