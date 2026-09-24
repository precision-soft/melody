package container

import (
    "context"
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
        ownCollectionPriorityByName:    make(map[string]int),

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
    /* the immutable plan this scope was created against, shared by reference so creating a scope is O(1) */
    plan          *scopePlan
    instances     map[string]any
    typeInstances map[reflect.Type]any
    /* the services built through this scope, apart from the overrides installed into it, which outlive the scope */
    createdInstances     map[string]any
    createdTypeInstances map[reflect.Type]any
    /* the type→name link of an instance filed under both, written at the dual filing; the teardown collapses along it so the instance closes once, after its dependents */
    createdAliasNodeKeys map[string]string
    /* a created instance evicted by a ClosedWithScope override is still the scope's to close, after the ordered walk */
    evictedCreatedInstances []any
    /* registrations made on this scope itself, layered over the plan */
    ownProviders                   map[string]providerAny
    ownTypeProviders               map[reflect.Type]providerAny
    ownTypeRegistrationNamesByType map[reflect.Type][]string
    ownReplacesContainerService    map[string]bool
    ownCollectionPriorityByName    map[string]int
    /* creatingByName, creatingByType and dependencyGraph are guarded by the container mutex, not this scope's; Close must never nil them */
    creatingByName  map[string]*creationState
    creatingByType  map[string]*creationState
    dependencyGraph map[string]map[string]struct{}
    /* the order this scope's teardown nodes came into being, written under the scope mutex */
    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
}

/* recordCreationOrderLocked stamps a teardown node the first time only; the scope mutex is held. */
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
        /* a closed scope answers an error, not a panic: a goroutine outliving its request has no recover; the Must wrappers still panic */
        return nil, exception.NewError(
            "scope is closed",
            nil,
            ErrScopeClosed,
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
        /* a closed scope answers an error, as in Get */
        return nil, exception.NewError(
            "scope is closed",
            nil,
            ErrScopeClosed,
        )
    }

    resolver := newScopeResolverContext(containerInstance, instance)

    return resolver.GetByType(targetType)
}

func (instance *scope) MustGetByType(targetType reflect.Type) any {
    value, getByTypeErr := instance.GetByType(targetType)
    if nil != getByTypeErr {
        /* a nil targetType answers a clean error rather than a nil reflect.Type panic */
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

    /* the scope's answer is read and its lock released before the container is asked, since the container reaches into the scope under its own lock */
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

    /* a scoped registration answers before it is built; the plan is immutable and needs no lock */
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

    /* every lookup is canonical, as the overrides and registrations are filed */
    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        return false
    }

    /* the scope answers first and releases its lock before the container is asked */
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

    /* the container's registered types are read before the scope lock, container before scope being the only lock order */
    containerInstance := instance.container.Load()
    if nil == containerInstance {
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "refusedAt": "entry",
            },
            ErrScopeClosed,
        )
    }

    propagatedTypes := containerInstance.registeredTypesForServiceName(serviceName)

    /* the override is exposed under its own canonical type only when no service anywhere registered that type */
    canonicalTypeClaimed := 0 < len(containerInstance.serviceNamesForRegisteredType(canonicalType))
    if false == canonicalTypeClaimed {
        if planNames, exists := instance.plan.typeRegistrationNamesByType[canonicalType]; true == exists && 0 < len(planNames) {
            canonicalTypeClaimed = true
        }
    }

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
        /* a closed scope answers an error, as in Get; the stage tells this refusal from the entry one */
        return exception.NewError(
            "scope is closed",
            map[string]any{
                "refusedAt": "lockHandOff",
            },
            ErrScopeClosed,
        )
    }

    if false == canonicalTypeClaimed {
        if ownNames, exists := instance.ownTypeRegistrationNamesByType[canonicalType]; true == exists && 0 < len(ownNames) {
            canonicalTypeClaimed = true
        }
    }

    for registeredType, registeredServiceNames := range instance.ownTypeRegistrationNamesByType {
        for _, registeredServiceName := range registeredServiceNames {
            if serviceName == registeredServiceName {
                propagatedTypes = append(propagatedTypes, registeredType)
                break
            }
        }
    }

    /* the override reaches every type this name is registered under, and a value a registered type cannot hold is refused before anything is written */
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

    /* an override closed with the scope is filed into the created maps, which the teardown walks; a created instance it evicts goes to the graveyard */
    if true == overrideOption.ClosedWithScope {
        if evictedValue, evicted := instance.createdInstances[serviceName]; true == evicted {
            instance.evictedCreatedInstances = append(instance.evictedCreatedInstances, evictedValue)
        }

        instance.createdInstances[serviceName] = value
        instance.recordCreationOrderLocked(scopedNameNodeKey(serviceName))
    }

    /* exposed under the value's own canonical type only when that type is free */
    overriddenTypes := make([]reflect.Type, 0, 1+len(propagatedTypes))
    seenTypes := map[reflect.Type]struct{}{}

    if false == canonicalTypeClaimed {
        overriddenTypes = append(overriddenTypes, canonicalType)
        seenTypes[canonicalType] = struct{}{}
    }

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

/* Closed reports whether Close has ended the request this scope stood for, so a lazy handle stops serving its memoized value. It is on the concrete scope, asked through a type assertion. */
func (instance *scope) Closed() bool {
    return nil == instance.container.Load()
}

/* Close ends the request the scope stands for and closes only the services the scope built: overrides belong to their installers and singletons to the root container. It is CloseWithContext with no deadline. */
func (instance *scope) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext is Close under the caller's deadline, handed to every scoped service carrying CloseWithContext. It is on the concrete scope, reached through a type assertion. */
func (instance *scope) CloseWithContext(closeContext context.Context) error {
    /* the dependency graph is guarded by the container mutex, so the snapshot takes the container lock first; an edge a racing creation misses degrades to creation order */
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

    /* the lock is released before anything is closed, with the scope already marked closed, so a Close reaching back reads a closed scope */
    instance.mutex.Unlock()

    return closeCreatedScopeInstances(closeContext, createdInstances, createdTypeInstances, createdAliasNodeKeys, dependencyGraph, evictedCreatedInstances, creationOrderByNodeKey)
}

/* closeCreatedScopeInstances closes each service the scope built once, through CloseWithContext when the value carries it and Close otherwise, dependents before dependencies, creation order latest first as the tie-break. An instance filed under name and type is collapsed onto its name node first. A failing or panicking Close is recorded and the loop carries on. The evicted instances close after the ordered walk. */
func closeCreatedScopeInstances(
    closeContext context.Context,
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

    /* the type alias collapses onto its name node only while both still hold the same filing */
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

    /* an alias group is as old as its oldest member */
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

    /* a scope closes serially; the waves the shared drain computes are dropped */
    closeOrder, _, cycleNodeKeys := teardownCloseOrder(canonicalNodeKeys, canonicalEdges, canonicalCreationOrder)

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)
    failureDetails := make(map[string]exceptioncontract.Context)

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

        closeable, contextCloseable, carriesADoor := closeDoorsOf(value)
        if false == carriesADoor {
            return
        }

        closeErr := closeServiceValueWithin(closeContext, closeable, contextCloseable)
        if nil != closeErr {
            failureLine := errorText(closeErr)
            failures[nodeKey] = failureLine
            recordCloseFailureDetails(failureDetails, nodeKey, closeErr, failureLine)
        }
    }

    for _, nodeKey := range closeOrder {
        closeCandidateValue(nodeKey, valueOfNodeKey[nodeKey])
    }

    /* keyed by position, since the evicted instances carry no node key */
    for evictedIndex, evictedValue := range evictedCreatedInstances {
        closeCandidateValue(fmt.Sprintf("scope.evictedInstance[%d]", evictedIndex), evictedValue)
    }

    if 0 == len(failures) {
        return nil
    }

    return exception.NewError(
        "failed to close scope services",
        withCloseFailureDetails(exceptioncontract.Context{
            "failures": failures,
        }, failureDetails),
        nil,
    )
}

/* sameServiceValue reports whether two filings hold one instance: pointer identity, then equality for comparable values; otherwise the caller relies on the alias link. */
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

/* TypesImplementing lists the container's type registrations and the scope's own, merged; a closed scope enumerates nothing. */
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

/* ReferencesImplementing lists every registration a collection on this scope may reach, the scope's own first; a name the scope registers hides the container's registration of it. */
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
            return instance.ownCollectionPriorityByName[serviceName]
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
            ErrScopeClosed,
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
            ErrScopeClosed,
        )
    }

    value, exists := instance.typeInstances[canonicalType]
    if true == exists {
        return value, true, nil
    }

    value, exists = instance.createdTypeInstances[canonicalType]

    return value, exists, nil
}

/* storeCreatedInstance keeps a service built from this scope's entries, filed as the root container would file it and gone when the scope closes. An override installed while the provider ran wins, and the beaten value is handed back to be closed. A dual filing records its type→name alias link. */
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
            ErrScopeClosed,
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
