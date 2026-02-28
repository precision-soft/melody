package contract

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type EntryPoint interface {
    Start(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (httpcontract.Response, error)
}
