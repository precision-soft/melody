package amqp

import (
    "reflect"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

func NewMessageRegistry() *MessageRegistry {
    return &MessageRegistry{
        typeByName: make(map[string]reflect.Type),
        nameByType: make(map[reflect.Type]string),
    }
}

type MessageRegistry struct {
    mutex      sync.RWMutex
    typeByName map[string]reflect.Type
    nameByType map[reflect.Type]string
}

func RegisterMessage[T any](registry *MessageRegistry, name string) {
    messageType := reflect.TypeOf((*T)(nil)).Elem()

    registry.mutex.Lock()
    defer registry.mutex.Unlock()

    if existingType, exists := registry.typeByName[name]; true == exists && existingType != messageType {
        exception.Panic(exception.NewError(
            "amqp message name is already registered to a different type",
            map[string]any{"name": name, "existingType": existingType.String(), "newType": messageType.String()},
            nil,
        ))
    }

    if existingName, exists := registry.nameByType[messageType]; true == exists && existingName != name {
        exception.Panic(exception.NewError(
            "amqp message type is already registered under a different name",
            map[string]any{"type": messageType.String(), "existingName": existingName, "newName": name},
            nil,
        ))
    }

    registry.typeByName[name] = messageType
    registry.nameByType[messageType] = name
}

func (instance *MessageRegistry) NameFor(message any) (string, bool) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    name, exists := instance.nameByType[reflect.TypeOf(message)]
    return name, exists
}

func (instance *MessageRegistry) New(name string) (any, bool) {
    instance.mutex.RLock()
    messageType, exists := instance.typeByName[name]
    instance.mutex.RUnlock()

    if false == exists {
        return nil, false
    }

    return reflect.New(messageType).Interface(), true
}
