package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Transport interface {
    Send(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Receive(runtimeInstance runtimecontract.Runtime) (<-chan Envelope, error)

    Ack(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance runtimecontract.Runtime, envelope Envelope, requeue bool) error

    /* Close takes no runtime: the container's ordered teardown calls it through the closer RegisterTransports registers, and the teardown recognizes Close() error alone. */
    Close() error
}
