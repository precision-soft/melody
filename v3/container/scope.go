package container

import (
    "container/heap"
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

        ownProviders:                   make(map[string]providerAny),
        ownTypeProviders:               make(map[reflect.Type]providerAny),
        ownTypeRegistrationNamesByType: make(map[reflect.Type][]string),
        ownReplacesContainerService:    make(map[string]bool),

        creatingByName:  make(map[string]*creationState),
        creatingByType:  make(map[string]*creationState),
        dependencyGraph: make(map[string]map[string]struct{}),
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
    /* registrations made on this scope itself, layered over the plan. They are the rare case: the declared plan is where a scoped service normally comes from. */
    ownProviders                   map[string]providerAny
    ownTypeProviders               map[reflect.Type]providerAny
    ownTypeRegistrationNamesByType map[reflect.Type][]string
    ownReplacesContainerService    map[string]bool
    /* @important creatingByName, creatingByType and dependencyGraph are guarded by the CONTAINER mutex, not by this scope's. The creation guard that reads and writes them runs with the container lock held and releases it only around the provider call, so that is the only lock they are ever touched under. Close() must therefore never nil them: it holds the scope lock alone, and emptying a map a guard is writing to would be a data race with no lock in common. They die with the scope instead. */
    creatingByName  map[string]*creationState
    creatingByType  map[string]*creationState
    dependencyGraph map[string]map[string]struct{}
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
        /* @important return the closed-scope error instead of panicking: error-returning methods follow the Must/non-Must convention (Must* wrappers keep panicking), and a panic here is fatal in handler-spawned goroutines that outlive the request — the kernel closes the scope when ServeHttp returns and no recover covers those goroutines */
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
        /* @important mirror Get: closed scope yields an error, not a panic; MustGetByType keeps the panic */
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
        /* @important a nil targetType yields a clean GetByType error, so guard the type string here too rather than dereferencing a nil reflect.Type (whose String() panics with an obscure nil-pointer error and discards the wrapped cause), matching resolverContext.MustGetByType */
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

    /* @important the scope's own answer is read and the lock released before the container is asked, because the container asks the scope back the other way round: a created service is stored into the scope while the container lock is held, so holding the scope lock across a container call is the one ordering that can deadlock */
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

    /* @important the same ordering as Has: the scope answers first and lets its lock go before the container is asked, since the container reaches into the scope while holding its own lock */
    instance.mutex.RLock()
    _, exists := instance.typeInstances[targetType]
    if false == exists {
        _, exists = instance.createdTypeInstances[targetType]
    }
    instance.mutex.RUnlock()

    if true == exists {
        return true
    }

    /* @important the scoped lookups canonicalise the type, because that is the key the registrations are filed under. The two lookups above deliberately do not, mirroring container.HasType — a pre-existing gap that is not this branch's to inherit. */
    canonicalType := canonicalServiceType(targetType)
    if nil != canonicalType {
        instance.mutex.RLock()
        _, scopedExists := instance.ownTypeRegistrationNamesByType[canonicalType]
        if false == scopedExists {
            _, scopedExists = instance.ownTypeProviders[canonicalType]
        }
        instance.mutex.RUnlock()

        if true == scopedExists {
            return true
        }

        if _, planned := instance.plan.typeRegistrationNamesByType[canonicalType]; true == planned {
            return true
        }

        if _, planned := instance.plan.typeProviders[canonicalType]; true == planned {
            return true
        }
    }

    return containerInstance.HasType(targetType)
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

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.container.Load() {
        /* @important mirror Get: closed scope yields an error, not a panic; MustOverrideProtectedInstance keeps the panic */
        return exception.NewError(
            "scope is closed",
            nil,
            nil,
        )
    }

    instance.instances[serviceName] = value

    /* an override declared as closed with the scope is filed into the created maps as well, which is the whole of the mechanism: the teardown already walks exactly those, so nothing about closing has to learn about overrides. */
    if true == overrideOption.ClosedWithScope {
        instance.createdInstances[serviceName] = value
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

    instance.typeInstances[canonicalType] = value

    if true == overrideOption.ClosedWithScope {
        instance.createdTypeInstances[canonicalType] = value
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

/* Close ends the request the scope stands for and closes the services the scope itself built. Only those: an override was installed from outside and belongs to whoever installed it, and a singleton reached through the scope belongs to the root container, which closes it when the process ends — closing either here would tear down, once per request, something the next request still needs. What the scope built is exactly what a service which read one of those entries turned into, so it holds that request and has nobody else to close it. */
func (instance *scope) Close() error {
    /* @important the dependency graph lives on the scope but is guarded by the CONTAINER mutex, because the resolver writes it with that lock held and never takes the scope's for it. The snapshot is therefore taken container first, scope second — the one order the two locks are ever taken in. A creation racing this Close either has its edge in the snapshot or does not, and a missing edge degrades to the descending-name order; that is the same window the created instances themselves already have. */
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

    instance.instances = nil
    instance.typeInstances = nil
    instance.createdInstances = nil
    instance.createdTypeInstances = nil
    instance.container.Store(nil)

    /* the lock is released before anything is closed, and the scope is already marked closed above: a Close() that reaches back into the scope then reads a closed scope instead of deadlocking on a mutex its own caller holds, which is the ordering the container's own teardown uses */
    instance.mutex.Unlock()

    return closeCreatedScopeInstances(createdInstances, createdTypeInstances, dependencyGraph)
}

/* closeCreatedScopeInstances closes each service the scope built, once. The same instance is filed under its name and under its type whenever both were known, so the aliases are collapsed the way the container's teardown collapses them — by pointer identity, or by value for a comparable non-pointer — before Close is called. A panicking or failing Close is recorded and the loop carries on, because a request scope closes on the way out of a handler and one bad service must not keep the rest of that request's services alive.

The order is the scope's own dependency graph, dependents before their dependencies: a scoped repository holding a scoped transaction is the ordinary case now that a scope owns registrations, and closing the two by name would be a coin flip. Nodes the graph says nothing about, and nodes left over by a cycle, fall back to the sorted node key descending — stable, and the order this walk used before there was a graph at all. */
func closeCreatedScopeInstances(
    createdInstances map[string]any,
    createdTypeInstances map[reflect.Type]any,
    dependencyGraph map[string]map[string]struct{},
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

    closeOrder, cycleNodeKeys := scopeCloseOrder(nodeKeys, dependencyGraph)

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    if 0 < len(cycleNodeKeys) {
        failures["scope.dependencyCycle"] = "dependency cycle detected"
    }

    for _, nodeKey := range closeOrder {
        value := valueOfNodeKey[nodeKey]

        pointerKey, hasPointer := pointerKeyOf(value)
        if true == hasPointer && true == isZeroSizePointerIdentity(pointerKey) {
            hasPointer = false
        }

        comparableValue := false == hasPointer && false == isZeroSizeValue(value) && true == isComparableValue(value)

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                continue
            }

            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[value]; true == alreadyClosed {
                continue
            }

            closedValues[value] = struct{}{}
        }

        closeable, isCloseable := value.(closer)
        if false == isCloseable {
            continue
        }

        closeErr := closeServiceValue(closeable)
        if nil != closeErr {
            failures[nodeKey] = errorText(closeErr)
        }
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

/* scopeCloseOrder puts the services the scope built into the order they have to be closed in: a dependent before everything it depends on, so nothing is torn down while something still using it is alive. Ties are broken by the node key descending, which is what the walk did before it had a graph, so adding an edge never reshuffles the services around it.

An edge naming a node the scope did not build is dropped rather than followed: a scoped service reaching a container singleton records nothing here, and a dependency whose creation failed was never stored. What a cycle leaves behind is closed last and reported, exactly as the container's teardown reports its own. */
func scopeCloseOrder(
    nodeKeys []string,
    dependencyGraph map[string]map[string]struct{},
) ([]string, []string) {
    adjacency := make(map[string]map[string]struct{}, len(nodeKeys))
    inDegree := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range nodeKeys {
        inDegree[nodeKey] = 0
    }

    for dependentKey, dependencySet := range dependencyGraph {
        if _, created := inDegree[dependentKey]; false == created {
            continue
        }

        for dependencyKey := range dependencySet {
            if _, created := inDegree[dependencyKey]; false == created {
                continue
            }

            if dependentKey == dependencyKey {
                continue
            }

            dependencies, exists := adjacency[dependentKey]
            if false == exists {
                dependencies = make(map[string]struct{})
                adjacency[dependentKey] = dependencies
            }

            if _, alreadyAdded := dependencies[dependencyKey]; true == alreadyAdded {
                continue
            }

            dependencies[dependencyKey] = struct{}{}
            inDegree[dependencyKey] = inDegree[dependencyKey] + 1
        }
    }

    available := make([]string, 0, len(nodeKeys))
    for nodeKey, degree := range inDegree {
        if 0 == degree {
            available = append(available, nodeKey)
        }
    }

    availableHeap := &nodeKeyHeap{
        items: available,
    }
    heap.Init(availableHeap)

    closeOrder := make([]string, 0, len(nodeKeys))

    for 0 < availableHeap.Len() {
        current := heap.Pop(availableHeap).(string)

        closeOrder = append(closeOrder, current)

        dependencies, exists := adjacency[current]
        if false == exists {
            continue
        }

        for dependencyKey := range dependencies {
            inDegree[dependencyKey] = inDegree[dependencyKey] - 1
            if 0 == inDegree[dependencyKey] {
                heap.Push(
                    availableHeap,
                    dependencyKey,
                )
            }
        }
    }

    cycleNodeKeys := make([]string, 0)
    for nodeKey, degree := range inDegree {
        if 0 < degree {
            cycleNodeKeys = append(cycleNodeKeys, nodeKey)
        }
    }

    sort.Slice(
        cycleNodeKeys,
        func(leftIndex int, rightIndex int) bool {
            return cycleNodeKeys[leftIndex] > cycleNodeKeys[rightIndex]
        },
    )

    closeOrder = append(closeOrder, cycleNodeKeys...)

    return closeOrder, cycleNodeKeys
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

/* storeCreatedInstance keeps a service the resolver built out of this scope's entries. It belongs to the request the scope stands for: the value holds the per-request logger, the request context or whatever else was overridden, and the root container would hand that same instance — carrying one request's identity — to every request for the rest of the process. It is filed under the name, the type, or both, exactly as the root container would have filed it, and it is gone when the scope closes. */
func (instance *scope) storeCreatedInstance(
    serviceName string,
    canonicalType reflect.Type,
    value any,
) error {
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

    if "" != serviceName {
        instance.createdInstances[serviceName] = value
    }

    if nil != canonicalType {
        instance.createdTypeInstances[canonicalType] = value
    }

    return nil
}

var _ containercontract.Scope = (*scope)(nil)
var _ containercontract.TypeLister = (*scope)(nil)
