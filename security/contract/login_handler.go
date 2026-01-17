package contract

import (
	httpcontract "github.com/precision-soft/melody/http/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type LoginHandler interface {
	Login(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, input LoginInput) (*LoginResult, error)
}
