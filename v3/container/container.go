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
    /* the identity key is a string, and distinct types CAN share it — pointer-to-unnamed-composite types drop their package path, so *[]alpha.Bus and *[]beta.Bus of two same-short-named packages read identically. The string keys the creation guard and the teardown; two types behind one key mean false cycles at resolution and merged nodes at close. Every type registration therefore records its key here and a second, DIFFERENT type arriving under the same key is refused at the boot line that declares it. */
    typeIdentityKeyToType map[string]reflect.Type
    dependencyGraph       map[string]map[string]struct{}
    /* builtServiceNames marks the name-keyed instances the container itself created, as opposed to installed overrides: an override replacing a built instance orphans a value only the container ever held, and the teardown closes what this set points at even after the maps stopped naming it (via replacedBuiltInstances). An installed override evicted by a later override is NOT the container's to close — it belongs to whoever installed it. */
    builtServiceNames      map[string]struct{}
    replacedBuiltInstances []any
    /* the order in which the teardown nodes came into being, which is what breaks a tie the dependency graph leaves open. It is written wherever a value enters the instance maps — a creation or an installed override — and read only by the teardown; see closesBefore for why creation order and not the node key. */
    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
    /* the scoped registrations: providers of services the SCOPES of this container own, kept apart from the container's own so nothing resolved against the container can reach them. They are the source the immutable plan below is built from, never read on the request path. */
    scopedProviders                   map[string]providerAny
    scopedTypeProviders               map[reflect.Type]providerAny
    scopedTypeRegistrationNamesByType map[reflect.Type][]string
    scopedCollectionPriorityByName    map[string]int
    scopedReplacesContainerService    map[string]bool
    /* the declared return type of each provider, kept beside it for introspection: the wrapped provider erases the signature, and a description without a type would send the operator to build a service the listing exists not to build */
    providerServiceTypeByName       map[string]reflect.Type
    scopedProviderServiceTypeByName map[string]reflect.Type
    /* the published plan every new scope is bound to by reference. A registration clears it and the next scope rebuilds it, so creating a scope costs one atomic load once boot has settled. */
    scopePlanPointer atomic.Pointer[scopePlan]
    isClosed         bool
    /* declaredTeardownEdges is the list of edges an application wrote by hand rather than resolving, kept beside the graph they were written into. The graph itself cannot say where an edge came from — that is its virtue, since the teardown must read one graph — but the difference matters twice: an edge naming a service that was never registered is a wiring typo the resolution form cannot produce, and the operator reading the teardown wants to know which orderings were asserted and which were observed. */
    declaredTeardownEdges []declaredTeardownEdge
    /* heldIdentitiesByNodeKey is what each built service was seen to hold, recorded at construction and matched at teardown. It is written only while the waves are armed; see teardown_reflection.go for why the walk is there and not at teardown. heldIdentitiesByValue is the same record keyed by the value walked, so a value filed under several nodes is walked once and the nodes share the answer. */
    heldIdentitiesByNodeKey map[string][]heldPointer
    heldIdentitiesByValue   map[pointerIdentity][]heldPointer
    /* teardownInWaves is the application's assertion that this container's teardown graph is complete: that every ordering its services need is either written by a resolution or declared. It is off by default, because the teardown the framework has shipped so far is strictly sequential and a service that depends on another without saying so has been carried by the creation order all along. */
    teardownInWaves bool
    /* teardownFinished is the SECOND of the two closing states, and the pair is what lets a service's own Close still resolve what it depends on. isClosed is set before the first service Close runs and refuses every new CREATION for the whole teardown; teardownFinished is set after the last one returns and refuses every RESOLUTION from then on. Between them the memoized instances are still served, because a worker reporting its drain at Close is entitled to the logger it reports through, while after them a resolution used to answer a closed instance with a nil error, so a caller that kept a resolver got a handle to a dead service and no way to know it. */
    teardownFinished bool
    closeErr         error
    closeOnce        sync.Once
    /* teardownDeadline is the record of the one teardown this container ran, kept only when it ran PAST the deadline it was given: nil for a teardown with no deadline, and for one that finished inside it. It is what TeardownDeadlineOverrun answers, because a spent budget is not a failure and the error the teardown returns cannot carry it alone. */
    teardownDeadline exceptioncontract.Context
}

/* declaredTeardownEdge is one hand-written ordering: the service that declared it, the node it named, and the spelling it used, which is what a refusal has to quote back. */
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

/* OverrideInstance installs a value under a registered name, and the container CLOSES what was installed into it — unlike a scope, whose overrides belong to whoever installed them unless the installer says otherwise with ClosedWithScope. The two defaults are opposite because the two lifetimes are: a scope ends while its installer goes on running, and the http kernel that installs the request logger keeps using it to report the scope's own close failure, so a scope closing its overrides would close them under their owner. A container ends when the process does, and there is nobody left to hand the value to.

   The one case where that reasoning does not hold is a value shared with a SECOND container in the same process — a test suite that boots the application repeatedly and reuses one client across every boot, or a host embedding melody that closes its own handles. There this container's teardown closes it for all of them, and there is no opt-out. Install a wrapper whose Close does nothing, and keep the real handle where it belongs. */
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

        /* the walk's memo of the evicted value goes with it: keyed on the value's address, the memo kept every collaborator the evicted value held alive for as long as the container stood — measured, five hundred overrides installed one after the other under the armed teardown kept all five hundred evicted values from collection until Close, where the unarmed container let all but the last go. A node's own record is replaced below; the memo is what nothing replaced. */
        if evictedKey, hasEvictedKey := pointerKeyOf(replacedValue); true == hasEvictedKey && nil != instance.heldIdentitiesByValue {
            delete(instance.heldIdentitiesByValue, evictedKey)
        }
    }
    delete(instance.builtServiceNames, serviceName)

    instance.instances[serviceName] = value
    instance.recordCreationOrderLocked(containerNameNodeKey(serviceName))

    /* what the installed value HOLDS is recorded where a built value's is, under the same node, replacing the record of the value it evicted: without it an armed teardown read the override as holding nothing — its holder shared a wave with what it held — and read what the evicted value used to hold as if the new value held it. The value comes from the caller and is already in use, so it is walked as published memory. */
    instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)

    /* the override propagates only to the types this NAME is registered under, by the loop below: the previous block also wrote it under the override value's own canonical type whenever that type was registered by ANY service, so overriding one name answered a different service's GetByType with this value. A canonical type this name owns is already reached by the loop; a type another service owns must not learn this override; and a free type is deliberately left out here (unlike the scope, which exposes it) because a container value-type service filed under a second, uncollapsed node closes twice at teardown. */
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

/* registeredTypesForServiceName answers every type the name is registered under, for the scope override that propagates to them; the scope calls it before taking its own lock, container-then-scope being the only order the two locks are ever taken in. */
/* serviceNamesForRegisteredType lists the service names a type is registered under, so a caller deciding whether a type is free can see who, if anyone, already claims it. */
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

    /* the declared teardown edges are validated BEFORE anything is written, so a refused registration leaves the maps exactly as it found them */
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

        /* a registration declaring a teardown edge to its OWN registered type is declaring one on itself: the two nodes are collapsed onto one representative before the walk, so the edge would be a self-edge the walk drops — silently, which is the shape this refuses everywhere else */
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

    /* once the waves are armed, the rule arming asked of every declared edge is asked of each new one here, at the door that declares it: arming validated a snapshot, and a declaration registered after it used to land, silently, in one wave with the service it named */
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

    /* the declared edges are written last, once the registration cannot fail anymore: the graph is never pruned, so an edge left behind by a refused registration would outlive it for the life of the process. They go into the very graph a resolution writes into, in the same key space, so the teardown reads one graph and cannot order two ways. */
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

        /* a second name under a type a declaration already names would make that declaration ambiguous — the refusal arming gives it — after arming answered; under the waves the ambiguity is refused where it is created, at the registration that would create it */
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

var (
    _ containercontract.Container       = (*container)(nil)
    _ containercontract.ScopedRegistrar = (*container)(nil)
    _ parallelTeardownArmer             = (*container)(nil)
    _ teardownPlanner                   = (*container)(nil)
    _ contextCloser                     = (*container)(nil)
    _ closedContainerChecker            = (*container)(nil)
)
