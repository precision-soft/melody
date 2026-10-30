package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Bus interface {
    /* Dispatch runs the message through the bus with the stamps added, nil for none; a message that is already an Envelope keeps its own stamps. */
    Dispatch(runtimeInstance runtimecontract.Runtime, message any, stamps []Stamp) (Envelope, error)
}
