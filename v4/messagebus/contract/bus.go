package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Bus interface {
    /* Dispatch runs the message through the bus with the stamps added, nil for none; a message that is already an Envelope keeps its own stamps. */
    Dispatch(runtimeInstance containercontract.Runtime, message any, stamps []Stamp) (Envelope, error)
}
