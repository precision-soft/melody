package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type StackNext func(runtimeInstance containercontract.Runtime, envelope Envelope) (Envelope, error)

type Middleware func(runtimeInstance containercontract.Runtime, envelope Envelope, next StackNext) (Envelope, error)
