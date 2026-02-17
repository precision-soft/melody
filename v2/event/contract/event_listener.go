package contract

import (
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type EventListener func(runtimeInstance runtimecontract.Runtime, event Event) error
