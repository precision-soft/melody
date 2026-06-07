package messagebus

import (
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type HandleOptions struct {
    RequireHandler bool
}

func NewHandleMessageMiddleware(locator messagebuscontract.HandlerLocator) messagebuscontract.Middleware {
    return NewHandleMessageMiddlewareWithOptions(locator, HandleOptions{})
}

func NewHandleMessageMiddlewareWithOptions(
    locator messagebuscontract.HandlerLocator,
    options HandleOptions,
) messagebuscontract.Middleware {
    return func(
        runtimeInstance runtimecontract.Runtime,
        envelopeInstance messagebuscontract.Envelope,
        next messagebuscontract.StackNext,
    ) (messagebuscontract.Envelope, error) {
        handlers := locator.HandlersFor(envelopeInstance.Message())

        if 0 == len(handlers) {
            if missingErr := noHandler(runtimeInstance, envelopeInstance, options); nil != missingErr {
                return envelopeInstance, missingErr
            }

            return next(runtimeInstance, envelopeInstance)
        }

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

func noHandler(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    options HandleOptions,
) error {
    if _, handled := LastStampOfType[HandledStamp](envelopeInstance); true == handled {
        return nil
    }

    /** reflect.TypeOf(nil) is a nil reflect.Type, so calling .String() on it panics. A nil message has
        no registered handler and reaches here; report a safe type name instead of dereferencing nil. */
    messageType := "<nil>"
    if message := envelopeInstance.Message(); nil != message {
        messageType = reflect.TypeOf(message).String()
    }

    if true == options.RequireHandler {
        return exception.NewError(
            "no handler is registered for the message",
            map[string]any{"type": messageType},
            nil,
        )
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Warning(
            "no handler is registered for the message; it passes through unhandled",
            map[string]any{"type": messageType},
        )
    }

    return nil
}
