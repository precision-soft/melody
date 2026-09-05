package messagebus

import (
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type HandleOptions struct {
    /* AllowMissingHandler lets a message with no registered handler pass through with a warning instead of failing the dispatch. The default refuses: on the consume path a pass-through is immediately Acked, so a forgotten registration — or the pointer-vs-value keying trap — would silently drain a production queue one warning at a time, with the retry and dead-letter machinery never engaging because the pipeline was told the message was handled. */
    AllowMissingHandler bool
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

    messageType := "<nil>"
    if message := envelopeInstance.Message(); nil != message {
        messageType = reflect.TypeOf(message).String()
    }

    if false == options.AllowMissingHandler {
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
