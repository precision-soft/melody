package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* MessageHandler handles one delivery: the envelope carries the message and the stamps a consumer reads, the message id and the redelivery count among them. */
type MessageHandler interface {
    Handle(runtimeInstance containercontract.Runtime, envelope Envelope) error
}
