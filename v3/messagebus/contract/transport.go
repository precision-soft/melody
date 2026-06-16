package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Transport interface {
    Send(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Receive(runtimeInstance runtimecontract.Runtime) (<-chan Envelope, error)

    Ack(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance runtimecontract.Runtime, envelope Envelope, requeue bool) error

    Close(runtimeInstance runtimecontract.Runtime) error
}
