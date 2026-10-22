package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type StackNext func(runtimeInstance runtimecontract.Runtime, envelope Envelope) (Envelope, error)

type Middleware func(runtimeInstance runtimecontract.Runtime, envelope Envelope, next StackNext) (Envelope, error)
