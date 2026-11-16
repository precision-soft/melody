package contract

import (
    "context"

    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type Transport interface {
    /* Send publishes the envelope. A failure marked with exception.MarkInfrastructureFailure says the transport could not be reached: the outbox relay then charges the row no attempt, unless the next row of the batch is delivered, and tries it again after its outage backoff, so the mark belongs to an outage alone, never to a refusal of the message. */
    Send(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Receive(runtimeInstance runtimecontract.Runtime) (<-chan Envelope, error)

    Ack(runtimeInstance runtimecontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance runtimecontract.Runtime, envelope Envelope, requeue bool) error

    /* CloseWithContext takes no runtime: the container's ordered teardown calls it through the closer RegisterTransports registers, handing it the teardown's remaining budget. */
    CloseWithContext(closeContext context.Context) error
}
