package container

import (
    "reflect"
    "strings"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
)

/* RegisterScoped adds a lazy service to this scope, closed with the scope. Name collisions at either lifetime are refused unless Replacing is declared. */
func (instance *scope) RegisterScoped(
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

    if true == strings.HasPrefix(serviceName, "service.") {
        return exception.NewError(
            "service is protected and cannot be registered as a scoped service",
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

    return instance.registerOnScope(
        serviceName,
        serviceType,
        wrappedProvider,
        options...,
    )
}

func (instance *scope) MustRegisterScoped(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    registerScopedErr := instance.RegisterScoped(serviceName, provider, options...)
    if nil != registerScopedErr {
        exception.Panic(
            exception.NewError(
                "failed to register scoped service on the scope",
                map[string]any{
                    "serviceName": serviceName,
                },
                registerScopedErr,
            ),
        )
    }
}

func (instance *scope) registerOnScope(
    serviceName string,
    serviceType reflect.Type,
    provider providerAny,
    options ...containercontract.RegisterOption,
) error {
    registerOption := applyRegisterServiceOptions(options)

    if 0 < len(registerOption.TeardownDependencyNames) || 0 < len(registerOption.TeardownDependencyTypes) {
        return exception.NewError(
            "a scoped registration cannot declare a teardown dependency",
            map[string]any{
                "serviceName": serviceName,
            },
            ErrScopedTeardownDependencyUnsupported,
        )
    }

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "refusedAt":   "entry",
                "serviceName": serviceName,
            },
            ErrScopeClosed,
        )
    }

    canonicalType := canonicalServiceType(serviceType)

    containerInstance.mutex.RLock()
    containerIsClosed := containerInstance.isClosed
    _, containerHasName := containerInstance.providers[serviceName]
    containerTypeServiceNames := []string(nil)
    if nil != canonicalType {
        containerTypeServiceNames = containerInstance.typeRegistrationNamesByType[canonicalType]
    }
    containerInstance.mutex.RUnlock()

    if true == containerIsClosed {
        return newContainerClosedError(serviceName)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.container.Load() {
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "refusedAt":   "lockHandOff",
                "serviceName": serviceName,
            },
            ErrScopeClosed,
        )
    }

    if _, exists := instance.ownProviders[serviceName]; true == exists {
        return exception.NewError(
            "scoped service already registered on the scope",
            map[string]any{
                "serviceName": serviceName,
            },
            ErrScopedServiceIdAlreadyRegistered,
        )
    }

    if false == registerOption.ReplacesContainerService {
        if _, exists := instance.plan.providers[serviceName]; true == exists {
            return exception.NewError(
                "service name is already registered in the container's scope plan",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrScopedServiceIdAlreadyRegistered,
            )
        }

        if true == containerHasName {
            return exception.NewError(
                "service name is already registered on the container",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrServiceIdAlreadyRegistered,
            )
        }
    }

    instance.ownProviders[serviceName] = provider

    if 0 != registerOption.CollectionPriority {
        instance.ownCollectionPriorityByName[serviceName] = registerOption.CollectionPriority
    }

    if true == registerOption.ReplacesContainerService {
        instance.ownReplacesContainerService[serviceName] = true
    }

    if true == registerOption.AlsoRegisterType {
        registerTypeErr := instance.registerTypeOnScopeLocked(
            canonicalType,
            containerTypeServiceNames,
            serviceName,
            provider,
            registerOption.TypeRegistrationIsStrict,
            registerOption.ReplacesContainerService,
        )
        if nil != registerTypeErr {
            delete(instance.ownProviders, serviceName)
            delete(instance.ownReplacesContainerService, serviceName)
            delete(instance.ownCollectionPriorityByName, serviceName)

            return registerTypeErr
        }
    }

    return nil
}

func (instance *scope) registerTypeOnScopeLocked(
    canonicalType reflect.Type,
    containerTypeServiceNames []string,
    serviceName string,
    provider providerAny,
    isStrict bool,
    replacesContainerService bool,
) error {
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

    if collidingType, collides := instance.scopedTypeIdentityCollision(canonicalType); true == collides {
        return exception.NewError(
            "scoped service type identity key collides with a different registered scoped type",
            map[string]any{
                "serviceName":  serviceName,
                "serviceType":  canonicalType.String(),
                "existingType": collidingType.String(),
                "identityKey":  typeIdentityKey(canonicalType),
            },
            nil,
        )
    }

    if false == replacesContainerService {
        if planServiceNames, exists := instance.plan.typeRegistrationNamesByType[canonicalType]; true == exists && 0 < len(planServiceNames) {
            return exception.NewError(
                "service type is already registered in the container's scope plan",
                map[string]any{
                    "serviceName":         serviceName,
                    "serviceType":         canonicalType.String(),
                    "existingServiceName": planServiceNames[0],
                },
                ErrScopedServiceTypeAlreadyRegistered,
            )
        }

        if 0 < len(containerTypeServiceNames) {
            return exception.NewError(
                "service type is already registered on the container",
                map[string]any{
                    "serviceName":         serviceName,
                    "serviceType":         canonicalType.String(),
                    "existingServiceName": containerTypeServiceNames[0],
                },
                ErrServiceTypeAlreadyRegistered,
            )
        }
    }

    existingServiceNames, exists := instance.ownTypeRegistrationNamesByType[canonicalType]
    if true == exists && 0 < len(existingServiceNames) {
        if true == isStrict {
            return exception.NewError(
                "scoped service type already registered on the scope",
                map[string]any{
                    "serviceName":         serviceName,
                    "serviceType":         canonicalType.String(),
                    "existingServiceName": existingServiceNames[0],
                },
                ErrScopedServiceTypeAlreadyRegistered,
            )
        }

        instance.ownTypeRegistrationNamesByType[canonicalType] = append(
            instance.ownTypeRegistrationNamesByType[canonicalType],
            serviceName,
        )

        return nil
    }

    instance.ownTypeProviders[canonicalType] = provider
    instance.ownTypeRegistrationNamesByType[canonicalType] = []string{serviceName}

    return nil
}

func (instance *scope) scopedTypeIdentityCollision(canonicalType reflect.Type) (reflect.Type, bool) {
    identityKey := typeIdentityKey(canonicalType)

    for registeredType := range instance.ownTypeRegistrationNamesByType {
        if registeredType == canonicalType {
            continue
        }

        if typeIdentityKey(registeredType) == identityKey {
            return registeredType, true
        }
    }

    return nil, false
}

func (instance *scope) scopedProviderByName(serviceName string) (providerAny, bool) {
    instance.mutex.RLock()
    provider, exists := instance.ownProviders[serviceName]
    instance.mutex.RUnlock()

    if true == exists {
        return provider, true
    }

    provider, exists = instance.plan.providers[serviceName]

    return provider, exists
}

func (instance *scope) scopedTypeRegistrationNames(canonicalType reflect.Type) ([]string, bool) {
    instance.mutex.RLock()
    serviceNames, exists := instance.ownTypeRegistrationNamesByType[canonicalType]
    instance.mutex.RUnlock()

    if true == exists && 0 < len(serviceNames) {
        return serviceNames, true
    }

    serviceNames, exists = instance.plan.typeRegistrationNamesByType[canonicalType]

    return serviceNames, exists && 0 < len(serviceNames)
}

func (instance *scope) scopedProviderByType(canonicalType reflect.Type) (providerAny, bool) {
    instance.mutex.RLock()
    provider, exists := instance.ownTypeProviders[canonicalType]
    instance.mutex.RUnlock()

    if true == exists {
        return provider, true
    }

    provider, exists = instance.plan.typeProviders[canonicalType]

    return provider, exists
}
