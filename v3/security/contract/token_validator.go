package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Claims struct {
    UserIdentifier string
    Roles          []string
}

type TokenValidator interface {
    Validate(runtimeInstance runtimecontract.Runtime, tokenString string) (Claims, error)
}
