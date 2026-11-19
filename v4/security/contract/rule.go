package contract

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Rule interface {
    Applies(request httpcontract.Request) bool

    /* Check reads its stores and services through the runtime it is handed, the request's. */
    Check(runtimeInstance runtimecontract.Runtime, request httpcontract.Request) error
}
