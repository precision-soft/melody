package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type MessageHandler interface {
    Handle(runtimeInstance runtimecontract.Runtime, message any) error
}
