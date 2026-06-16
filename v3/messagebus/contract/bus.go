package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Bus interface {
    Dispatch(runtimeInstance runtimecontract.Runtime, message any, stamps ...Stamp) (Envelope, error)
}
