package container

import (
    "fmt"
    "reflect"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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
        typeIdentityKeyToType:       make(map[string]reflect.Type),
        dependencyGraph:             make(map[string]map[string]struct{}),
        builtServiceNames:           make(map[string]struct{}),
        creationOrderByNodeKey:      make(map[string]int),

        scopedProviders:                   make(map[string]providerAny),
        scopedTypeProviders:               make(map[reflect.Type]providerAny),
        scopedTypeRegistrationNamesByType: make(map[reflect.Type][]string),
        scopedCollectionPriorityByName:    make(map[string]int),
        scopedReplacesContainerService:    make(map[string]bool),

        providerServiceTypeByName:       make(map[string]reflect.Type),
        scopedProviderServiceTypeByName: make(map[string]reflect.Type),
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

    typeIdentityKeyToType map[string]reflect.Type
    dependencyGraph       map[string]map[string]struct{}

    builtServiceNames      map[string]struct{}
    replacedBuiltInstances []any

    creationOrderByNodeKey map[string]int
    creationOrderCounter   int

    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedCollectionPriorityByName    map[string]int
    scopedReplacesContainerService    map[string]bool

    providerServiceTypeByName       map[string]reflect.Type
    scopedProviderServiceTypeByName map[string]reflect.Type

    scopePlanPointer atomic.Pointer[scopePlan]
    isClosed         bool

    declaredTeardownEdges []declaredTeardownEdge

    heldIdentitiesByNodeKey map[string][]heldPointer
    heldIdentitiesByValue   map[pointerIdentity][]heldPointer

    teardownInWaves bool

    teardownFinished bool
    closeErr         error
    closeOnce        sync.Once

    teardownDeadline exceptioncontract.Context
}

type declaredTeardownEdge struct {
    dependentServiceName string
    dependencyNodeKey    string
    dependencySpelling   string
}

func (instance *container) Get(serviceName string) (any, error) {
    resolver := newResolverContext(instance)

    return resolver.Get(serviceName)
}

/* MustGet delegates to the resolver context the way MustGetByType always has, so the two doors dress a failure identically: a melody error panics out whole with the service name written into its context, keeping the log level, the already-logged mark and the capture stack a rebuilt wrapper would shed. */
func (instance *container) MustGet(serviceName string) any {
    resolver := newResolverContext(instance)

    return resolver.MustGet(serviceName)
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

/* OverrideInstance transfers teardown ownership of a registered value to the container, unlike borrowed scope overrides. A value shared across containers needs an explicitly non-owning wrapper; there is no container override ownership opt-out. */
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

    if true == instance.isClosed {
        return newContainerClosedError(serviceName)
    }

    if _, exists := instance.providers[serviceName]; false == exists {
        return exception.NewError(
            "service not registered in container",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

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

    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName != registeredServiceName {
                continue
            }

            if false == overrideValueFitsRegisteredType(valueType, registeredType) {
                return exception.NewError(
                    "override value is not assignable to the registered service type",
                    map[string]any{
                        "serviceName":    serviceName,
                        "registeredType": registeredType.String(),
                        "valueType":      valueType.String(),
                    },
                    nil,
                )
            }

            break
        }
    }

    if replacedValue, replacedExists := instance.instances[serviceName]; true == replacedExists {
        if _, wasBuilt := instance.builtServiceNames[serviceName]; true == wasBuilt {
            instance.replacedBuiltInstances = append(instance.replacedBuiltInstances, replacedValue)
        }

        if evictedKey, hasEvictedKey := pointerKeyOf(replacedValue); true == hasEvictedKey && nil != instance.heldIdentitiesByValue {
            delete(instance.heldIdentitiesByValue, evictedKey)
        }
    }
    delete(instance.builtServiceNames, serviceName)

    instance.instances[serviceName] = value
    instance.recordCreationOrderLocked(containerNameNodeKey(serviceName))

    instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)

    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                instance.typeInstances[registeredType] = value
                instance.recordCreationOrderLocked(containerTypeNodeKey(registeredType))
                instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value)
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

func (instance *container) serviceNamesForRegisteredType(canonicalType reflect.Type) []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.typeRegistrationNamesByType[canonicalType]
}

func (instance *container) registeredTypesForServiceName(serviceName string) []reflect.Type {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    var registeredTypes []reflect.Type

    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                registeredTypes = append(registeredTypes, registeredType)
                break
            }
        }
    }

    return registeredTypes
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

/* ServiceDescriptions answers what the container can say WITHOUT running a provider: every name either lifetime knows — the container's own registrations and the scoped ones Names() cannot see — with the type read from the built instance when one exists and from the provider's declared return type otherwise. The instances map is walked beside the providers although every override requires a registration, so a name only ever built or replaced stays described through the same door. It is what the introspection command lists through, so listing a container stops meaning building it. */
func (instance *container) ServiceDescriptions() []containercontract.ServiceDescription {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    descriptions := make([]containercontract.ServiceDescription, 0, len(instance.providers)+len(instance.instances)+len(instance.scopedProviders))

    containerNames := make(map[string]struct{}, len(instance.providers)+len(instance.instances))
    for serviceName := range instance.providers {
        containerNames[serviceName] = struct{}{}
    }
    for serviceName := range instance.instances {
        containerNames[serviceName] = struct{}{}
    }

    for serviceName := range containerNames {
        description := containercontract.ServiceDescription{
            Name:     serviceName,
            Lifetime: containercontract.ServiceLifetimeContainer,
        }

        if builtInstance, isBuilt := instance.instances[serviceName]; true == isBuilt {
            description.IsBuilt = true
            description.TypeName = fmt.Sprintf("%T", builtInstance)
        } else if declaredType, hasDeclaredType := instance.providerServiceTypeByName[serviceName]; true == hasDeclaredType {
            description.TypeName = declaredType.String()
        }

        descriptions = append(descriptions, description)
    }

    for serviceName := range instance.scopedProviders {
        description := containercontract.ServiceDescription{
            Name:     serviceName,
            Lifetime: containercontract.ServiceLifetimeScoped,
        }

        if declaredType, hasDeclaredType := instance.scopedProviderServiceTypeByName[serviceName]; true == hasDeclaredType {
            description.TypeName = declaredType.String()
        }

        descriptions = append(descriptions, description)
    }

    sort.Slice(descriptions, func(leftIndex int, rightIndex int) bool {
        if descriptions[leftIndex].Lifetime != descriptions[rightIndex].Lifetime {
            return descriptions[leftIndex].Lifetime < descriptions[rightIndex].Lifetime
        }

        return descriptions[leftIndex].Name < descriptions[rightIndex].Name
    })

    return descriptions
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

    if true == instance.isClosed {
        return newContainerClosedError(serviceName)
    }

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

    for _, dependencyName := range registerOption.TeardownDependencyNames {
        if "" == dependencyName {
            return exception.NewError(
                "a teardown dependency name is required",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyNameIsRequired,
            )
        }

        if serviceName == dependencyName {
            return exception.NewError(
                "a service cannot declare a teardown dependency on itself",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyIsSelf,
            )
        }
    }

    for _, dependencyType := range registerOption.TeardownDependencyTypes {
        if nil == dependencyType {
            return exception.NewError(
                "a teardown dependency type is required",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyTypeIsRequired,
            )
        }

        if nil != serviceType && containerTypeNodeKey(serviceType) == containerTypeNodeKey(dependencyType) {
            return exception.NewError(
                "a service cannot declare a teardown dependency on its own registered type",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyIsSelf,
            )
        }
    }

    identityTypes := append([]reflect.Type(nil), registerOption.TeardownDependencyTypes...)
    if true == registerOption.AlsoRegisterType && nil != serviceType {
        identityTypes = append(identityTypes, serviceType)
    }
    if identityErr := instance.validateTypeIdentitiesLocked(serviceName, identityTypes); nil != identityErr {
        return identityErr
    }

    if true == instance.teardownInWaves {
        for _, dependencyName := range registerOption.TeardownDependencyNames {
            declaredEdge := declaredTeardownEdge{
                dependentServiceName: serviceName,
                dependencyNodeKey:    containerNameNodeKey(dependencyName),
                dependencySpelling:   dependencyName,
            }

            if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredEdge); nil != refusalErr {
                return refusalErr
            }
        }

        for _, dependencyType := range registerOption.TeardownDependencyTypes {
            declaredEdge := declaredTeardownEdge{
                dependentServiceName: serviceName,
                dependencyNodeKey:    containerTypeNodeKey(dependencyType),
                dependencySpelling:   dependencyType.String(),
            }

            if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredEdge); nil != refusalErr {
                return refusalErr
            }
        }
    }

    instance.providers[serviceName] = provider
    if nil != serviceType {
        instance.providerServiceTypeByName[serviceName] = serviceType
    }

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
            delete(instance.providerServiceTypeByName, serviceName)
            delete(instance.collectionPriorityByName, serviceName)
            return registerTypeErr
        }
    }

    for _, dependencyType := range registerOption.TeardownDependencyTypes {
        canonicalType := canonicalServiceType(dependencyType)
        instance.typeIdentityKeyToType[typeIdentityKey(canonicalType)] = canonicalType
    }

    for _, dependencyName := range registerOption.TeardownDependencyNames {
        instance.recordDeclaredTeardownEdgeLocked(serviceName, containerNameNodeKey(dependencyName), dependencyName)
    }

    for _, dependencyType := range registerOption.TeardownDependencyTypes {
        instance.recordDeclaredTeardownEdgeLocked(serviceName, containerTypeNodeKey(dependencyType), dependencyType.String())
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

    if identityCollisionErr := instance.recordTypeIdentityKeyLocked(serviceName, canonicalType); nil != identityCollisionErr {
        return identityCollisionErr
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

        if true == instance.teardownInWaves {
            typeNodeKey := containerTypeNodeKey(canonicalType)

            for _, declaredEdge := range instance.declaredTeardownEdges {
                if declaredEdge.dependencyNodeKey != typeNodeKey {
                    continue
                }

                return exception.NewError(
                    "a second service cannot be registered under a type a teardown dependency already names once the parallel teardown is armed, because the declaration would then name a set and order nothing",
                    map[string]any{
                        "serviceName":         serviceName,
                        "serviceType":         canonicalType.String(),
                        "existingServiceName": existingServiceNames[0],
                        "declaredBy":          declaredEdge.dependentServiceName,
                    },
                    ErrTeardownDependencyTypeIsAmbiguous,
                )
            }
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

func (instance *container) recordTypeIdentityKeyLocked(serviceName string, canonicalType reflect.Type) error {
    if identityErr := instance.validateTypeIdentitiesLocked(serviceName, []reflect.Type{canonicalType}); nil != identityErr {
        return identityErr
    }
    instance.typeIdentityKeyToType[typeIdentityKey(canonicalType)] = canonicalType
    return nil
}

func (instance *container) validateTypeIdentitiesLocked(serviceName string, serviceTypes []reflect.Type) error {
    pending := make(map[string]reflect.Type, len(serviceTypes))
    for _, serviceType := range serviceTypes {
        canonicalType := canonicalServiceType(serviceType)
        identityKey := typeIdentityKey(canonicalType)
        existingType, exists := pending[identityKey]
        if false == exists {
            existingType, exists = instance.typeIdentityKeyToType[identityKey]
        }
        if true == exists && existingType != canonicalType {
            return exception.NewError(
                "service type identity key collides with a different registered or declared type",
                map[string]any{
                    "serviceName":  serviceName,
                    "serviceType":  canonicalType.String(),
                    "existingType": existingType.String(),
                    "identityKey":  identityKey,
                },
                nil,
            )
        }
        pending[identityKey] = canonicalType
    }
    return nil
}

type providerAny func(resolver containercontract.Resolver) (any, error)

var (
    _ containercontract.Container       = (*container)(nil)
    _ containercontract.ScopedRegistrar = (*container)(nil)
    _ parallelTeardownArmer             = (*container)(nil)
    _ teardownPlanner                   = (*container)(nil)
    _ contextCloser                     = (*container)(nil)
    _ closedContainerChecker            = (*container)(nil)
)
