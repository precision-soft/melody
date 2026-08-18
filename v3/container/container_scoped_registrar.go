package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* scopePlan is the set of scoped registrations a scope is created against. It is built once and never written to afterwards, so every scope holds a reference to the same value instead of a copy of the maps: creating a scope is a pointer load, whatever the size of the plan.

A registration made after a scope already exists rebuilds the plan for the scopes created next; the ones already running keep the plan they were created with. That is the only reading under which a scope's contents do not change under its own feet halfway through a request. */
type scopePlan struct {
    providers                   map[string]providerAny
    typeProviders               map[reflect.Type]providerAny
    typeRegistrationNamesByType map[reflect.Type][]string
    collectionPriorityByName    map[string]int
}

func newEmptyScopePlan() *scopePlan {
    return &scopePlan{
        providers:                   make(map[string]providerAny),
        typeProviders:               make(map[reflect.Type]providerAny),
        typeRegistrationNamesByType: make(map[reflect.Type][]string),
        collectionPriorityByName:    make(map[string]int),
    }
}

func (instance *container) RegisterScoped(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is required to register a scoped service",
            nil,
            nil,
        )
    }

    if nil == provider {
        return exception.NewError(
            "the provider is required to register a scoped service",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    wrappedProvider, serviceType, reflectedProviderErr := reflectedProvider(serviceName, provider)
    if nil != reflectedProviderErr {
        return reflectedProviderErr
    }

    return instance.registerScoped(
        serviceName,
        serviceType,
        wrappedProvider,
        options...,
    )
}

func (instance *container) MustRegisterScoped(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    registerScopedErr := instance.RegisterScoped(serviceName, provider, options...)
    if nil != registerScopedErr {
        exception.Panic(exception.FromError(registerScopedErr))
    }
}

/* registerScoped records a provider the scopes of this container own. The registration is refused when the container already holds the name, unless the caller declared Replacing: a name that answers with a process singleton outside a scope and with a per-request service inside one is exactly the ambiguity the two lifetimes exist to keep apart, and it must be admitted deliberately rather than fall out of the order the modules registered in.

There is no hard seal after boot. A late registration invalidates the published plan and the scopes created next see it, while the ones already running keep the plan they were created with — which is what lets a test register a scoped service on a container it has just built. */
func (instance *container) registerScoped(
    serviceName string,
    serviceType reflect.Type,
    provider providerAny,
    options ...containercontract.RegisterOption,
) error {
    registerOption := applyRegisterServiceOptions(options)

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.isClosed {
        return newContainerClosedError(serviceName)
    }

    if _, exists := instance.scopedProviders[serviceName]; true == exists {
        return exception.NewError(
            "scoped service already registered",
            map[string]any{
                "serviceName": serviceName,
            },
            ErrScopedServiceIdAlreadyRegistered,
        )
    }

    if _, exists := instance.providers[serviceName]; true == exists {
        if false == registerOption.ReplacesContainerService {
            return exception.NewError(
                "service name is already registered on the container",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrServiceIdAlreadyRegistered,
            )
        }
    }

    instance.scopedProviders[serviceName] = provider

    if true == registerOption.ReplacesContainerService {
        instance.scopedReplacesContainerService[serviceName] = true
    }

    if 0 != registerOption.CollectionPriority {
        instance.scopedCollectionPriorityByName[serviceName] = registerOption.CollectionPriority
    }

    if true == registerOption.AlsoRegisterType {
        registerScopedTypeErr := instance.registerScopedType(
            serviceName,
            serviceType,
            provider,
            registerOption.TypeRegistrationIsStrict,
            registerOption.ReplacesContainerService,
        )
        if nil != registerScopedTypeErr {
            delete(instance.scopedProviders, serviceName)
            delete(instance.scopedReplacesContainerService, serviceName)
            delete(instance.scopedCollectionPriorityByName, serviceName)

            return registerScopedTypeErr
        }
    }

    instance.scopePlanPointer.Store(nil)

    return nil
}

func (instance *container) registerScopedType(
    serviceName string,
    targetType reflect.Type,
    provider providerAny,
    isStrict bool,
    replacesContainerService bool,
) error {
    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        if true == isStrict {
            return exception.NewError(
                "could not register scoped service by type",
                map[string]any{
                    "serviceName": serviceName,
                    "reason":      "canonical type is nil",
                },
                nil,
            )
        }

        return nil
    }

    /* the cross-level check does not care whether the registration is strict: strictness decides whether a second name may share a type at the SAME lifetime, while a type answering with a singleton outside a scope and with a per-request service inside one is the ambiguity itself, whichever way it was declared. */
    containerServiceNames, containerTypeExists := instance.typeRegistrationNamesByType[canonicalType]
    if true == containerTypeExists && 0 < len(containerServiceNames) && false == replacesContainerService {
        return exception.NewError(
            "service type is already registered on the container",
            map[string]any{
                "serviceName":         serviceName,
                "serviceType":         canonicalType.String(),
                "existingServiceName": containerServiceNames[0],
            },
            ErrServiceTypeAlreadyRegistered,
        )
    }

    existingServiceNames, exists := instance.scopedTypeRegistrationNamesByType[canonicalType]
    if true == exists && 0 < len(existingServiceNames) {
        if true == isStrict {
            return exception.NewError(
                "scoped service type already registered",
                map[string]any{
                    "serviceName":         serviceName,
                    "serviceType":         canonicalType.String(),
                    "existingServiceName": existingServiceNames[0],
                },
                ErrScopedServiceTypeAlreadyRegistered,
            )
        }

        instance.scopedTypeRegistrationNamesByType[canonicalType] = append(
            instance.scopedTypeRegistrationNamesByType[canonicalType],
            serviceName,
        )

        return nil
    }

    instance.scopedTypeProviders[canonicalType] = provider
    instance.scopedTypeRegistrationNamesByType[canonicalType] = []string{serviceName}

    return nil
}

/* scopedRegistrationBlocksLocked reports whether a scoped registration stands in the way of taking this name on the container. The check is what makes the refusal independent of the order the two levels registered in: without it, whether a collision is reported at all would depend on which module ran first. */
func (instance *container) scopedRegistrationBlocksLocked(serviceName string) bool {
    if _, exists := instance.scopedProviders[serviceName]; false == exists {
        return false
    }

    return false == instance.scopedReplacesContainerService[serviceName]
}

func (instance *container) scopedTypeRegistrationBlocksLocked(canonicalType reflect.Type) (string, bool) {
    existingServiceNames, exists := instance.scopedTypeRegistrationNamesByType[canonicalType]
    if false == exists || 0 == len(existingServiceNames) {
        return "", false
    }

    for _, existingServiceName := range existingServiceNames {
        if false == instance.scopedReplacesContainerService[existingServiceName] {
            return existingServiceName, true
        }
    }

    return "", false
}

/* scopePlanForNewScope yields the plan a scope created right now is bound to. After boot the pointer never changes again, so the common path is a single atomic load and the container mutex is never touched on the request path. */
func (instance *container) scopePlanForNewScope() *scopePlan {
    plan := instance.scopePlanPointer.Load()
    if nil != plan {
        return plan
    }

    return instance.rebuildScopePlan()
}

func (instance *container) rebuildScopePlan() *scopePlan {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* another goroutine may have published a plan between the failed load and this lock; republishing an identical one would be harmless but pointless. */
    plan := instance.scopePlanPointer.Load()
    if nil != plan {
        return plan
    }

    plan = &scopePlan{
        providers:                   make(map[string]providerAny, len(instance.scopedProviders)),
        typeProviders:               make(map[reflect.Type]providerAny, len(instance.scopedTypeProviders)),
        typeRegistrationNamesByType: make(map[reflect.Type][]string, len(instance.scopedTypeRegistrationNamesByType)),
        collectionPriorityByName:    make(map[string]int, len(instance.scopedCollectionPriorityByName)),
    }

    for serviceName, provider := range instance.scopedProviders {
        plan.providers[serviceName] = provider
    }

    for canonicalType, provider := range instance.scopedTypeProviders {
        plan.typeProviders[canonicalType] = provider
    }

    for canonicalType, serviceNames := range instance.scopedTypeRegistrationNamesByType {
        copiedServiceNames := make([]string, len(serviceNames))
        copy(copiedServiceNames, serviceNames)

        plan.typeRegistrationNamesByType[canonicalType] = copiedServiceNames
    }

    for serviceName, priority := range instance.scopedCollectionPriorityByName {
        plan.collectionPriorityByName[serviceName] = priority
    }

    instance.scopePlanPointer.Store(plan)

    return plan
}

var _ containercontract.ScopedRegistrar = (*container)(nil)
