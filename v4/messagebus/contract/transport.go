package contract

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* Subscription is one stream of deliveries. Envelopes is closed when the stream ends; Err then answers why, nil for an end the consumer asked for through its runtime or the transport's close. */
type Subscription interface {
    Envelopes() <-chan Envelope

    Err() error
}

/* NackOptions is how a delivery is refused; the zero value does not requeue. */
type NackOptions struct {
    /* Requeue hands the delivery back to the transport for another attempt. */
    Requeue bool
}

type Transport interface {
    /* Send publishes the envelope. Across a process boundary two stamps cross every transport, messagebus.HeadersStamp and the message id, each refused above its limits before any write; every other stamp is process-local unless the transport documents otherwise. A failure marked with exception.MarkInfrastructureFailure says the transport could not be reached: the outbox relay then charges the row no attempt, unless the next row of the batch is delivered, and tries it again after its outage backoff, so the mark belongs to an outage alone, never to a refusal of the message. */
    Send(runtimeInstance containercontract.Runtime, envelope Envelope) error

    /* Receive answers the subscription its deliveries arrive on; the runtime's context is the one stop. */
    Receive(runtimeInstance containercontract.Runtime) (Subscription, error)

    Ack(runtimeInstance containercontract.Runtime, envelope Envelope) error

    Nack(runtimeInstance containercontract.Runtime, envelope Envelope, options NackOptions) error

    /* CloseWithContext takes no runtime: the container's ordered teardown calls it through the closer RegisterTransports registers, handing it the teardown's remaining budget. */
    CloseWithContext(closeContext context.Context) error
}
