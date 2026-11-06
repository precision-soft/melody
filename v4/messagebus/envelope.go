package messagebus

import (
    "github.com/precision-soft/melody/v4/internal"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* NewEnvelope wraps the message with the stamps, copied; nil is an envelope with no stamp. */
func NewEnvelope(message any, stamps []messagebuscontract.Stamp) messagebuscontract.Envelope {
    return &envelope{
        message: message,
        stamps:  append([]messagebuscontract.Stamp(nil), stamps...),
    }
}

func EnsureEnvelope(message any) messagebuscontract.Envelope {
    existing, isEnvelope := message.(messagebuscontract.Envelope)
    if true == isEnvelope {
        return existing
    }

    return NewEnvelope(message, nil)
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

func (instance *envelope) WithStamp(stamp messagebuscontract.Stamp) messagebuscontract.Envelope {
    return instance.WithStamps([]messagebuscontract.Stamp{stamp})
}

func (instance *envelope) WithStamps(stamps []messagebuscontract.Stamp) messagebuscontract.Envelope {
    combined := make([]messagebuscontract.Stamp, 0, len(instance.stamps)+len(stamps))
    combined = append(combined, instance.stamps...)
    combined = append(combined, stamps...)

    return &envelope{
        message: instance.message,
        stamps:  combined,
    }
}

/* withReplacedStamp answers the envelope carrying stamp in place of every earlier stamp of its name, or appended where it carries none: the redelivery, delay and dead-letter counters are read from their last stamp, so a message requeued for ever carries one of each, not one per cycle. An Envelope implemented outside the package gets the stamp appended. */
func withReplacedStamp(envelopeInstance messagebuscontract.Envelope, stamp messagebuscontract.Stamp) messagebuscontract.Envelope {
    concrete, isConcrete := envelopeInstance.(*envelope)
    if false == isConcrete || nil == concrete {
        return envelopeInstance.WithStamp(stamp)
    }

    kept := make([]messagebuscontract.Stamp, 0, len(concrete.stamps)+1)
    for _, existing := range concrete.stamps {
        if false == internal.IsNilInterface(existing) && stamp.StampName() == existing.StampName() {
            continue
        }

        kept = append(kept, existing)
    }

    return &envelope{
        message: concrete.message,
        stamps:  append(kept, stamp),
    }
}

var _ messagebuscontract.Envelope = (*envelope)(nil)
