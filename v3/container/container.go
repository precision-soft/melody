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
    /* distinct types can share an identity key, so each key records its type and a second, different type under it is refused at registration */
    typeIdentityKeyToType map[string]reflect.Type
    dependencyGraph       map[string]map[string]struct{}
    /* the name-keyed instances the container built, as opposed to installed overrides; an override replacing one leaves it to the teardown through replacedBuiltInstances */
    builtServiceNames      map[string]struct{}
    replacedBuiltInstances []any
    /* the order the teardown nodes came into being, written wherever a value enters the instance maps and read by the teardown alone */
    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
    /* the providers the scopes own, never reachable from a resolution against the container */
    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedCollectionPriorityByName    map[string]int
    scopedReplacesContainerService    map[string]bool
    /* the declared return type of each provider, for introspection without building */
    providerServiceTypeByName       map[string]reflect.Type
    scopedProviderServiceTypeByName map[string]reflect.Type
    /* the published plan new scopes are bound to by reference; a registration clears it and the next scope rebuilds it */
    scopePlanPointer atomic.Pointer[scopePlan]
    isClosed         bool
    /* the hand-declared edges, kept beside the graph they were written into so a refusal can quote them */
    declaredTeardownEdges []declaredTeardownEdge
    /* what each built service was seen to hold, recorded at construction while the waves are armed; heldIdentitiesByValue keys the same record by the value walked */
    heldIdentitiesByNodeKey map[string][]heldPointer
    heldIdentitiesByValue   map[pointerIdentity][]heldPointer
    /* teardownInWaves is the application's assertion that the teardown graph is complete; off by default */
    teardownInWaves bool
    /* teardownFinished is the second closing state: isClosed refuses new creations for the whole teardown, and teardownFinished refuses every resolution once the last Close returned, so a service's own Close can still resolve its dependencies */
    teardownFinished bool
    closeErr         error
    closeOnce        sync.Once
    /* teardownDeadline is the record of a teardown that ran past its deadline, nil otherwise; TeardownDeadlineOverrun answers it */
    teardownDeadline exceptioncontract.Context
}

/* declaredTeardownEdge is one hand-written ordering: the declaring service, the node it named and the spelling it used. declaredNameEdge and declaredTypeEdge build the two spellings. */
type declaredTeardownEdge struct {
    dependentServiceName string
    dependencyNodeKey    string
    dependencySpelling   string
}

func declaredNameEdge(serviceName string, dependencyName string) declaredTeardownEdge {
    return declaredTeardownEdge{
        dependentServiceName: serviceName,
        dependencyNodeKey:    containerNameNodeKey(dependencyName),
        dependencySpelling:   dependencyName,
    }
}

func declaredTypeEdge(serviceName string, dependencyType reflect.Type) declaredTeardownEdge {
    return declaredTeardownEdge{
        dependentServiceName: serviceName,
        dependencyNodeKey:    containerTypeNodeKey(dependencyType),
        dependencySpelling:   dependencyType.String(),
    }
}

func (instance *container) Get(serviceName string) (any, error) {
    resolver := newResolverContext(instance)

    return resolver.Get(serviceName)
}

/* MustGet panics with the resolution failure whole, the service name in its context, as MustGetByType does. */
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

    /* the lookup is canonical, as the registrations and GetByType are */
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

/* OverrideInstance installs a value under a registered name, and the container closes it at teardown, unlike a scope, whose overrides stay their installer's. A value shared with another container in the same process is closed for all of them; install a wrapper whose Close does nothing in that case. */
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

    /* an override after Close has enumerated the instances would never be closed, so it is refused */
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

    /* the override reaches every type this name is registered under, so a value the registered type cannot hold is refused before anything is written */
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

    /* a built instance being replaced goes to the graveyard, closed once by the teardown after everything that still uses it; an evicted override stays its installer's */
    if replacedValue, replacedExists := instance.instances[serviceName]; true == replacedExists {
        if _, wasBuilt := instance.builtServiceNames[serviceName]; true == wasBuilt {
            instance.replacedBuiltInstances = append(instance.replacedBuiltInstances, replacedValue)
        }

        /* the walk's memo of the evicted value goes with it, so its collaborators can be collected */
        if evictedKey, hasEvictedKey := pointerKeyOf(replacedValue); true == hasEvictedKey && nil != instance.heldIdentitiesByValue {
            delete(instance.heldIdentitiesByValue, evictedKey)
        }
    }
    delete(instance.builtServiceNames, serviceName)

    instance.instances[serviceName] = value
    instance.recordCreationOrderLocked(containerNameNodeKey(serviceName))

    /* what the installed value holds is recorded under the same node, replacing the evicted value's record */
    instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)

    /* the override reaches only the types this name is registered under: a type another service owns must not learn it, and a free type is left out because a value-type service under a second node would close twice */
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

/* serviceNamesForRegisteredType lists the service names a type is registered under. */
func (instance *container) serviceNamesForRegisteredType(canonicalType reflect.Type) []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.typeRegistrationNamesByType[canonicalType]
}

/* registeredTypesForServiceName answers every type the name is registered under; the scope calls it before taking its own lock, container before scope being the only lock order. */
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

/* ServiceDescriptions describes every name either lifetime knows without running a provider, the type read from the built instance or from the provider's declared return type. */
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

    /* a closed container refuses a registration, as the scoped registrar does */
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

    /* the declared teardown edges are validated before anything is written */
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

        /* an edge from a registration to the type it files itself would be a self-edge the walk drops, so it is refused; an edge to a type another service filed is admitted */
        if true == registerOption.AlsoRegisterType && nil != serviceType && containerTypeNodeKey(serviceType) == containerTypeNodeKey(dependencyType) {
            return exception.NewError(
                "a service cannot declare a teardown dependency on its own registered type",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyIsSelf,
            )
        }
    }

    /* once the waves are armed, each new declared edge is validated at its registration */
    if true == instance.teardownInWaves {
        for _, dependencyName := range registerOption.TeardownDependencyNames {
            if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredNameEdge(serviceName, dependencyName)); nil != refusalErr {
                return refusalErr
            }
        }

        for _, dependencyType := range registerOption.TeardownDependencyTypes {
            if refusalErr := instance.refuseDeclaredTeardownEdgeLocked(declaredTypeEdge(serviceName, dependencyType)); nil != refusalErr {
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

    /* the declared edges are written last, once the registration cannot fail, since the graph is never pruned. A name edge goes into the resolution graph; a type edge is kept beside it and expanded per plan */
    for _, dependencyName := range registerOption.TeardownDependencyNames {
        instance.recordDeclaredTeardownEdgeLocked(declaredNameEdge(serviceName, dependencyName))
    }

    for _, dependencyType := range registerOption.TeardownDependencyTypes {
        instance.recordDeclaredTeardownEdgeLocked(declaredTypeEdge(serviceName, dependencyType))
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

        /* under the waves, a second name under a type a declaration names is refused at the registration that would make the declaration ambiguous */
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

/* recordTypeIdentityKeyLocked refuses a type whose identity key a different type already claimed, since the two would share a creation-guard key and a close node. */
func (instance *container) recordTypeIdentityKeyLocked(serviceName string, canonicalType reflect.Type) error {
    identityKey := typeIdentityKey(canonicalType)

    existingType, exists := instance.typeIdentityKeyToType[identityKey]
    if true == exists && existingType != canonicalType {
        return exception.NewError(
            "service type identity key collides with a different registered type",
            map[string]any{
                "serviceName":  serviceName,
                "serviceType":  canonicalType.String(),
                "existingType": existingType.String(),
                "identityKey":  identityKey,
            },
            nil,
        )
    }

    instance.typeIdentityKeyToType[identityKey] = canonicalType

    return nil
}

type providerAny func(resolver containercontract.Resolver) (any, error)

var (
    _ containercontract.Container       = (*container)(nil)
    _ containercontract.ScopedRegistrar = (*container)(nil)
    _ parallelTeardownArmer             = (*container)(nil)
    _ teardownPlanner                   = (*container)(nil)
    _ containercontract.ContextCloser   = (*container)(nil)
    _ closedContainerChecker            = (*container)(nil)
)
