package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type EventListener func(runtimeInstance runtimecontract.Runtime, event Event) error
