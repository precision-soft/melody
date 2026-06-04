package openapi

import (
    "reflect"
)

func TypeOf[T any]() reflect.Type {
    return reflect.TypeOf((*T)(nil)).Elem()
}

type Descriptor struct {
    Summary     string
    Description string
    Tags        []string
    RequestType reflect.Type
    Responses   map[int]reflect.Type
}

func NewRegistry() *Registry {
    return &Registry{
        descriptorsByRoute: make(map[string]Descriptor),
    }
}

type Registry struct {
    descriptorsByRoute map[string]Descriptor
}

func (instance *Registry) Describe(routeName string, descriptor Descriptor) *Registry {
    instance.descriptorsByRoute[routeName] = descriptor
    return instance
}

func (instance *Registry) Get(routeName string) (Descriptor, bool) {
    descriptor, exists := instance.descriptorsByRoute[routeName]
    return descriptor, exists
}
