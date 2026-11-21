package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type Rule interface {
    Applies(request httpcontract.Request) bool

    /* Check reads its stores and services through the runtime it is handed, the request's. */
    Check(runtimeInstance containercontract.Runtime, request httpcontract.Request) error
}
