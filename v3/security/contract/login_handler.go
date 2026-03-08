package contract

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type LoginHandler interface {
    Login(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, input LoginInput) (*LoginResult, error)
}
