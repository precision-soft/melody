package messagebus

import (
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewHandlerLocator() *HandlerLocator {
    return &HandlerLocator{
        handlersByType: make(map[reflect.Type][]messagebuscontract.MessageHandler),
    }
}

type HandlerLocator struct {
    mutex          sync.RWMutex
    handlersByType map[reflect.Type][]messagebuscontract.MessageHandler
}

func (instance *HandlerLocator) Register(messageType reflect.Type, handler messagebuscontract.MessageHandler) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    existing := instance.handlersByType[messageType]

    updated := make([]messagebuscontract.MessageHandler, 0, len(existing)+1)
    updated = append(updated, existing...)
    updated = append(updated, handler)

    instance.handlersByType[messageType] = updated
}

func (instance *HandlerLocator) HandlersFor(message any) []messagebuscontract.MessageHandler {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.handlersByType[reflect.TypeOf(message)]
}

func RegisterHandler[T any](
    locator *HandlerLocator,
    handle func(runtimeInstance runtimecontract.Runtime, message T) error,
) {
    messageType := reflect.TypeOf((*T)(nil)).Elem()
    locator.Register(messageType, &functionHandler[T]{handle: handle})
}

type functionHandler[T any] struct {
    handle func(runtimeInstance runtimecontract.Runtime, message T) error
}

func (instance *functionHandler[T]) Handle(runtimeInstance runtimecontract.Runtime, message any) error {
    typed, isType := message.(T)
    if false == isType {
        return exception.NewError(
            "message handler received unexpected message type",
            map[string]any{
                "expectedType": reflect.TypeOf((*T)(nil)).Elem().String(),
                "actualType":   reflect.TypeOf(message).String(),
            },
            nil,
        )
    }

    return instance.handle(runtimeInstance, typed)
}

var _ messagebuscontract.HandlerLocator = (*HandlerLocator)(nil)
