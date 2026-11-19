package security

import (
    nethttp "net/http"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func NewJsonAccessDeniedHandler() *JsonAccessDeniedHandler {
    return &JsonAccessDeniedHandler{}
}

type JsonAccessDeniedHandler struct {
}

func (instance *JsonAccessDeniedHandler) Handle(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
    decisionErr error,
) (httpcontract.Response, error) {
    return http.JsonErrorResponse(nethttp.StatusForbidden, "forbidden"), nil
}

var _ securitycontract.AccessDeniedHandler = (*JsonAccessDeniedHandler)(nil)
