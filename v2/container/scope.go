package container

import (
    "reflect"
    "strings"
    "sync"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

func newScope(containerInstance *container) containercontract.Scope {
    return &scope{
        container:     containerInstance,
        instances:     make(map[string]any),
        typeInstances: make(map[reflect.Type]any),
    }
}

type scope struct {
    mutex         sync.RWMutex
    container     *container
    instances     map[string]any
    typeInstances map[reflect.Type]any
}

func (instance *scope) Get(serviceName string) (any, error) {
    if "" == serviceName {
        return nil, exception.NewError(
            "service name is empty in get",
            nil,
            nil,
        )
    }

    if nil == instance.container {
        exception.Panic(
            exception.NewError(
                "scope is closed",
                nil,
                nil,
            ),
        )
    }

    resolver := newScopeResolverContext(instance.container, instance)

    return resolver.Get(serviceName)
}

func (instance *scope) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        exception.Panic(
            exception.NewError(
                "failed to get service from scope",
                map[string]any{
                    "serviceName": serviceName,
                },
                getErr,
            ),
        )
    }

    return value
}

func (instance *scope) GetByType(targetType reflect.Type) (any, error) {
    if nil == targetType {
        return nil, exception.NewError(
            "service type is required in get by type",
            nil,
            nil,
        )
    }

    if nil == instance.container {
        exception.Panic(
            exception.NewError(
                "scope is closed",
                nil,
                nil,
            ),
        )
    }

    resolver := newScopeResolverContext(instance.container, instance)

    return resolver.GetByType(targetType)
}

func (instance *scope) MustGetByType(targetType reflect.Type) any {
    value, getByTypeErr := instance.GetByType(targetType)
    if nil != getByTypeErr {
        exception.Panic(
            exception.NewError(
                "failed to get service from scope by type",
                map[string]any{
                    "targetType": targetType.String(),
                },
                getByTypeErr,
            ),
        )
    }

    return value
}

func (instance *scope) Has(serviceName string) bool {
    if "" == serviceName {
        return false
    }

    if nil == instance.container {
        return false
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    _, exists := instance.instances[serviceName]
    if true == exists {
        return true
    }

    return instance.container.Has(serviceName)
}

func (instance *scope) HasType(targetType reflect.Type) bool {
    if nil == targetType {
        return false
    }

    if nil == instance.container {
        return false
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    _, exists := instance.typeInstances[targetType]
    if true == exists {
        return true
    }

    return instance.container.HasType(targetType)
}

func (instance *scope) OverrideInstance(serviceName string, value any) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is empty in override instance",
            nil,
            nil,
        )
    }

    if true == strings.HasPrefix(serviceName, "service.") {
        return exception.NewError(
            "service is protected and cannot be overridden",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    return instance.OverrideProtectedInstance(serviceName, value)
}

func (instance *scope) MustOverrideInstance(serviceName string, value any) {
    overrideInstanceErr := instance.OverrideInstance(serviceName, value)
    if nil != overrideInstanceErr {
        exception.Panic(
            exception.NewError(
                "failed to override service instance",
                map[string]any{
                    "serviceName": serviceName,
                },
                overrideInstanceErr,
            ),
        )
    }
}

func (instance *scope) OverrideProtectedInstance(serviceName string, value any) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is empty in override instance",
            nil,
            nil,
        )
    }

    if nil == value {
        return exception.NewError(
            "value is nil in override instance",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    if true == internal.IsNilInterface(value) {
        return exception.NewError(
            "value is nil in override instance",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    if nil == instance.container {
        exception.Panic(
            exception.NewError(
                "scope is closed",
                nil,
                nil,
            ),
        )
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.instances[serviceName] = value

    valueType := reflect.TypeOf(value)
    if nil == valueType {
        return exception.NewError(
            "service type is nil in override instance",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    canonicalType := canonicalServiceType(valueType)
    if nil == canonicalType {
        return exception.NewError(
            "canonical type is nil in override instance",
            map[string]any{
                "serviceName": serviceName,
                "valueType":   valueType.String(),
            },
            nil,
        )
    }

    instance.typeInstances[canonicalType] = value

    return nil
}

func (instance *scope) MustOverrideProtectedInstance(serviceName string, value any) {
    overrideInstanceErr := instance.OverrideProtectedInstance(serviceName, value)
    if nil != overrideInstanceErr {
        exception.Panic(
            exception.NewError(
                "failed to override protected service instance",
                map[string]any{
                    "serviceName": serviceName,
                },
                overrideInstanceErr,
            ),
        )
    }
}

func (instance *scope) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.instances = nil
    instance.typeInstances = nil
    instance.container = nil

    return nil
}

func (instance *scope) lookupInstanceByName(serviceName string) (any, bool, error) {
    if "" == serviceName {
        return nil, false, exception.NewError(
            "service name is empty in get",
            nil,
            nil,
        )
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if nil == instance.container {
        return nil, false, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    value, exists := instance.instances[serviceName]

    return value, exists, nil
}

func (instance *scope) lookupInstanceByType(canonicalType reflect.Type) (any, bool, error) {
    if nil == canonicalType {
        return nil, false, exception.NewError(
            "service type is required in get by type",
            nil,
            nil,
        )
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if nil == instance.container {
        return nil, false, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    value, exists := instance.typeInstances[canonicalType]

    return value, exists, nil
}

var _ containercontract.Scope = (*scope)(nil)
