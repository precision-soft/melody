package contract

import (
    "context"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Transport interface {
    Send(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Receive(runtimeInstance runtimecontract.Runtime) (<-chan Envelope, error)

    Ack(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance runtimecontract.Runtime, envelope Envelope, requeue bool) error

    /* CloseWithContext takes no runtime: the container's ordered teardown calls it through the closer RegisterTransports registers, handing it the teardown's remaining budget. */
    CloseWithContext(closeContext context.Context) error
}
