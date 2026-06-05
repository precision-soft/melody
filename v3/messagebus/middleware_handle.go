package messagebus

import (
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/** HandleOptions tunes how the handle middleware reacts when a message reaches it with no
registered handler. By default that is logged as a warning and the message continues through the
stack; with RequireHandler set it becomes an error, so a forgotten RegisterHandler fails loudly
(and, on the consumer, is retried/dead-lettered) instead of being acked and discarded silently. */
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

/** noHandler decides what to do when no handler matched the message. A message already carrying a
HandledStamp was handled by an earlier stage and is left alone; otherwise it is rejected when
RequireHandler is set, or logged so the silent loss is at least observable. */
func noHandler(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
    options HandleOptions,
) error {
    if _, handled := LastStampOfType[HandledStamp](envelopeInstance); true == handled {
        return nil
    }

    messageType := reflect.TypeOf(envelopeInstance.Message()).String()

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
