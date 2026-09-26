package container

import (
    "fmt"
    "reflect"
    "sort"
    "strings"
    "sync"
    "sync/atomic"

    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
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
        typeIdentityKeyToType:       make(map[string]reflect.Type),
        dependencyGraph:             make(map[string]map[string]struct{}),
        builtServiceNames:           make(map[string]struct{}),
        creationOrderByNodeKey:      make(map[string]int),

        scopedProviders:                   make(map[string]providerAny),
        scopedTypeProviders:               make(map[reflect.Type]providerAny),
        scopedTypeRegistrationNamesByType: make(map[reflect.Type][]string),
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
    /* distinct types can share an identity key string (pointer-to-unnamed-composite types drop their package path), and two types behind one key mean false cycles at resolution and merged nodes at close, so every type registration records its key here and a second, different type under the same key is refused at the registration that declares it. */
    typeIdentityKeyToType map[string]reflect.Type
    dependencyGraph       map[string]map[string]struct{}
    /* the name-keyed instances the container itself built, as opposed to installed overrides: an override replacing one of them leaves a value only the container holds, so the teardown closes it through replacedBuiltInstances. An evicted installed override belongs to whoever installed it. */
    builtServiceNames      map[string]struct{}
    replacedBuiltInstances []any
    /* the order the teardown nodes came into being, written wherever a value enters the instance maps and read only by the teardown, to break the ties the dependency graph leaves open (see closesBefore). */
    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
    /* the scoped registrations, kept apart from the container's own so nothing resolved against the container reaches them; the immutable scope plan is built from them, and they are never read on the request path. */
    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedReplacesContainerService    map[string]bool
    /* the declared return type of each provider, for introspection: the wrapped provider erases the signature. */
    providerServiceTypeByName       map[string]reflect.Type
    scopedProviderServiceTypeByName map[string]reflect.Type
    /* the published plan every new scope is bound to by reference. A registration clears it and the next scope rebuilds it, so creating a scope costs one atomic load once boot has settled. */
    scopePlanPointer atomic.Pointer[scopePlan]
    isClosed         bool
    /* teardownFinished is the second closing state. isClosed is set before the first service Close runs and refuses every new creation; teardownFinished is set after the last Close returns and refuses every resolution. Between the two the memoized instances are still served, so a service's own Close can resolve what it depends on, the logger among them. */
    teardownFinished bool
    closeErr         error
    closeOnce        sync.Once
}

func (instance *container) Get(serviceName string) (any, error) {
    resolver := newResolverContext(instance)

    return resolver.Get(serviceName)
}

/* MustGet resolves serviceName and panics on failure: a melody error panics out whole with the service name written into its context, so it keeps its log level, its already-logged mark and its capture stack. */
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

    /* the lookup is canonical because the registrations are: a provider returning *T is filed under *T and GetByType canonicalises before it looks, so Has and Get agree. */
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

/* OverrideInstance installs a value under a registered name, and the container closes the installed value at its teardown, unlike a scope, whose overrides belong to their installer unless ClosedWithScope says otherwise. A value shared with a second container in the same process is therefore closed by the first teardown; install a wrapper whose Close does nothing and keep the real handle where it belongs. */
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

    /* an override landing after Close has enumerated the instances would be served and closed by nobody, so it is refused; the read paths keep serving what was built. */
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

    /* the override propagates to every type this name is registered under, and a type-keyed resolution serves it without a re-check, so a value a registered type cannot hold is refused before anything is written. */
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

    /* a replaced instance the container built is closed by the teardown, through the graveyard rather than inline: a provider may hand the same pointer to several names, and the teardown's identity marks close each instance once, after everything that still uses it. An evicted override stays its installer's. */
    if replacedValue, replacedExists := instance.instances[serviceName]; true == replacedExists {
        if _, wasBuilt := instance.builtServiceNames[serviceName]; true == wasBuilt {
            instance.replacedBuiltInstances = append(instance.replacedBuiltInstances, replacedValue)
        }
    }
    delete(instance.builtServiceNames, serviceName)

    instance.instances[serviceName] = value
    instance.recordCreationOrderLocked("service:" + serviceName)

    /* the override propagates only to the types this name is registered under: a type another service owns must not learn it, and a free type is left out (unlike the scope, which exposes it) because a value-type service filed under a second, uncollapsed node would close twice at teardown. */
    for registeredType, registeredServiceNames := range instance.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                instance.typeInstances[registeredType] = value
                instance.recordCreationOrderLocked("type:" + typeIdentityKey(registeredType))
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

/* serviceNamesForRegisteredType lists the service names a type is registered under, so a caller deciding whether a type is free can see who, if anyone, already claims it. */
func (instance *container) serviceNamesForRegisteredType(canonicalType reflect.Type) []string {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.typeRegistrationNamesByType[canonicalType]
}

/* registeredTypesForServiceName answers every type the name is registered under, for the scope override that propagates to them; the scope calls it before taking its own lock, container-then-scope being the only order the two locks are ever taken in. */
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

/* ServiceDescriptions describes every name either lifetime knows without running a provider: the container's registrations and the scoped ones Names() cannot see, with the type read from the built instance when one exists and from the provider's declared return type otherwise. The introspection command lists through it, so listing a container does not build it. */
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

    /* a closed container refuses a registration, as the scoped registrar does: the creation guard would refuse every resolution of it. */
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

    instance.providers[serviceName] = provider
    if nil != serviceType {
        instance.providerServiceTypeByName[serviceName] = serviceType
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

/* recordTypeIdentityKeyLocked refuses a type whose identity key a different type already claimed: the pair would share a creation-guard key and a close node while holding two instances. The refusal lands at the registration that declares the second type. */
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

/* Container answers the container itself, so a caller holding the container directly as its resolver satisfies the same ContainerCarrier door a scope or a resolution context answers. */
func (instance *container) Container() containercontract.Container {
    return instance
}

var (
    _ containercontract.Container        = (*container)(nil)
    _ containercontract.ScopedRegistrar  = (*container)(nil)
    _ containercontract.ContainerCarrier = (*container)(nil)
)
