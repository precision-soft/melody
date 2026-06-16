package messagebus

import (
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

func NewEnvelope(message any, stamps ...messagebuscontract.Stamp) messagebuscontract.Envelope {
    return &envelope{
        message: message,
        stamps:  stamps,
    }
}

func EnsureEnvelope(message any) messagebuscontract.Envelope {
    existing, isEnvelope := message.(messagebuscontract.Envelope)
    if true == isEnvelope {
        return existing
    }

    return NewEnvelope(message)
}

type envelope struct {
    message any
    stamps  []messagebuscontract.Stamp
}

func (instance *envelope) Message() any {
    return instance.message
}

func (instance *envelope) Stamps() []messagebuscontract.Stamp {
    return instance.stamps
}

func (instance *envelope) WithStamp(stamps ...messagebuscontract.Stamp) messagebuscontract.Envelope {
    combined := make([]messagebuscontract.Stamp, 0, len(instance.stamps)+len(stamps))
    combined = append(combined, instance.stamps...)
    combined = append(combined, stamps...)

    return &envelope{
        message: instance.message,
        stamps:  combined,
    }
}

var _ messagebuscontract.Envelope = (*envelope)(nil)
