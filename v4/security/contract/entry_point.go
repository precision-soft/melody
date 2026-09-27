package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type EntryPoint interface {
    Start(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (httpcontract.Response, error)
}
