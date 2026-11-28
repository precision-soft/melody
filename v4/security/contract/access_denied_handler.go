package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type AccessDeniedHandler interface {
    Handle(runtimeInstance containercontract.Runtime, request httpcontract.Request, decisionErr error) (httpcontract.Response, error)
}
