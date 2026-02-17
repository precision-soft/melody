package contract

import (
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type LoginHandler interface {
	Login(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, input LoginInput) (*LoginResult, error)
}
