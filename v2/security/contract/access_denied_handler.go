package contract

import (
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type AccessDeniedHandler interface {
    Handle(runtimeInstance runtimecontract.Runtime, request httpcontract.Request, decisionErr error) (httpcontract.Response, error)
}
