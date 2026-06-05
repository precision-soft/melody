package messagebus

import (
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryTransport(bufferSize int) *InMemoryTransport {
    return &InMemoryTransport{
        queue: make(chan messagebuscontract.Envelope, bufferSize),
        done:  make(chan struct{}),
    }
}

type InMemoryTransport struct {
    queue     chan messagebuscontract.Envelope
    done      chan struct{}
    closeOnce sync.Once
}

func (instance *InMemoryTransport) Send(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    if _, received := LastStampOfType[ReceivedStamp](envelopeInstance); false == received {
        envelopeInstance = envelopeInstance.WithStamp(ReceivedStamp{TransportName: "in_memory"})
    }

    /** The closed state is checked first so a Send after Close fails deterministically; without this
    a select would otherwise pick the still-writable buffer at random over the closed signal. */
    select {
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    default:
    }

    select {
    case instance.queue <- envelopeInstance:
        return nil
    case <-instance.done:
        return exception.NewError("in-memory transport is closed", nil, nil)
    case <-runtimeInstance.Context().Done():
        return runtimeInstance.Context().Err()
    }
}

func (instance *InMemoryTransport) Receive(
    runtimeInstance runtimecontract.Runtime,
) (<-chan messagebuscontract.Envelope, error) {
    return instance.queue, nil
}

/** Close signals that no further messages will be accepted; it is idempotent and never closes the
underlying queue channel so a concurrent Send can never panic on a closed channel. */
func (instance *InMemoryTransport) Close(runtimeInstance runtimecontract.Runtime) error {
    instance.closeOnce.Do(func() {
        close(instance.done)
    })

    return nil
}

func (instance *InMemoryTransport) Ack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    return nil
}

func (instance *InMemoryTransport) Nack(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    requeue bool,
) error {
    if false == requeue {
        return nil
    }

    /** The envelope is re-enqueued exactly as handed over: the consumer owns the retry policy and
    has already stamped the incremented redelivery count, so the transport caps nothing itself.
    The requeue is non-blocking on purpose — Nack runs on the single consumer goroutine, so a
    blocking send on a full queue would deadlock the very reader that drains it. */
    select {
    case instance.queue <- envelopeInstance:
        return nil
    default:
        return exception.NewError("in-memory transport queue is full, dropped the requeued message", nil, nil)
    }
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
