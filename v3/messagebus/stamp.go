package messagebus

import (
    "time"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const (
    StampNameBusName    = "bus_name"
    StampNameSent       = "sent"
    StampNameReceived   = "received"
    StampNameHandled    = "handled"
    StampNameRedelivery       = "redelivery"
    StampNameDelay            = "delay"
    StampNameDeadLetterAttempt = "dead_letter_attempt"
    StampNameMessageId         = "message_id"
)

type BusNameStamp struct {
    BusName string
}

func (instance BusNameStamp) StampName() string {
    return StampNameBusName
}

type SentStamp struct {
    TransportName string
}

func (instance SentStamp) StampName() string {
    return StampNameSent
}

type ReceivedStamp struct {
    TransportName string
}

func (instance ReceivedStamp) StampName() string {
    return StampNameReceived
}

type HandledStamp struct {
    HandlerName string
}

func (instance HandledStamp) StampName() string {
    return StampNameHandled
}

type RedeliveryStamp struct {
    Count int
}

func (instance RedeliveryStamp) StampName() string {
    return StampNameRedelivery
}

type DelayStamp struct {
    Delay time.Duration
}

func (instance DelayStamp) StampName() string {
    return StampNameDelay
}

type DeadLetterAttemptStamp struct {
    Count int
}

func (instance DeadLetterAttemptStamp) StampName() string {
    return StampNameDeadLetterAttempt
}

/* MessageIdStamp carries a stable, producer-assigned identifier for the message so a transport can publish it (for example as the AMQP message id) and a consumer can deduplicate redeliveries. A producer with at-least-once semantics — such as the outbox relay, which may redeliver after a transport-success-then-crash — stamps it with a deterministic id per logical message. */
type MessageIdStamp struct {
    MessageId string
}

func (instance MessageIdStamp) StampName() string {
    return StampNameMessageId
}

func RedeliveryCount(envelopeInstance messagebuscontract.Envelope) int {
    stamp, found := LastStampOfType[RedeliveryStamp](envelopeInstance)
    if false == found {
        return 0
    }

    return stamp.Count
}

func DeadLetterAttemptCount(envelopeInstance messagebuscontract.Envelope) int {
    stamp, found := LastStampOfType[DeadLetterAttemptStamp](envelopeInstance)
    if false == found {
        return 0
    }

    return stamp.Count
}

/* MessageId returns the producer-assigned message id stamped on the envelope, if any, so a transport can carry it for consumer-side deduplication. */
func MessageId(envelopeInstance messagebuscontract.Envelope) (string, bool) {
    stamp, found := LastStampOfType[MessageIdStamp](envelopeInstance)
    if false == found {
        return "", false
    }

    return stamp.MessageId, true
}

func LastStampOfType[T messagebuscontract.Stamp](envelopeInstance messagebuscontract.Envelope) (T, bool) {
    var found T
    var exists bool

    for _, stamp := range envelopeInstance.Stamps() {
        typed, isType := stamp.(T)
        if true == isType {
            found = typed
            exists = true
        }
    }

    return found, exists
}
