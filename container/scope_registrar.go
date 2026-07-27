package container

import (
    "reflect"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
)

/* RegisterScoped adds a service to this one scope, layered over the plan its container was booted with. It is the rare case — a scoped service is normally declared at boot through the container, so every scope gets it — and exists for the caller that only knows what to build once the request is in front of it.

What it registers is built on the first Get through this scope and closed when the scope closes, exactly like a planned scoped service. It is refused when the name is already taken at either level, unless Replacing was declared: a name that answers differently depending on where it is asked from has to be made ambiguous deliberately. */
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

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    canonicalType := canonicalServiceType(serviceType)

    /* @important the container is asked first and its lock is released before the scope's is taken, never held across it. Has holds the scope lock and reaches for the container's, so holding the container's while reaching for the scope's closes a cycle the moment a writer queues on the container mutex — Go's RWMutex makes a pending writer block new readers, so the two would wait on each other through it. The window this leaves is a container registration landing between the two locks, which is a registration racing a registration and has no ordering to preserve anyway. */
    containerInstance.mutex.RLock()
    _, containerHasName := containerInstance.providers[serviceName]
    containerTypeServiceNames := []string(nil)
    if nil != canonicalType {
        containerTypeServiceNames = containerInstance.typeRegistrationNamesByType[canonicalType]
    }
    containerInstance.mutex.RUnlock()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.container.Load() {
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
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

/* scopedProviderByName yields the provider this scope would build the name from: its own registration first, then the plan its container was booted with. The plan is immutable and shared, so it is read without a lock; only the scope's own registrations need one. */
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

/* scopedTypeRegistrationNames yields the scoped services registered under a type, the scope's own before the plan's. A name found here resolves through the named path, so a scoped service reached by type and by name is one instance. */
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

var _ containercontract.ScopedRegistrar = (*scope)(nil)
