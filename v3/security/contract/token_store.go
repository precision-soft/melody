package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type TokenStore interface {
    Lookup(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, bool, error)
}
