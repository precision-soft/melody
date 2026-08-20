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

/* Describe records the descriptor of a route. It writes a plain map the spec handler reads on the request path with nothing synchronizing the two, so it belongs to boot — module construction, before the application serves — exactly like the routes it describes; a Describe issued while requests are in flight is a concurrent map write, which Go answers by killing the process. */
func (instance *Registry) Describe(routeName string, descriptor Descriptor) *Registry {
    instance.descriptorsByRoute[routeName] = descriptor
    return instance
}

func (instance *Registry) Get(routeName string) (Descriptor, bool) {
    descriptor, exists := instance.descriptorsByRoute[routeName]
    return descriptor, exists
}
