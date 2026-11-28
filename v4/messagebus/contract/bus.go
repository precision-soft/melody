package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* DispatchOptions is what one dispatch declares beside its message; the zero value adds nothing. */
type DispatchOptions struct {
    /* Stamps are added to the envelope, in order; nil adds none. */
    Stamps []Stamp
}

type Bus interface {
    /* Dispatch runs the message through the bus with the options' stamps added; a message that is already an Envelope keeps its own stamps. */
    Dispatch(runtimeInstance containercontract.Runtime, message any, options DispatchOptions) (Envelope, error)
}
