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
    StampNameRedelivery = "redelivery"
    StampNameDelay      = "delay"
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

/** RedeliveryStamp records how many times an envelope has been requeued for another handling
attempt. The consumer owns the retry policy: it reads the count, decides whether to requeue or to
route the message to the failure transport, and stamps the incremented count on each retry. */
type RedeliveryStamp struct {
    Count int
}

func (instance RedeliveryStamp) StampName() string {
    return StampNameRedelivery
}

/** DelayStamp asks a delay-aware transport to hold an envelope before delivering it, which the
consumer attaches to a retry to space out attempts (backoff). The in-memory transport delivers
immediately and ignores it; a durable transport such as AMQP can honor it. */
type DelayStamp struct {
    Delay time.Duration
}

func (instance DelayStamp) StampName() string {
    return StampNameDelay
}

/** RedeliveryCount returns how many times an envelope has already been redelivered, or zero when it
has never been requeued. */
func RedeliveryCount(envelopeInstance messagebuscontract.Envelope) int {
    stamp, found := LastStampOfType[RedeliveryStamp](envelopeInstance)
    if false == found {
        return 0
    }

    return stamp.Count
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
