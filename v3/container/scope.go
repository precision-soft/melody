package container

import (
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

func newScope(containerInstance *container, plan *scopePlan) containercontract.Scope {
    if nil == plan {
        plan = newEmptyScopePlan()
    }

    scopeInstance := &scope{
        plan:                 plan,
        instances:            make(map[string]any),
        typeInstances:        make(map[reflect.Type]any),
        createdInstances:     make(map[string]any),
        createdTypeInstances: make(map[reflect.Type]any),
        createdAliasNodeKeys: make(map[string]string),

        ownProviders:                   make(map[string]providerAny),
        ownTypeProviders:               make(map[reflect.Type]providerAny),
        ownTypeRegistrationNamesByType: make(map[reflect.Type][]string),
        ownReplacesContainerService:    make(map[string]bool),

        creatingByName:  make(map[string]*creationState),
        creatingByType:  make(map[string]*creationState),
        dependencyGraph: make(map[string]map[string]struct{}),

        creationOrderByNodeKey: make(map[string]int),
    }
    scopeInstance.container.Store(containerInstance)

    return scopeInstance
}

type scope struct {
    mutex     sync.RWMutex
    container atomic.Pointer[container]
    /* the immutable plan this scope was created against, shared by reference with every other scope of the same generation and never written to. Holding a reference rather than a copy is what keeps creating a scope O(1) whatever the size of the plan. */
    plan          *scopePlan
    instances     map[string]any
    typeInstances map[reflect.Type]any
    /* the services built through this scope are kept apart from the overrides installed into it: an override belongs to whoever installed it and outlives the scope, while a service created from one holds that request's substitutes and must not survive it. Keeping them in their own maps is what lets the scope be emptied of the second kind without touching the first. */
    createdInstances     map[string]any
    createdTypeInstances map[reflect.Type]any
    /* one created instance filed under its name AND its type is two teardown nodes for one value, and a dependency edge recorded against either must constrain both. The link from the type node to the name node is written at the moment of the dual filing — the only place that knows the two are one — and the teardown collapses along it before it orders anything. Without the collapse the type alias carries no edges and — stamped after its name filing, so popped first under the latest-first tie-break — closes the shared instance ahead of its still-open dependents. */
    createdAliasNodeKeys map[string]string
    /* a created instance evicted by a ClosedWithScope override left the maps the teardown reads, but it is still the scope's to close; it waits here and the teardown closes it after the ordered walk, under the same identity marks that keep any still-filed alias of it from being closed twice. */
    evictedCreatedInstances []any
    /* registrations made on this scope itself, layered over the plan. They are the rare case: the declared plan is where a scoped service normally comes from. */
    ownProviders                   map[string]providerAny
    ownTypeProviders               map[reflect.Type]providerAny
    ownTypeRegistrationNamesByType map[reflect.Type][]string
    ownReplacesContainerService    map[string]bool
    /* creatingByName, creatingByType and dependencyGraph are guarded by the CONTAINER mutex, not by this scope's. The creation guard that reads and writes them runs with the container lock held and releases it only around the provider call, so that is the only lock they are ever touched under. Close() must therefore never nil them: it holds the scope lock alone, and emptying a map a guard is writing to would be a data race with no lock in common. They die with the scope instead. */
    creatingByName  map[string]*creationState
    creatingByType  map[string]*creationState
    dependencyGraph map[string]map[string]struct{}
    /* the order in which this scope's teardown nodes came into being, the tie-break the container's teardown uses for the same reason: the two share one walk, and two rules for it would be two chances to order a teardown differently. Written under the scope mutex on the same lines the created maps are written. */
    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
}

/* recordCreationOrderLocked stamps a teardown node the first time it comes into being; the scope mutex is held by every caller */
func (instance *scope) recordCreationOrderLocked(nodeKey string) {
    if _, stamped := instance.creationOrderByNodeKey[nodeKey]; true == stamped {
        return
    }

    instance.creationOrderCounter = instance.creationOrderCounter + 1
    instance.creationOrderByNodeKey[nodeKey] = instance.creationOrderCounter
}

func (instance *scope) Get(serviceName string) (any, error) {
    if "" == serviceName {
        return nil, exception.NewError(
            "service name is empty in get",
            nil,
            nil,
        )
    }

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        /* return the closed-scope error instead of panicking: error-returning methods follow the Must/non-Must convention (Must* wrappers keep panicking), and a panic here is fatal in handler-spawned goroutines that outlive the request — the kernel closes the scope when ServeHttp returns and no recover covers those goroutines */
        return nil, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    resolver := newScopeResolverContext(containerInstance, instance)

    return resolver.Get(serviceName)
}

func (instance *scope) MustGet(serviceName string) any {
    value, getErr := instance.Get(serviceName)
    if nil != getErr {
        exception.Panic(
            exception.NewError(
                "failed to get service from scope",
                map[string]any{
                    "serviceName": serviceName,
                },
                getErr,
            ),
        )
    }

    return value
}

func (instance *scope) GetByType(targetType reflect.Type) (any, error) {
    if nil == targetType {
        return nil, exception.NewError(
            "service type is required in get by type",
            nil,
            nil,
        )
    }

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        /* mirror Get: closed scope yields an error, not a panic; MustGetByType keeps the panic */
        return nil, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    resolver := newScopeResolverContext(containerInstance, instance)

    return resolver.GetByType(targetType)
}

func (instance *scope) MustGetByType(targetType reflect.Type) any {
    value, getByTypeErr := instance.GetByType(targetType)
    if nil != getByTypeErr {
        /* a nil targetType yields a clean GetByType error, so guard the type string here too rather than dereferencing a nil reflect.Type (whose String() panics with an obscure nil-pointer error and discards the wrapped cause), matching resolverContext.MustGetByType */
        targetTypeString := ""
        if nil != targetType {
            targetTypeString = targetType.String()
        }

        exception.Panic(
            exception.NewError(
                "failed to get service from scope by type",
                map[string]any{
                    "targetType": targetTypeString,
                },
                getByTypeErr,
            ),
        )
    }

    return value
}

func (instance *scope) Has(serviceName string) bool {
    if "" == serviceName {
        return false
    }

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return false
    }

    /* the scope's own answer is read and the lock released before the container is asked, because the container asks the scope back the other way round: a created service is stored into the scope while the container lock is held, so holding the scope lock across a container call is the one ordering that can deadlock */
    instance.mutex.RLock()
    _, exists := instance.instances[serviceName]
    if false == exists {
        _, exists = instance.createdInstances[serviceName]
    }
    if false == exists {
        _, exists = instance.ownProviders[serviceName]
    }
    instance.mutex.RUnlock()

    if true == exists {
        return true
    }

    /* a scoped registration answers before it is built: Has reports what the scope can produce, not what it happens to hold already. The plan is immutable and shared, so it needs no lock. */
    if _, planned := instance.plan.providers[serviceName]; true == planned {
        return true
    }

    return containerInstance.Has(serviceName)
}

func (instance *scope) HasType(targetType reflect.Type) bool {
    if nil == targetType {
        return false
    }

    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return false
    }

    /* every lookup is canonical, because that is the key both the overrides and the registrations are filed under: an override is stored under canonicalServiceType of the value's type, and GetByType canonicalises before it looks. Asking with the value type was answered "no" for a service the very next GetByType resolves. */
    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        return false
    }

    /* the same ordering as Has: the scope answers first and lets its lock go before the container is asked, since the container reaches into the scope while holding its own lock */
    instance.mutex.RLock()
    _, exists := instance.typeInstances[canonicalType]
    if false == exists {
        _, exists = instance.createdTypeInstances[canonicalType]
    }
    if false == exists {
        _, exists = instance.ownTypeRegistrationNamesByType[canonicalType]
    }
    if false == exists {
        _, exists = instance.ownTypeProviders[canonicalType]
    }
    instance.mutex.RUnlock()

    if true == exists {
        return true
    }

    if _, planned := instance.plan.typeRegistrationNamesByType[canonicalType]; true == planned {
        return true
    }

    if _, planned := instance.plan.typeProviders[canonicalType]; true == planned {
        return true
    }

    return containerInstance.HasType(canonicalType)
}

func (instance *scope) OverrideInstance(serviceName string, value any) error {
    return instance.OverrideInstanceWithOptions(serviceName, value)
}

func (instance *scope) OverrideInstanceWithOptions(
    serviceName string,
    value any,
    options ...containercontract.OverrideOption,
) error {
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

    return instance.OverrideProtectedInstanceWithOptions(serviceName, value, options...)
}

func (instance *scope) MustOverrideInstanceWithOptions(
    serviceName string,
    value any,
    options ...containercontract.OverrideOption,
) {
    overrideInstanceErr := instance.OverrideInstanceWithOptions(serviceName, value, options...)
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

func (instance *scope) MustOverrideProtectedInstanceWithOptions(
    serviceName string,
    value any,
    options ...containercontract.OverrideOption,
) {
    overrideInstanceErr := instance.OverrideProtectedInstanceWithOptions(serviceName, value, options...)
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

func (instance *scope) MustOverrideInstance(serviceName string, value any) {
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

func (instance *scope) OverrideProtectedInstance(serviceName string, value any) error {
    return instance.OverrideProtectedInstanceWithOptions(serviceName, value)
}

func (instance *scope) OverrideProtectedInstanceWithOptions(
    serviceName string,
    value any,
    options ...containercontract.OverrideOption,
) error {
    overrideOption := applyOverrideOptions(options)

    if "" == serviceName {
        return exception.NewError(
            "service name is empty in override instance",
            nil,
            nil,
        )
    }

    if nil == value {
        return exception.NewError(
            "value is nil in override instance",
            map[string]any{
                "serviceName": serviceName,
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

    /* the container's registered types are read before the scope lock is taken, because container-then-scope is the only order the two locks are ever taken in; a registration racing this override may miss the propagation, the same window the teardown's graph snapshot accepts. The plan is immutable and needs no lock. */
    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    propagatedTypes := containerInstance.registeredTypesForServiceName(serviceName)

    for registeredType, registeredServiceNames := range instance.plan.typeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                propagatedTypes = append(propagatedTypes, registeredType)
                break
            }
        }
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.container.Load() {
        /* mirror Get: closed scope yields an error, not a panic; MustOverrideProtectedInstance keeps the panic */
        return exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    for registeredType, registeredServiceNames := range instance.ownTypeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                propagatedTypes = append(propagatedTypes, registeredType)
                break
            }
        }
    }

    /* the override propagates to every type this name is registered under, exactly as the container-level sibling propagates — without this, a type-keyed resolution through the scope answered the container's instance while the name answered the override, whenever the container had already memoized the name. The same assignability rule guards the propagation: a value a registered type cannot hold is refused before anything is written, so the name and type maps never learn two different answers. */
    for _, registeredType := range propagatedTypes {
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
    }

    instance.instances[serviceName] = value

    /* an override declared as closed with the scope is filed into the created maps as well, which is the whole of the mechanism: the teardown already walks exactly those, so nothing about closing has to learn about overrides. A created instance the filing evicts is moved to the graveyard the teardown also walks — it left the created maps, but it is the scope's to close, not the overrider's. */
    if true == overrideOption.ClosedWithScope {
        if evictedValue, evicted := instance.createdInstances[serviceName]; true == evicted {
            instance.evictedCreatedInstances = append(instance.evictedCreatedInstances, evictedValue)
        }

        instance.createdInstances[serviceName] = value
        instance.recordCreationOrderLocked(scopedNameNodeKey(serviceName))
    }

    overriddenTypes := make([]reflect.Type, 0, 1+len(propagatedTypes))
    overriddenTypes = append(overriddenTypes, canonicalType)

    seenTypes := map[reflect.Type]struct{}{canonicalType: {}}
    for _, registeredType := range propagatedTypes {
        if _, seen := seenTypes[registeredType]; true == seen {
            continue
        }

        seenTypes[registeredType] = struct{}{}
        overriddenTypes = append(overriddenTypes, registeredType)
    }

    for _, overriddenType := range overriddenTypes {
        instance.typeInstances[overriddenType] = value

        if true == overrideOption.ClosedWithScope {
            if evictedValue, evicted := instance.createdTypeInstances[overriddenType]; true == evicted {
                instance.evictedCreatedInstances = append(instance.evictedCreatedInstances, evictedValue)
            }

            instance.createdTypeInstances[overriddenType] = value
            instance.recordCreationOrderLocked(scopedTypeNodeKey(typeIdentityKey(overriddenType)))
            instance.createdAliasNodeKeys[scopedTypeNodeKey(typeIdentityKey(overriddenType))] = scopedNameNodeKey(serviceName)
        }
    }

    return nil
}

func (instance *scope) MustOverrideProtectedInstance(serviceName string, value any) {
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

/* Closed reports whether Close has ended the request this scope stood for. It lives on the concrete scope rather than the contract, the shape the logger's own liveness question has: a caller that holds something merely promising Resolver or Scope asks through an interface assertion and treats a value that cannot answer as open. The lazy handle is the reader this exists for — a memoized value from a scope that answers true here is a dead request's state and must not be served again. It is the public spelling of the same question isScopeClosed answers for the collection, which stays unexported because a collection refusing a closed scope is a property of collecting, not of the scope's own surface. */
func (instance *scope) Closed() bool {
    return nil == instance.container.Load()
}

/* Close ends the request the scope stands for and closes the services the scope itself built. Only those: an override was installed from outside and belongs to whoever installed it, and a singleton reached through the scope belongs to the root container, which closes it when the process ends — closing either here would tear down, once per request, something the next request still needs. What the scope built is exactly what a service which read one of those entries turned into, so it holds that request and has nobody else to close it. */
func (instance *scope) Close() error {
    /* the dependency graph lives on the scope but is guarded by the CONTAINER mutex, because the resolver writes it with that lock held and never takes the scope's for it. The snapshot is therefore taken container first, scope second — the one order the two locks are ever taken in. A creation racing this Close either has its edge in the snapshot or does not, and a missing edge degrades to the creation order, latest first; that is the same window the created instances themselves already have. */
    dependencyGraph := map[string]map[string]struct{}(nil)

    containerInstance := instance.container.Load()
    if nil != containerInstance {
        containerInstance.mutex.RLock()
        dependencyGraph = make(map[string]map[string]struct{}, len(instance.dependencyGraph))
        for dependentKey, dependencySet := range instance.dependencyGraph {
            copiedDependencies := make(map[string]struct{}, len(dependencySet))
            for dependencyKey := range dependencySet {
                copiedDependencies[dependencyKey] = struct{}{}
            }

            dependencyGraph[dependentKey] = copiedDependencies
        }
        containerInstance.mutex.RUnlock()
    }

    instance.mutex.Lock()

    createdInstances := instance.createdInstances
    createdTypeInstances := instance.createdTypeInstances
    createdAliasNodeKeys := instance.createdAliasNodeKeys
    evictedCreatedInstances := instance.evictedCreatedInstances
    creationOrderByNodeKey := instance.creationOrderByNodeKey

    instance.instances = nil
    instance.typeInstances = nil
    instance.createdInstances = nil
    instance.createdTypeInstances = nil
    instance.createdAliasNodeKeys = nil
    instance.evictedCreatedInstances = nil
    instance.creationOrderByNodeKey = nil
    instance.container.Store(nil)

    /* the lock is released before anything is closed, and the scope is already marked closed above: a Close() that reaches back into the scope then reads a closed scope instead of deadlocking on a mutex its own caller holds, which is the ordering the container's own teardown uses */
    instance.mutex.Unlock()

    return closeCreatedScopeInstances(createdInstances, createdTypeInstances, createdAliasNodeKeys, dependencyGraph, evictedCreatedInstances, creationOrderByNodeKey)
}

/* closeCreatedScopeInstances closes each service the scope built, once. One instance filed under its name and its type is first collapsed onto the name node along the alias links recorded at filing time, with the edges of both nodes merged onto the survivor, so a dependency edge recorded against either alias constrains the one close that happens; whatever identity the links do not cover is still caught by the pointer/value marks at close time. A panicking or failing Close is recorded and the loop carries on, because a request scope closes on the way out of a handler and one bad service must not keep the rest of that request's services alive.

The order is the scope's own dependency graph, dependents before their dependencies: a scoped repository holding a scoped transaction is the ordinary case now that a scope owns registrations, and closing the two by name would be a coin flip. Nodes the graph says nothing about, and nodes left over by a cycle, fall back to creation order, latest first — the same tie-break the container's teardown applies, because the two share one walk. The evicted instances close after the ordered walk, under the same marks. */
func closeCreatedScopeInstances(
    createdInstances map[string]any,
    createdTypeInstances map[reflect.Type]any,
    createdAliasNodeKeys map[string]string,
    dependencyGraph map[string]map[string]struct{},
    evictedCreatedInstances []any,
    creationOrderByNodeKey map[string]int,
) error {
    type closer interface {
        Close() error
    }

    nodeKeys := make([]string, 0, len(createdInstances)+len(createdTypeInstances))
    valueOfNodeKey := make(map[string]any, len(createdInstances)+len(createdTypeInstances))

    for serviceName, value := range createdInstances {
        nodeKey := scopedNameNodeKey(serviceName)
        nodeKeys = append(nodeKeys, nodeKey)
        valueOfNodeKey[nodeKey] = value
    }

    for targetType, value := range createdTypeInstances {
        nodeKey := scopedTypeNodeKey(typeIdentityKey(targetType))
        nodeKeys = append(nodeKeys, nodeKey)
        valueOfNodeKey[nodeKey] = value
    }

    /* the type alias collapses onto its name node only while BOTH still hold the same filing: an eviction may have replaced one of the two, and collapsing across it would close the survivor under the wrong node and skip the evicted value's alias mark */
    representativeOf := make(map[string]string, len(nodeKeys))
    for _, nodeKey := range nodeKeys {
        representativeOf[nodeKey] = nodeKey
    }

    for typeNodeKey, nameNodeKey := range createdAliasNodeKeys {
        typeValue, typeExists := valueOfNodeKey[typeNodeKey]
        nameValue, nameExists := valueOfNodeKey[nameNodeKey]
        if false == typeExists || false == nameExists {
            continue
        }

        if false == sameServiceValue(typeValue, nameValue) {
            continue
        }

        representativeOf[typeNodeKey] = nameNodeKey
    }

    canonicalNodeKeys := make([]string, 0, len(nodeKeys))
    for _, nodeKey := range nodeKeys {
        if nodeKey == representativeOf[nodeKey] {
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)
        }
    }

    canonicalEdges := make(map[string]map[string]struct{}, len(canonicalNodeKeys))
    for dependentKey, dependencySet := range dependencyGraph {
        canonicalDependent, dependentExists := representativeOf[dependentKey]
        if false == dependentExists {
            continue
        }

        for dependencyKey := range dependencySet {
            canonicalDependency, dependencyExists := representativeOf[dependencyKey]
            if false == dependencyExists {
                continue
            }

            dependencies, exists := canonicalEdges[canonicalDependent]
            if false == exists {
                dependencies = make(map[string]struct{})
                canonicalEdges[canonicalDependent] = dependencies
            }

            dependencies[canonicalDependency] = struct{}{}
        }
    }

    /* an alias group is as old as its oldest member, the same rule the container's teardown applies to its own */
    canonicalCreationOrder := make(map[string]int, len(canonicalNodeKeys))

    for nodeKey, canonicalKey := range representativeOf {
        nodeOrder, stamped := creationOrderByNodeKey[nodeKey]
        if false == stamped {
            continue
        }

        existingOrder, hasExisting := canonicalCreationOrder[canonicalKey]
        if false == hasExisting || nodeOrder < existingOrder {
            canonicalCreationOrder[canonicalKey] = nodeOrder
        }
    }

    closeOrder, cycleNodeKeys := teardownCloseOrder(canonicalNodeKeys, canonicalEdges, canonicalCreationOrder)

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    if 0 < len(cycleNodeKeys) {
        failures["scope.dependencyCycle"] = "dependency cycle detected: " + strings.Join(cycleNodeKeys, ", ")
    }

    closeCandidateValue := func(nodeKey string, value any) {
        pointerKey, hasPointer := pointerKeyOf(value)
        if true == hasPointer && true == isZeroSizePointerIdentity(pointerKey) {
            hasPointer = false
        }

        comparableValue := false == hasPointer && false == isZeroSizeValue(value) && true == isComparableValue(value)

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                return
            }

            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[value]; true == alreadyClosed {
                return
            }

            closedValues[value] = struct{}{}
        }

        closeable, isCloseable := value.(closer)
        if false == isCloseable {
            return
        }

        closeErr := closeServiceValue(closeable)
        if nil != closeErr {
            failures[nodeKey] = errorText(closeErr)
        }
    }

    for _, nodeKey := range closeOrder {
        closeCandidateValue(nodeKey, valueOfNodeKey[nodeKey])
    }

    for _, evictedValue := range evictedCreatedInstances {
        closeCandidateValue("scope.evictedInstance", evictedValue)
    }

    if 0 == len(failures) {
        return nil
    }

    return exception.NewError(
        "failed to close scope services",
        exceptioncontract.Context{
            "failures": failures,
        },
        nil,
    )
}

/* sameServiceValue reports whether the two filings hold one instance, deciding it the way the close marks do: pointer identity first, plain equality for comparable non-pointers, and — for the values neither can judge — the fact that the alias link was written by the dual filing itself, which is what the caller falls back on. */
func sameServiceValue(leftValue any, rightValue any) bool {
    leftPointer, leftHasPointer := pointerKeyOf(leftValue)
    rightPointer, rightHasPointer := pointerKeyOf(rightValue)

    if leftHasPointer != rightHasPointer {
        return false
    }

    if true == leftHasPointer {
        return leftPointer == rightPointer
    }

    if true == isComparableValue(leftValue) && true == isComparableValue(rightValue) {
        return leftValue == rightValue
    }

    return true
}

/* TypesImplementing lists what a collection gathered on this scope may hold: the container's type registrations and the scope's own, merged. A scoped registration missing here would be a handler that is simply never dispatched to — no error anywhere, just a message nothing answers — which is why the merge is not an optimisation. A closed scope enumerates nothing, mirroring Has. */
func (instance *scope) TypesImplementing(interfaceType reflect.Type) []reflect.Type {
    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return []reflect.Type{}
    }

    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []reflect.Type{}
    }

    matches := containerInstance.TypesImplementing(interfaceType)

    seen := make(map[reflect.Type]struct{}, len(matches))
    for _, matchedType := range matches {
        seen[matchedType] = struct{}{}
    }

    for _, registeredType := range instance.scopedRegisteredTypes() {
        if false == registeredType.Implements(interfaceType) {
            continue
        }

        if _, alreadyMatched := seen[registeredType]; true == alreadyMatched {
            continue
        }

        seen[registeredType] = struct{}{}
        matches = append(matches, registeredType)
    }

    sort.Slice(matches, func(first int, second int) bool {
        return matches[first].String() < matches[second].String()
    })

    return matches
}

/* ReferencesImplementing lists every registration a collection gathered on this scope may reach, the scope's own before the container's. A name the scope registers answers for that name inside the scope, so the container's registration under the same name is left out rather than listed twice; everything else is merged and ordered by the one shared rule. */
func (instance *scope) ReferencesImplementing(interfaceType reflect.Type) []containercontract.ServiceReference {
    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return []containercontract.ServiceReference{}
    }

    if nil == interfaceType || reflect.Interface != interfaceType.Kind() {
        return []containercontract.ServiceReference{}
    }

    references := instance.scopedPrioritizedReferences(interfaceType)

    scopedServiceNames := make(map[string]struct{}, len(references))
    for _, entry := range references {
        scopedServiceNames[entry.reference.ServiceName] = struct{}{}
    }

    containerInstance.mutex.RLock()
    for registeredType, registeredServiceNames := range containerInstance.typeRegistrationNamesByType {
        if false == registeredType.Implements(interfaceType) {
            continue
        }

        for _, serviceName := range registeredServiceNames {
            if _, shadowed := scopedServiceNames[serviceName]; true == shadowed {
                continue
            }

            references = append(references, prioritizedReference{
                reference: containercontract.ServiceReference{
                    ServiceName: serviceName,
                    ServiceType: registeredType,
                },
                priority: containerInstance.collectionPriorityByName[serviceName],
            })
        }
    }
    containerInstance.mutex.RUnlock()

    return sortServiceReferences(references)
}

/* scopedRegisteredTypes yields every type this scope has a registration for, its own before the plan's. */
func (instance *scope) scopedRegisteredTypes() []reflect.Type {
    registeredTypes := make([]reflect.Type, 0)

    instance.mutex.RLock()
    for registeredType := range instance.ownTypeRegistrationNamesByType {
        registeredTypes = append(registeredTypes, registeredType)
    }
    instance.mutex.RUnlock()

    for registeredType := range instance.plan.typeRegistrationNamesByType {
        registeredTypes = append(registeredTypes, registeredType)
    }

    return registeredTypes
}

func (instance *scope) scopedPrioritizedReferences(interfaceType reflect.Type) []prioritizedReference {
    references := make([]prioritizedReference, 0)

    appendReferences := func(registeredType reflect.Type, registeredServiceNames []string, priorityOf func(serviceName string) int) {
        if false == registeredType.Implements(interfaceType) {
            return
        }

        for _, serviceName := range registeredServiceNames {
            references = append(references, prioritizedReference{
                reference: containercontract.ServiceReference{
                    ServiceName: serviceName,
                    ServiceType: registeredType,
                },
                priority: priorityOf(serviceName),
            })
        }
    }

    instance.mutex.RLock()
    for registeredType, registeredServiceNames := range instance.ownTypeRegistrationNamesByType {
        appendReferences(registeredType, registeredServiceNames, func(serviceName string) int {
            return 0
        })
    }
    instance.mutex.RUnlock()

    ownServiceNames := make(map[string]struct{}, len(references))
    for _, entry := range references {
        ownServiceNames[entry.reference.ServiceName] = struct{}{}
    }

    for registeredType, registeredServiceNames := range instance.plan.typeRegistrationNamesByType {
        plannedServiceNames := make([]string, 0, len(registeredServiceNames))
        for _, serviceName := range registeredServiceNames {
            if _, shadowed := ownServiceNames[serviceName]; true == shadowed {
                continue
            }

            plannedServiceNames = append(plannedServiceNames, serviceName)
        }

        appendReferences(registeredType, plannedServiceNames, func(serviceName string) int {
            return instance.plan.collectionPriorityByName[serviceName]
        })
    }

    return references
}

func (instance *scope) isScopeClosed() bool {
    return nil == instance.container.Load()
}

func (instance *scope) lookupInstanceByName(serviceName string) (any, bool, error) {
    if "" == serviceName {
        return nil, false, exception.NewError(
            "service name is empty in get",
            nil,
            nil,
        )
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if nil == instance.container.Load() {
        return nil, false, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    value, exists := instance.instances[serviceName]
    if true == exists {
        return value, true, nil
    }

    value, exists = instance.createdInstances[serviceName]

    return value, exists, nil
}

func (instance *scope) lookupInstanceByType(canonicalType reflect.Type) (any, bool, error) {
    if nil == canonicalType {
        return nil, false, exception.NewError(
            "service type is required in get by type",
            nil,
            nil,
        )
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    if nil == instance.container.Load() {
        return nil, false, exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    value, exists := instance.typeInstances[canonicalType]
    if true == exists {
        return value, true, nil
    }

    value, exists = instance.createdTypeInstances[canonicalType]

    return value, exists, nil
}

/* storeCreatedInstance keeps a service the resolver built out of this scope's entries. It belongs to the request the scope stands for: the value holds the per-request logger, the request context or whatever else was overridden, and the root container would hand that same instance — carrying one request's identity — to every request for the rest of the process. It is filed under the name, the type, or both, exactly as the root container would have filed it, and it is gone when the scope closes. An override installed while the provider ran occupies the slot already and wins — the value it beat is handed back for the guard to close. A dual filing records its type→name alias link, which is what the teardown collapses along. */
func (instance *scope) storeCreatedInstance(
    serviceName string,
    canonicalType reflect.Type,
    value any,
) (any, bool, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.container.Load() {
        return nil, false, exception.NewError(
            "scope is closed",
            map[string]any{
                "serviceName": serviceName,
            },
            nil,
        )
    }

    if "" != serviceName {
        if existingValue, exists := instance.instances[serviceName]; true == exists {
            return existingValue, true, nil
        }

        instance.createdInstances[serviceName] = value
        instance.recordCreationOrderLocked(scopedNameNodeKey(serviceName))

        if nil != canonicalType {
            instance.createdTypeInstances[canonicalType] = value
            instance.recordCreationOrderLocked(scopedTypeNodeKey(typeIdentityKey(canonicalType)))
            instance.createdAliasNodeKeys[scopedTypeNodeKey(typeIdentityKey(canonicalType))] = scopedNameNodeKey(serviceName)
        }

        return value, false, nil
    }

    if nil != canonicalType {
        if existingValue, exists := instance.typeInstances[canonicalType]; true == exists {
            return existingValue, true, nil
        }

        instance.createdTypeInstances[canonicalType] = value
        instance.recordCreationOrderLocked(scopedTypeNodeKey(typeIdentityKey(canonicalType)))
    }

    return value, false, nil
}

var (
    _ containercontract.Scope                      = (*scope)(nil)
    _ containercontract.ScopedRegistrar            = (*scope)(nil)
    _ containercontract.OverrideServiceWithOptions = (*scope)(nil)
    _ containercontract.TypeLister                 = (*scope)(nil)
    _ closedScopeChecker                           = (*scope)(nil)
)
