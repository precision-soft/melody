package messagebus

import (
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewInMemoryTransport(bufferSize int) *InMemoryTransport {
    return &InMemoryTransport{
        queue: make(chan messagebuscontract.Envelope, bufferSize),
    }
}

type InMemoryTransport struct {
    queue chan messagebuscontract.Envelope
}

type inMemoryRedeliveredStamp struct{}

func (instance inMemoryRedeliveredStamp) StampName() string {
    return "in_memory_redelivered"
}

func (instance *InMemoryTransport) Send(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) error {
    if _, received := LastStampOfType[ReceivedStamp](envelopeInstance); false == received {
        envelopeInstance = envelopeInstance.WithStamp(ReceivedStamp{TransportName: "in_memory"})
    }

    select {
    case instance.queue <- envelopeInstance:
        return nil
    case <-runtimeInstance.Context().Done():
        return runtimeInstance.Context().Err()
    }
}

func (instance *InMemoryTransport) Receive(
    runtimeInstance runtimecontract.Runtime,
) (<-chan messagebuscontract.Envelope, error) {
    return instance.queue, nil
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

    if _, redelivered := LastStampOfType[inMemoryRedeliveredStamp](envelopeInstance); true == redelivered {
        return nil
    }

    return instance.Send(runtimeInstance, envelopeInstance.WithStamp(inMemoryRedeliveredStamp{}))
}

var _ messagebuscontract.Transport = (*InMemoryTransport)(nil)
