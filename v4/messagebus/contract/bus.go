package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Bus interface {
    Dispatch(runtimeInstance runtimecontract.Runtime, message any, stamps ...Stamp) (Envelope, error)
}
