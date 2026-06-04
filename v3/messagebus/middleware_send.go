package messagebus

import (
    "reflect"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type TransportRouting struct {
    Name      string
    Transport messagebuscontract.Transport
}

func NewSendMessageMiddleware(routingByType map[reflect.Type]TransportRouting) messagebuscontract.Middleware {
    return func(
        runtimeInstance runtimecontract.Runtime,
        envelopeInstance messagebuscontract.Envelope,
        next messagebuscontract.StackNext,
    ) (messagebuscontract.Envelope, error) {
        _, alreadyReceived := LastStampOfType[ReceivedStamp](envelopeInstance)
        if true == alreadyReceived {
            return next(runtimeInstance, envelopeInstance)
        }

        routing, hasRoute := routingByType[reflect.TypeOf(envelopeInstance.Message())]
        if false == hasRoute {
            return next(runtimeInstance, envelopeInstance)
        }

        sendErr := routing.Transport.Send(runtimeInstance, envelopeInstance)
        if nil != sendErr {
            return envelopeInstance, sendErr
        }

        return envelopeInstance.WithStamp(SentStamp{TransportName: routing.Name}), nil
    }
}
