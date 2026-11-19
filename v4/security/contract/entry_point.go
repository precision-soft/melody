package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type EntryPoint interface {
    Start(runtimeInstance containercontract.Runtime, request httpcontract.Request) (httpcontract.Response, error)
}
