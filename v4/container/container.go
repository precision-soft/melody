package container

import (
    "context"
    "fmt"
    "reflect"
    "slices"
    "sort"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* DefaultScopeTeardownTimeout is how long a scope's Close gives its scoped services when ContainerConfig.ScopeTeardownTimeout is left out. It stays under config.DefaultHttpShutdownTimeout, since a request scope still closing when a shutdown's wait ends is reported as left open. */
const DefaultScopeTeardownTimeout = 3 * time.Second

/* ScopeTeardownTimeoutRule resolves ContainerConfig.ScopeTeardownTimeout: 3s when left out, at least 1ms, unlimited admitted. */
var ScopeTeardownTimeoutRule = bound.Rule[time.Duration]{
    Name:              "ContainerConfig.ScopeTeardownTimeout",
    Default:           DefaultScopeTeardownTimeout,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

/* DefaultCreationWaiters is how many resolutions may wait on one in-flight creation when ContainerConfig.CreationWaiters is left out: every concurrent request of one server waiting on the same singleton at a cold start. */
const DefaultCreationWaiters = 1024

/* CreationWaitersRule resolves ContainerConfig.CreationWaiters: 1,024 when left out, at least 1, unlimited admitted. */
var CreationWaitersRule = bound.Rule[int]{
    Name:              "ContainerConfig.CreationWaiters",
    Default:           DefaultCreationWaiters,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* ContainerConfig is what a container declares beyond its registrations; the zero value is the default. */
type ContainerConfig struct {
    /* ScopeTeardownTimeout bounds what a scope's Close hands its scoped services: 3s when left out, at least 1ms, unlimited admitted for no term. */
    ScopeTeardownTimeout bound.Duration
    /* CreationWaiters bounds the resolutions that may wait on one in-flight creation: 1,024 when left out, at least 1, unlimited admitted. One more is refused with ErrCreationWaitersExceeded. */
    CreationWaiters bound.Count
    /* TeardownSequential closes the services one after the other in the plan's order, where the default closes in one wave every service the teardown graph proves unrelated to the others in it: the opt-out for services whose Close methods touch shared external state the container cannot see, such as one file or one remote session. */
    TeardownSequential bool
}

/* NewContainer builds an empty container; a ContainerConfig field outside its rule panics naming the field. */
func NewContainer(config ContainerConfig) containercontract.Container {
    creationWaiters, creationWaitersErr := bound.Resolve(config.CreationWaiters, CreationWaitersRule)
    if nil != creationWaitersErr {
        exception.Panic(exception.FromError(creationWaitersErr))
    }

    scopeTeardownTimeout, scopeTeardownTimeoutErr := bound.Resolve(config.ScopeTeardownTimeout, ScopeTeardownTimeoutRule)
    if nil != scopeTeardownTimeoutErr {
        exception.Panic(exception.FromError(scopeTeardownTimeoutErr))
    }

    return &container{
        creationWaiters:             creationWaiters,
        scopeTeardownTimeout:        scopeTeardownTimeout,
        teardownInWaves:             false == config.TeardownSequential,
        closedChannel:               make(chan struct{}),
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
        provisionalDependencyEdges:  make(map[string]map[string]int),
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
    /* provisionalDependencyEdges counts the resolutions in flight holding an edge of dependencyGraph no resolution acquired yet; a failed resolution takes back the edge it alone held */
    provisionalDependencyEdges map[string]map[string]int
    /* the name-keyed instances the container built, as opposed to installed overrides; an override replacing one leaves it to the teardown through replacedBuiltInstances */
    builtServiceNames      map[string]struct{}
    replacedBuiltInstances []any
    /* replacedBuiltClosers holds, at the index of each replaced instance, the closer its registration declared, nil where none */
    replacedBuiltClosers []*declaredCloser
    /* closerByNodeKey holds the closer a registration declared in RegisterOptions.Closer, under its name node and its type node */
    closerByNodeKey map[string]*declaredCloser
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
    /* closedChannel is closed under the lock that raises isClosed, so a resolution parked on another goroutine's creation leaves when the teardown begins */
    closedChannel chan struct{}
    creationWaiters bound.Resolved[int]

    scopeTeardownTimeout bound.Resolved[time.Duration]
    /* afterCreationWaitReleased is a test seam: it runs once a waiter left its wait on a release, before it re-takes the lock */
    afterCreationWaitReleased func()
    /* the hand-declared edges, kept beside the graph they were written into so a refusal can quote them */
    declaredTeardownEdges []declaredTeardownEdge
    /* what each built service was seen to hold, recorded at construction under the waves; heldIdentitiesByValue keys the same record by the value walked */
    heldIdentitiesByNodeKey map[string][]heldPointer
    heldIdentitiesByValue   map[pointerIdentity][]heldPointer
    /* reflectionSkippedNodeKeys are the nodes registered with TeardownReflectionDisabled; opaqueIdentities are their filed values, which a walk records and never enters, kept alive so an address is not reused under them */
    reflectionSkippedNodeKeys map[string]struct{}
    opaqueIdentities          map[pointerIdentity]any
    /* opaqueTypes are the concrete pointer types registered with TeardownReflectionDisabled: a walk records a value of such a type and never enters it, filed or not, so a holder walked before the service it captured cannot read it */
    opaqueTypes map[reflect.Type]struct{}
    /* teardownInWaves closes the unrelated services of one wave at once; on unless ContainerConfig.TeardownSequential */
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
    /* dependencyType is the canonical type a type edge declares, nil on a name edge: the key finds the registration, and the type confirms it is this one */
    dependencyType reflect.Type
    /* optional marks an edge declared through OptionalTeardownDependencyNames or OptionalTeardownDependencyTypes: kept when its dependency is registered, dropped without a refusal when it is not */
    optional bool
}

/* declaredTeardownEdgesOf lists the edges a registration declares, the strict ones first, in the order of their fields. */
func declaredTeardownEdgesOf(serviceName string, options RegisterOptions) []declaredTeardownEdge {
    declaredEdges := make([]declaredTeardownEdge, 0, len(options.TeardownDependencyNames)+len(options.TeardownDependencyTypes)+len(options.OptionalTeardownDependencyNames)+len(options.OptionalTeardownDependencyTypes))

    for _, dependencyName := range options.TeardownDependencyNames {
        declaredEdges = append(declaredEdges, declaredNameEdge(serviceName, dependencyName))
    }

    for _, dependencyType := range options.TeardownDependencyTypes {
        declaredEdges = append(declaredEdges, declaredTypeEdge(serviceName, dependencyType))
    }

    for _, dependencyName := range options.OptionalTeardownDependencyNames {
        optionalEdge := declaredNameEdge(serviceName, dependencyName)
        optionalEdge.optional = true
        declaredEdges = append(declaredEdges, optionalEdge)
    }

    for _, dependencyType := range options.OptionalTeardownDependencyTypes {
        optionalEdge := declaredTypeEdge(serviceName, dependencyType)
        optionalEdge.optional = true
        declaredEdges = append(declaredEdges, optionalEdge)
    }

    return declaredEdges
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
        dependencyType:       canonicalServiceType(dependencyType),
    }
}

func (instance *container) Get(ctx context.Context, serviceName string) (any, error) {
    resolver := newResolverContext(instance)

    return resolver.Get(ctx, serviceName)
}

/* MustGet panics with the resolution failure whole, as MustGetByType does, and writes the service name into its context only where no nested failure named the service that failed; MustGetByType writes the type the same way. */
func (instance *container) MustGet(ctx context.Context, serviceName string) any {
    resolver := newResolverContext(instance)

    return resolver.MustGet(ctx, serviceName)
}

func (instance *container) GetByType(ctx context.Context, targetType reflect.Type) (any, error) {
    resolver := newResolverContext(instance)
    return resolver.GetByType(ctx, targetType)
}

func (instance *container) MustGetByType(ctx context.Context, targetType reflect.Type) any {
    resolver := newResolverContext(instance)
    return resolver.MustGetByType(ctx, targetType)
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

/* OverrideInstance installs a value under a registered name, and the container closes at teardown the value still installed then, an override a later override evicted staying its installer's, unlike a scope, whose overrides stay their installer's. A value shared with another container in the same process is closed for all of them; install a wrapper whose CloseWithContext does nothing in that case. OverrideOptions.ClosedWithScope is refused, the container having no scope to hand the value to. */
func (instance *container) OverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
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

    return instance.OverrideProtectedInstance(serviceName, value, options)
}

func (instance *container) MustOverrideInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    overrideInstanceErr := instance.OverrideInstance(serviceName, value, options)
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

func (instance *container) OverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) error {
    if "" == serviceName {
        return exception.NewError(
            "service name is empty in override instance",
            nil,
            nil,
        )
    }

    if true == options.ClosedWithScope {
        return exception.NewError(
            "the container has no scope to close an override with",
            map[string]any{
                "serviceName": serviceName,
                "field":       "OverrideOptions.ClosedWithScope",
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

    /* the container closes what it installs, so a value carrying Close alone is refused unless the registration's closer takes it */
    if true == closeOnlyValueRefused(value, instance.closerByNodeKey[containerNameNodeKey(serviceName)]) {
        return newCloseDoorRequiredError("the override's value carries Close without CloseWithContext", serviceName, valueType)
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
            instance.replacedBuiltClosers = append(instance.replacedBuiltClosers, instance.closerByNodeKey[containerNameNodeKey(serviceName)])
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

func (instance *container) MustOverrideProtectedInstance(serviceName string, value any, options containercontract.OverrideOptions) {
    overrideInstanceErr := instance.OverrideProtectedInstance(serviceName, value, options)
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

    addDependencyEdge(instance.dependencyGraph, dependentKey, dependencyKey)
    acquireProvisionalEdge(instance.provisionalDependencyEdges, dependentKey, dependencyKey)
}

/* ringNodeKeyLocked folds a type node onto the one name registered under its type, so a ring closed partly by name and partly by type is seen as one ring; a type with no name or several keeps its own node. A prospective fold answers for a type a registration in progress is about to change. */
func (instance *container) ringNodeKeyLocked(nodeKey string, prospectiveFolds map[string]string) string {
    if false == strings.HasPrefix(nodeKey, containerTypeNodeKeyPrefix) {
        return nodeKey
    }

    if foldedKey, prospective := prospectiveFolds[nodeKey]; true == prospective {
        return foldedKey
    }

    registeredType, known := instance.typeIdentityKeyToType[strings.TrimPrefix(nodeKey, containerTypeNodeKeyPrefix)]
    if false == known {
        return nodeKey
    }

    serviceNames := instance.typeRegistrationNamesByType[registeredType]
    if 1 != len(serviceNames) {
        return nodeKey
    }

    return containerNameNodeKey(serviceNames[0])
}

/* foldedDependencyGraphLocked answers the edges the resolutions and the declarations recorded, every node folded: the name form of a declaration is already in the graph, and the type form counts once it folds onto its one name, as the plan expands it. */
func (instance *container) foldedDependencyGraphLocked(prospectiveFolds map[string]string, prospectiveType reflect.Type) map[string]map[string]struct{} {
    foldedEdges := make(map[string]map[string]struct{}, len(instance.dependencyGraph))
    for graphDependent, dependencySet := range instance.dependencyGraph {
        for graphDependency := range dependencySet {
            addDependencyEdge(foldedEdges, instance.ringNodeKeyLocked(graphDependent, prospectiveFolds), instance.ringNodeKeyLocked(graphDependency, prospectiveFolds))
        }
    }

    for _, declaredEdge := range instance.declaredTeardownEdges {
        if false == strings.HasPrefix(declaredEdge.dependencyNodeKey, containerTypeNodeKeyPrefix) {
            continue
        }

        if foldedDependency, orders := instance.declaredTypeRingKeyLocked(declaredEdge.dependencyNodeKey, declaredEdge.dependencyType, prospectiveFolds, prospectiveType); true == orders {
            addDependencyEdge(foldedEdges, containerNameNodeKey(declaredEdge.dependentServiceName), foldedDependency)
        }
    }

    return foldedEdges
}

/* declaredTypeRingKeyLocked folds a type declaration onto its one name only where the type filed under its key is the very type declared, the belt the plan's expansion applies, and answers false for a declaration that orders nothing. */
func (instance *container) declaredTypeRingKeyLocked(dependencyNodeKey string, dependencyType reflect.Type, prospectiveFolds map[string]string, prospectiveType reflect.Type) (string, bool) {
    if _, prospective := prospectiveFolds[dependencyNodeKey]; true == prospective {
        if dependencyType != prospectiveType {
            return "", false
        }
    } else if registeredType, known := instance.typeIdentityKeyToType[strings.TrimPrefix(dependencyNodeKey, containerTypeNodeKeyPrefix)]; false == known || registeredType != dependencyType {
        return "", false
    }

    foldedKey := instance.ringNodeKeyLocked(dependencyNodeKey, prospectiveFolds)

    return foldedKey, foldedKey != dependencyNodeKey
}

/* refuseDependencyRingLocked refuses the edge from dependentKey to dependencyKey when the dependency already reaches the dependent through the recorded edges, so no ring is ever written into the graph. The container mutex is held. */
func (instance *container) refuseDependencyRingLocked(dependentKey string, dependencyKey string) error {
    if "" == dependentKey || "" == dependencyKey || true == hasDependencyEdge(instance.dependencyGraph, dependentKey, dependencyKey) {
        return nil
    }

    ringDependent := instance.ringNodeKeyLocked(dependentKey, nil)
    ringDependency := instance.ringNodeKeyLocked(dependencyKey, nil)
    if ringDependent == ringDependency {
        return nil
    }

    if false == dependencyReaches(instance.foldedDependencyGraphLocked(nil, nil), ringDependency, ringDependent) {
        return nil
    }

    return newCircularDependencyError(dependentKey, dependencyKey)
}

/* refuseRegistrationRingLocked refuses a registration whose declarations, or whose type turning on a declaration already recorded, would close a ring. Every edge the registration adds touches the new service, so a new ring passes through it. The container mutex is held. */
func (instance *container) refuseRegistrationRingLocked(serviceName string, serviceType reflect.Type, options RegisterOptions) error {
    serviceNodeKey := containerNameNodeKey(serviceName)

    prospectiveFolds := map[string]string(nil)
    prospectiveType := reflect.Type(nil)
    if false == options.TypeRegistrationDisabled && nil != serviceType {
        prospectiveType = canonicalServiceType(serviceType)
        typeNodeKey := containerTypeNodeKey(serviceType)
        prospectiveFolds = map[string]string{typeNodeKey: typeNodeKey}

        if 0 == len(instance.typeRegistrationNamesByType[prospectiveType]) {
            prospectiveFolds[typeNodeKey] = serviceNodeKey
        }
    }

    foldedEdges := instance.foldedDependencyGraphLocked(prospectiveFolds, prospectiveType)

    for _, dependencyName := range slices.Concat(options.TeardownDependencyNames, options.OptionalTeardownDependencyNames) {
        addDependencyEdge(foldedEdges, serviceNodeKey, containerNameNodeKey(dependencyName))
    }

    for _, dependencyType := range slices.Concat(options.TeardownDependencyTypes, options.OptionalTeardownDependencyTypes) {
        if foldedDependency, orders := instance.declaredTypeRingKeyLocked(containerTypeNodeKey(dependencyType), canonicalServiceType(dependencyType), prospectiveFolds, prospectiveType); true == orders {
            addDependencyEdge(foldedEdges, serviceNodeKey, foldedDependency)
        }
    }

    for dependencyKey := range foldedEdges[serviceNodeKey] {
        if dependencyKey != serviceNodeKey && true == dependencyReaches(foldedEdges, dependencyKey, serviceNodeKey) {
            return newCircularDependencyError(serviceNodeKey, dependencyKey)
        }
    }

    return nil
}

func newCircularDependencyError(dependentKey string, dependencyKey string) error {
    return exception.NewError(
        "circular service dependency detected",
        exceptioncontract.Context{
            "dependent":  dependentKey,
            "dependency": dependencyKey,
        },
        ErrCircularDependency,
    )
}

/* holdProvisionalEdge writes an edge a resolution in flight holds and answers its release, called under the same lock: an acquiring release keeps the edge, a failing one takes it back unless another resolution still holds it or a resolution or a declaration acquired it meanwhile. An edge already acquired is not held. */
func holdProvisionalEdge(graph map[string]map[string]struct{}, provisional map[string]map[string]int, dependentKey string, dependencyKey string) func(acquired bool) {
    if "" == dependentKey || "" == dependencyKey {
        return func(bool) {}
    }

    holders, held := provisional[dependentKey][dependencyKey]
    if true == hasDependencyEdge(graph, dependentKey, dependencyKey) && false == held {
        return func(bool) {}
    }

    addDependencyEdge(graph, dependentKey, dependencyKey)

    if nil == provisional[dependentKey] {
        provisional[dependentKey] = make(map[string]int)
    }

    provisional[dependentKey][dependencyKey] = holders + 1

    return func(acquired bool) {
        currentHolders, stillHeld := provisional[dependentKey][dependencyKey]
        if false == stillHeld {
            return
        }

        if false == acquired && 1 < currentHolders {
            provisional[dependentKey][dependencyKey] = currentHolders - 1

            return
        }

        acquireProvisionalEdge(provisional, dependentKey, dependencyKey)

        if false == acquired {
            delete(graph[dependentKey], dependencyKey)
            if 0 == len(graph[dependentKey]) {
                delete(graph, dependentKey)
            }
        }
    }
}

/* acquireProvisionalEdge marks an edge acquired, so no failing resolution takes it back. */
func acquireProvisionalEdge(provisional map[string]map[string]int, dependentKey string, dependencyKey string) {
    delete(provisional[dependentKey], dependencyKey)
    if 0 == len(provisional[dependentKey]) {
        delete(provisional, dependentKey)
    }
}

func addDependencyEdge(graph map[string]map[string]struct{}, dependentKey string, dependencyKey string) {
    dependencies, exists := graph[dependentKey]
    if false == exists {
        dependencies = make(map[string]struct{})
        graph[dependentKey] = dependencies
    }

    dependencies[dependencyKey] = struct{}{}
}

func hasDependencyEdge(graph map[string]map[string]struct{}, dependentKey string, dependencyKey string) bool {
    dependencies, exists := graph[dependentKey]
    if false == exists {
        return false
    }

    _, related := dependencies[dependencyKey]

    return related
}

func (instance *container) register(
    serviceName string,
    serviceType reflect.Type,
    provider providerAny,
    options containercontract.RegisterOptions,
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

    if true == closerIsIncomplete(options.Closer) {
        return exception.NewError(
            "a closer names both the type it takes and its close",
            map[string]any{
                "serviceName": serviceName,
                "field":       "RegisterOptions.Closer",
            },
            ErrCloserTypeMismatch,
        )
    }

    /* a closer that does not take the provider's declared type is refused before anything is written */
    if nil != options.Closer.Close && nil != serviceType && false == isAnyType(serviceType) && false == serviceType.AssignableTo(options.Closer.ValueType) {
        return exception.NewError(
            "the closer does not take the service's type",
            map[string]any{
                "serviceName":     serviceName,
                "serviceType":     serviceType.String(),
                "closerValueType": options.Closer.ValueType.String(),
            },
            ErrCloserTypeMismatch,
        )
    }

    /* a concrete type carrying Close alone could not take the teardown's budget, so it is refused here, where its type is known, unless a closer takes it */
    if false == closerIsDeclared(options.Closer) && true == typeCarriesOnlyPlainClose(serviceType) {
        return newCloseDoorRequiredError("the service's type carries Close without CloseWithContext", serviceName, serviceType)
    }

    /* the declared teardown edges, strict and optional, are validated before anything is written */
    for _, dependencyName := range slices.Concat(options.TeardownDependencyNames, options.OptionalTeardownDependencyNames) {
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

    for _, dependencyType := range slices.Concat(options.TeardownDependencyTypes, options.OptionalTeardownDependencyTypes) {
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
        if false == options.TypeRegistrationDisabled && nil != serviceType && containerTypeNodeKey(serviceType) == containerTypeNodeKey(dependencyType) {
            return exception.NewError(
                "a service cannot declare a teardown dependency on its own registered type",
                map[string]any{
                    "serviceName": serviceName,
                },
                ErrTeardownDependencyIsSelf,
            )
        }
    }

    declaredEdges := declaredTeardownEdgesOf(serviceName, options)

    /* under the waves, what a declaration names is refused at the door when the refusal is already certain; a name not registered yet is left to VerifyDeclaredTeardownDependencies */
    if true == instance.teardownInWaves {
        for _, declaredEdge := range declaredEdges {
            if refusalErr := instance.refuseDeclaredTeardownEdgeAtDoorLocked(declaredEdge); nil != refusalErr {
                return refusalErr
            }
        }
    }

    if ringErr := instance.refuseRegistrationRingLocked(serviceName, serviceType, options); nil != ringErr {
        return ringErr
    }

    instance.providers[serviceName] = provider
    if nil != serviceType {
        instance.providerServiceTypeByName[serviceName] = serviceType
    }

    if 0 != options.CollectionPriority {
        instance.collectionPriorityByName[serviceName] = options.CollectionPriority
    }

    if false == options.TypeRegistrationDisabled {
        registerTypeErr := instance.registerType(
            serviceName,
            serviceType,
            provider,
            false == options.TypeRegistrationRelaxed,
        )
        if nil != registerTypeErr {
            delete(instance.providers, serviceName)
            delete(instance.providerServiceTypeByName, serviceName)
            delete(instance.collectionPriorityByName, serviceName)
            return registerTypeErr
        }
    }

    if nil != options.Closer.Close {
        if nil == instance.closerByNodeKey {
            instance.closerByNodeKey = make(map[string]*declaredCloser)
        }

        closer := &declaredCloser{valueType: options.Closer.ValueType, close: options.Closer.Close}
        instance.closerByNodeKey[containerNameNodeKey(serviceName)] = closer
        if false == options.TypeRegistrationDisabled && nil != serviceType {
            instance.closerByNodeKey[containerTypeNodeKey(serviceType)] = closer
        }
    }

    if true == options.TeardownReflectionDisabled {
        instance.skipTeardownReflectionLocked(containerNameNodeKey(serviceName))
        instance.skipTeardownReflectionOfTypeLocked(serviceType)
        if false == options.TypeRegistrationDisabled && nil != serviceType {
            instance.skipTeardownReflectionLocked(containerTypeNodeKey(serviceType))
        }
    }

    /* the declared edges are written last, once the registration cannot fail, since a declared edge is never taken back. A name edge goes into the resolution graph; a type edge is kept beside it and expanded per plan */
    for _, declaredEdge := range declaredEdges {
        instance.recordDeclaredTeardownEdgeLocked(declaredEdge)
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
                    "a second service cannot be registered under a type a teardown dependency already names, because the declaration would then name a set and order nothing in the teardown's waves",
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
    _ declaredTeardownVerifier          = (*container)(nil)
    _ teardownDependencyDeclarer        = (*container)(nil)
    _ teardownPlanner                   = (*container)(nil)
    _ containercontract.ContextCloser   = (*container)(nil)
    _ closedContainerChecker            = (*container)(nil)
)
