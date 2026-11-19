package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type EventListener func(runtimeInstance containercontract.Runtime, event Event) error
