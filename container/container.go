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
    /* the identity key is a string, and distinct types CAN share it — pointer-to-unnamed-composite types drop their package path, so *[]alpha.Bus and *[]beta.Bus of two same-short-named packages read identically. The string keys the creation guard and the teardown; two types behind one key mean false cycles at resolution and merged nodes at close. Every type registration therefore records its key here and a second, DIFFERENT type arriving under the same key is refused at the boot line that declares it. */
    typeIdentityKeyToType map[string]reflect.Type
    dependencyGraph       map[string]map[string]struct{}
    /* builtServiceNames marks the name-keyed instances the container itself created, as opposed to installed overrides: an override replacing a built instance orphans a value only the container ever held, and the teardown closes what this set points at even after the maps stopped naming it (via replacedBuiltInstances). An installed override evicted by a later override is NOT the container's to close — it belongs to whoever installed it. */
    builtServiceNames       map[string]struct{}
    replacedBuiltInstances  []any
    /* the scoped registrations: providers of services the SCOPES of this container own, kept apart from the container's own so nothing resolved against the container can reach them. They are the source the immutable plan below is built from, never read on the request path. */
    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedReplacesContainerService    map[string]bool
    /* the declared return type of each provider, kept beside it for introspection: the wrapped provider erases the signature, and a description without a type would send the operator to build a service the listing exists not to build */
    providerServiceTypeByName       map[string]reflect.Type
    scopedProviderServiceTypeByName map[string]reflect.Type
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

    /* the lookup is canonical because the registrations are: a service registered from a provider returning *T is filed under *T, and GetByType canonicalises before it looks. Asking with the value type therefore used to be answered "no" for a service GetByType resolves happily, so Has and Get disagreed about the same container. */
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

    /* an override landing after Close has enumerated the instances is stored into a map no teardown will ever read again — it is served by later lookups and closed by nobody. Refused, like the scoped registrar refuses; the read paths keep serving what was built, so a shutdown-racing request degrades gracefully instead of half-working. */
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

    /* the override propagates to every type this name is registered under, and a type-keyed resolution hands out whatever sits there with no re-check — the call-time assignability guard of the provider contract does not see overrides. A value the registered type cannot hold is refused before anything is written, so GetByType keeps its contract and the name/type maps never learn two different answers. */
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

    /* the instance being replaced is closed by the teardown if the container built it: it was evicted from the only maps the close sweep reads, and nothing else holds it. An evicted override stays its installer's. The graveyard — not an inline Close — because a provider may hand the same pointer to several names, and the teardown's identity marks are what guarantee one Close per instance, after everything that still uses it. */
    if replacedValue, replacedExists := instance.instances[serviceName]; true == replacedExists {
        if _, wasBuilt := instance.builtServiceNames[serviceName]; true == wasBuilt {
            instance.replacedBuiltInstances = append(instance.replacedBuiltInstances, replacedValue)
        }
    }
    delete(instance.builtServiceNames, serviceName)

    instance.instances[serviceName] = value

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

    /* the scoped registrar already refuses a closed container; the plain one accepted silently, reporting success for a service whose every resolution the creation guard then refuses. Same condition, same answer. */
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

/* recordTypeIdentityKeyLocked refuses the registration of a type whose identity key another, DIFFERENT type already claimed. The colliding pair would share a creation-guard key and a close node while holding two instances — a resolution of one reads as a cycle through the other, and the teardown merges what it should order. The refusal lands at the boot line that declares the second type, where the wiring mistake is. */
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

var _ containercontract.Container = (*container)(nil)
