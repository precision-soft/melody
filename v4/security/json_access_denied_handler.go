package security

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func NewJsonAccessDeniedHandler() *JsonAccessDeniedHandler {
    return &JsonAccessDeniedHandler{}
}

type JsonAccessDeniedHandler struct {
}

func (instance *JsonAccessDeniedHandler) Handle(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    decisionErr error,
) (httpcontract.Response, error) {
    return http.JsonErrorResponse(nethttp.StatusForbidden, "forbidden"), nil
}

var _ securitycontract.AccessDeniedHandler = (*JsonAccessDeniedHandler)(nil)
