package messagebus

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

type transportRouting struct {
    Name      string
    Transport messagebuscontract.Transport
}

/* NewSendMessageMiddleware sends each message type to the transport its routing names, the routing copied at the call; a nil routing is refused. */
func NewSendMessageMiddleware(routing *Routing) messagebuscontract.Middleware {
    if nil == routing {
        exception.Panic(exception.NewError("messagebus send middleware routing is nil", nil, nil))
    }

    routingByType := routing.build()

    return func(
        runtimeInstance containercontract.Runtime,
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

        return envelopeInstance.WithStamps([]messagebuscontract.Stamp{SentStamp{TransportName: routing.Name}}), nil
    }
}
