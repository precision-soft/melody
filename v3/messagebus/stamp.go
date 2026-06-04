package messagebus

import (
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const (
    StampNameBusName  = "bus_name"
    StampNameSent     = "sent"
    StampNameReceived = "received"
    StampNameHandled  = "handled"
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
