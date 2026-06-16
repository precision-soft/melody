package security

import (
    nethttp "net/http"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewJsonEntryPoint() *JsonEntryPoint {
    return &JsonEntryPoint{}
}

type JsonEntryPoint struct {
}

func (instance *JsonEntryPoint) Start(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (httpcontract.Response, error) {
    response := http.JsonErrorResponse(nethttp.StatusUnauthorized, "unauthorized")
    response.Headers().Set("WWW-Authenticate", "Bearer")

    return response, nil
}

var _ securitycontract.EntryPoint = (*JsonEntryPoint)(nil)
