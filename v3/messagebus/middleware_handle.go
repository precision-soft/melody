package messagebus

import (
    "reflect"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewHandleMessageMiddleware(locator messagebuscontract.HandlerLocator) messagebuscontract.Middleware {
    return func(
        runtimeInstance runtimecontract.Runtime,
        envelopeInstance messagebuscontract.Envelope,
        next messagebuscontract.StackNext,
    ) (messagebuscontract.Envelope, error) {
        handlers := locator.HandlersFor(envelopeInstance.Message())

        for _, handler := range handlers {
            handleErr := handler.Handle(runtimeInstance, envelopeInstance.Message())
            if nil != handleErr {
                return envelopeInstance, handleErr
            }

            envelopeInstance = envelopeInstance.WithStamp(HandledStamp{HandlerName: reflect.TypeOf(handler).String()})
        }

        return next(runtimeInstance, envelopeInstance)
    }
}
