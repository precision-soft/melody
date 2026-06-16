package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type StackNext func(runtimeInstance runtimecontract.Runtime, envelope Envelope) (Envelope, error)

type Middleware func(runtimeInstance runtimecontract.Runtime, envelope Envelope, next StackNext) (Envelope, error)
