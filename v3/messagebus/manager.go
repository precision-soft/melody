package messagebus

import (
    "github.com/precision-soft/melody/v3/exception"
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

    return chain(runtimeInstance, envelopeInstance)
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
