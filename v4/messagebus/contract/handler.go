package contract

import (
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* MessageHandler handles one delivery: the envelope carries the message and the stamps a consumer reads, the message id and the redelivery count among them. */
type MessageHandler interface {
    Handle(runtimeInstance runtimecontract.Runtime, envelope Envelope) error
}
