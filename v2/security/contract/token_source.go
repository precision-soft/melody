package contract

import (
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type TokenSource interface {
    Name() string

    Resolve(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) (Token, error)
}
