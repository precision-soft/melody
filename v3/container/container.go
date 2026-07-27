package container

import (
    "reflect"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

func NewContainer() containercontract.Container {
    return &container{
        providers:                   make(map[string]providerAny),
        instances:                   make(map[string]any),
        typeProviders:               make(map[reflect.Type]providerAny),
        typeInstances:               make(map[reflect.Type]any),
        creatingByName:              make(map[string]*creationState),
        creatingByType:              make(map[string]*creationState),
        resolverWaitGraph:           make(map[uint64]map[uint64]struct{}),
        typeRegistrationNamesByType: make(map[reflect.Type][]string),
        collectionPriorityByName:    make(map[string]int),
        dependencyGraph:             make(map[string]map[string]struct{}),

        scopedProviders:                   make(map[string]providerAny),
        scopedTypeProviders:               make(map[reflect.Type]providerAny),
        scopedTypeRegistrationNamesByType: make(map[reflect.Type][]string),
        scopedCollectionPriorityByName:    make(map[string]int),
        scopedReplacesContainerService:    make(map[string]bool),
    }
}

type container struct {
    mutex                       sync.RWMutex
    providers                   map[string]providerAny
    instances                   map[string]any
    typeProviders               map[reflect.Type]providerAny
    typeInstances               map[reflect.Type]any
    creatingByName              map[string]*creationState
    creatingByType              map[string]*creationState
    resolverContextIdCounter    atomic.Uint64
    resolverWaitGraph           map[uint64]map[uint64]struct{}
    typeRegistrationNamesByType map[reflect.Type][]string
    collectionPriorityByName    map[string]int
    dependencyGraph             map[string]map[string]struct{}
    /* the scoped registrations: providers of services the SCOPES of this container own, kept apart from the container's own so nothing resolved against the container can reach them. They are the source the immutable plan below is built from, never read on the request path. */
    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedCollectionPriorityByName    map[string]int
    scopedReplacesContainerService    map[string]bool
    /* the published plan every new scope is bound to by reference. A registration clears it and the next scope rebuilds it, so creating a scope costs one atomic load once boot has settled. */
    scopePlanPointer atomic.Pointer[scopePlan]
    isClosed         bool
    closeErr         error
    closeOnce        sync.Once
}

func (instance *container) Get(serviceName string) (any, error) {
    resolver := newResolverContext(instance)

    return resolver.Get(serviceName)
}

func (instance *container) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        exception.Panic(
            exception.NewError(
                "failed to get service instance",
                map[string]any{
                    "serviceName": serviceName,
                },
                getErr,
            ),
        )
    }

    return value
}

func (instance *container) GetByType(targetType reflect.Type) (any, error) {
    resolver := newResolverContext(instance)
    return resolver.GetByType(targetType)
}

func (instance *container) MustGetByType(targetType reflect.Type) any {
    resolver := newResolverContext(instance)
    return resolver.MustGetByType(targetType)
}

func (instance *container) Has(serviceName string) bool {
    if "" == serviceName {
        return false
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    _, exists := instance.instances[serviceName]
    if true == exists {
        return true
    }

    _, exists = instance.providers[serviceName]
    if true == exists {
        return true
    }

    return false
}

func (instance *container) HasType(targetType reflect.Type) bool {
    if nil == targetType {
        return false
    }

    /* @important the lookup is canonical because the registrations are: a service registered from a provider returning *T is filed under *T, and GetByType canonicalises before it looks. Asking with the value type therefore used to be answered "no" for a service GetByType resolves happily, so Has and Get disagreed about the same container. */
    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        return false
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    _, exists := instance.typeInstances[canonicalType]
    if true == exists {
        return true
    }

    _, exists = instance.typeProviders[canonicalType]
    if true == exists {
        return true
    }

    registeredServiceNames, existsName := instance.typeRegistrationNamesByType[canonicalType]
    if true == existsName && 0 < len(registeredServiceNames) {
        return true
    }

    return false
}

func (instance *container) OverrideInstance(serviceName string, value any) error {
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

func (instance *container) MustOverrideInstance(serviceName string, value any) {
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

func (instance *container) OverrideProtectedInstance(serviceName string, value any) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is empty in override instance",
            nil,
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

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, exists := instance.providers[serviceName]; false == exists {
        return exception.NewError(
            "service not registered in container",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

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

    if _, typeRegistered := instance.typeRegistrationNamesByType[canonicalType]; true == typeRegistered {
        instance.typeInstances[canonicalType] = value
    }

    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                instance.typeInstances[registeredType] = value
                break
            }
        }
    }

    return nil
}

func (instance *container) MustOverrideProtectedInstance(serviceName string, value any) {
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

func (instance *container) NewScope() containercontract.Scope {
    return newScope(instance, instance.scopePlanForNewScope())
}

func (instance *container) Names() []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    serviceNames := make([]string, 0, len(instance.providers))
    for serviceName := range instance.providers {
        serviceNames = append(serviceNames, serviceName)
    }

    sort.Strings(serviceNames)

    return serviceNames
}

func (instance *container) registerDependencyLocked(dependentKey string, dependencyKey string) {
    if "" == dependentKey || "" == dependencyKey {
        return
    }

    dependencies, exists := instance.dependencyGraph[dependentKey]
    if false == exists {
        dependencies = make(map[string]struct{})
        instance.dependencyGraph[dependentKey] = dependencies
    }

    dependencies[dependencyKey] = struct{}{}
}

func (instance *container) register(
    serviceName string,
    serviceType reflect.Type,
    provider providerAny,
    options ...containercontract.RegisterOption,
) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is required to register a service",
            nil,
            nil,
        )
    }

    if nil == provider {
        return exception.NewError(
            "the provider is required to register a service",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    registerOption := applyRegisterServiceOptions(options)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, exists := instance.providers[serviceName]; true == exists {
        return exception.NewError(
            "service already registered",
            map[string]any{
                "serviceName": serviceName,
            },
            ErrServiceIdAlreadyRegistered,
        )
    }

    if true == instance.scopedRegistrationBlocksLocked(serviceName) {
        return exception.NewError(
            "service name is already registered as a scoped service",
            map[string]any{
                "serviceName": serviceName,
            },
            ErrScopedServiceIdAlreadyRegistered,
        )
    }

    instance.providers[serviceName] = provider

    if 0 != registerOption.CollectionPriority {
        instance.collectionPriorityByName[serviceName] = registerOption.CollectionPriority
    }

    if true == registerOption.AlsoRegisterType {
        registerTypeErr := instance.registerType(
            serviceName,
            serviceType,
            provider,
            registerOption.TypeRegistrationIsStrict,
        )
        if nil != registerTypeErr {
            delete(instance.providers, serviceName)
            delete(instance.collectionPriorityByName, serviceName)
            return registerTypeErr
        }
    }

    return nil
}

func (instance *container) registerType(
    serviceName string,
    targetType reflect.Type,
    provider providerAny,
    isStrict bool,
) error {
    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        if true == isStrict {
            return exception.NewError(
                "could not register service by type",
                map[string]any{
                    "serviceName": serviceName,
                    "reason":      "canonical type is nil",
                },
                nil,
            )
        }

        return nil
    }

    if scopedServiceName, blocked := instance.scopedTypeRegistrationBlocksLocked(canonicalType); true == blocked {
        return exception.NewError(
            "service type is already registered as a scoped service type",
            map[string]any{
                "serviceName":       serviceName,
                "serviceType":       canonicalType.String(),
                "scopedServiceName": scopedServiceName,
            },
            ErrScopedServiceTypeAlreadyRegistered,
        )
    }

    existingServiceNames, exists := instance.typeRegistrationNamesByType[canonicalType]
    if true == exists && 0 < len(existingServiceNames) {
        if true == isStrict {
            return exception.NewError(
                "service type already registered",
                map[string]any{
                    "serviceName":         serviceName,
                    "serviceType":         canonicalType.String(),
                    "existingServiceName": existingServiceNames[0],
                },
                ErrServiceTypeAlreadyRegistered,
            )
        }

        instance.typeRegistrationNamesByType[canonicalType] = append(
            instance.typeRegistrationNamesByType[canonicalType],
            serviceName,
        )

        return nil
    }

    instance.typeProviders[canonicalType] = provider
    instance.typeRegistrationNamesByType[canonicalType] = []string{serviceName}

    return nil
}

type providerAny func(resolver containercontract.Resolver) (any, error)

var _ containercontract.Container = (*container)(nil)
