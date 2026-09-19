package messagebus

import (
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewManager(name string, middlewares ...messagebuscontract.Middleware) *Manager {
    return &Manager{
        name:        name,
        middlewares: middlewares,
    }
}

type Manager struct {
    name        string
    middlewares []messagebuscontract.Middleware
}

func (instance *Manager) Dispatch(
    runtimeInstance runtimecontract.Runtime,
    message any,
    stamps ...messagebuscontract.Stamp,
) (messagebuscontract.Envelope, error) {
    if nil == message {
        return nil, exception.NewError("cannot dispatch a nil message", nil, nil)
    }

    envelopeInstance := EnsureEnvelope(message).WithStamp(stamps...)
    envelopeInstance = envelopeInstance.WithStamp(BusNameStamp{BusName: instance.name})

    chain := instance.buildChain(0)

    result, chainErr := chain(runtimeInstance, envelopeInstance)
    if nil == chainErr && nil != result {
        instance.warnWhenUntouched(runtimeInstance, result)
    }

    return result, chainErr
}

/* warnWhenUntouched records a dispatch that succeeded while nothing sent, handled or even received the message — a send-only bus with no route for the type, or a bus assembled with no middlewares at all. The terminal chain answers success by construction, so this is the one place that can see the difference between "delivered somewhere" and "did absolutely nothing": without the record, a forgotten RouteType line means every dispatch of that type returns nil error while the message ceases to exist. A received envelope is exempt — on the consume path the handle middleware owns the missing-handler verdict. */
func (instance *Manager) warnWhenUntouched(
    runtimeInstance runtimecontract.Runtime,
    envelopeInstance messagebuscontract.Envelope,
) {
    if _, sent := LastStampOfType[SentStamp](envelopeInstance); true == sent {
        return
    }
    if _, handled := LastStampOfType[HandledStamp](envelopeInstance); true == handled {
        return
    }
    if _, received := LastStampOfType[ReceivedStamp](envelopeInstance); true == received {
        return
    }

    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    messageType := "<nil>"
    if message := envelopeInstance.Message(); nil != message {
        messageType = reflect.TypeOf(message).String()
    }

    logger.Warning(
        "message bus dispatch succeeded but nothing sent or handled the message",
        map[string]any{"bus": instance.name, "type": messageType},
    )
}

func (instance *Manager) buildChain(index int) messagebuscontract.StackNext {
    if index >= len(instance.middlewares) {
        return func(
            runtimeInstance runtimecontract.Runtime,
            envelopeInstance messagebuscontract.Envelope,
        ) (messagebuscontract.Envelope, error) {
            return envelopeInstance, nil
        }
    }

    middleware := instance.middlewares[index]
    next := instance.buildChain(index + 1)

    return func(
        runtimeInstance runtimecontract.Runtime,
        envelopeInstance messagebuscontract.Envelope,
    ) (messagebuscontract.Envelope, error) {
        return middleware(runtimeInstance, envelopeInstance, next)
    }
}

var _ messagebuscontract.Bus = (*Manager)(nil)
