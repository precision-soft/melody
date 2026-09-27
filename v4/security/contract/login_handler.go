package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type LoginHandler interface {
    Login(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, input LoginInput) (*LoginResult, error)
}
