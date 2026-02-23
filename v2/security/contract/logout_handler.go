package contract

import (
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type LogoutHandler interface {
    Logout(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, input LogoutInput) (*LogoutResult, error)
}
