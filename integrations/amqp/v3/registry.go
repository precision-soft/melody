package amqp

import (
    "reflect"
)

func NewMessageRegistry() *MessageRegistry {
    return &MessageRegistry{
        typeByName: make(map[string]reflect.Type),
        nameByType: make(map[reflect.Type]string),
    }
}

type MessageRegistry struct {
    typeByName map[string]reflect.Type
    nameByType map[reflect.Type]string
}

func RegisterMessage[T any](registry *MessageRegistry, name string) {
    messageType := reflect.TypeOf((*T)(nil)).Elem()
    registry.typeByName[name] = messageType
    registry.nameByType[messageType] = name
}

func (instance *MessageRegistry) NameFor(message any) (string, bool) {
    name, exists := instance.nameByType[reflect.TypeOf(message)]
    return name, exists
}

func (instance *MessageRegistry) New(name string) (any, bool) {
    messageType, exists := instance.typeByName[name]
    if false == exists {
        return nil, false
    }

    return reflect.New(messageType).Interface(), true
}
