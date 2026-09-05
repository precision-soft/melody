package contract

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type Transport interface {
    Send(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Receive(runtimeInstance runtimecontract.Runtime) (<-chan Envelope, error)

    Ack(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance runtimecontract.Runtime, envelope Envelope, requeue bool) error

    /* Close takes no runtime: it is the container's ordered teardown that calls it — through the closer RegisterTransports registers — and the teardown recognizes Close() error and nothing else. The former Close(runtime) signature made every transport structurally unreachable at shutdown: nothing in the framework or in any production wiring ever called it, so a broker connection lived exactly as long as the process and every deploy tore it down abruptly. */
    Close() error
}
