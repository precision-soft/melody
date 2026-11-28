package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type LogoutHandler interface {
    Logout(runtimeInstance containercontract.Runtime, request httpcontract.Request, input LogoutInput) (*LogoutResult, error)
}
