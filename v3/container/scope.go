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

    plan          *scopePlan
    instances     map[string]any
    typeInstances map[reflect.Type]any

    createdInstances     map[string]any
    createdTypeInstances map[reflect.Type]any

    createdAliasNodeKeys map[string]string

    evictedCreatedInstances []any

    ownProviders                   map[string]providerAny
    ownTypeProviders               map[reflect.Type]providerAny
    ownTypeRegistrationNamesByType map[reflect.Type][]string
    ownReplacesContainerService    map[string]bool
    ownCollectionPriorityByName    map[string]int

    creatingByName  map[string]*creationState
    creatingByType  map[string]*creationState
    dependencyGraph map[string]map[string]struct{}

    creationOrderByNodeKey map[string]int
    creationOrderCounter   int
}

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

    canonicalType := canonicalServiceType(targetType)
    if nil == canonicalType {
        return false
    }

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

    if true == overrideOption.ClosedWithScope {
        if evictedValue, evicted := instance.createdInstances[serviceName]; true == evicted {
            instance.evictedCreatedInstances = append(instance.evictedCreatedInstances, evictedValue)
        }

        instance.createdInstances[serviceName] = value
        instance.recordCreationOrderLocked(scopedNameNodeKey(serviceName))
    }

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

/* Closed reports whether Close has ended the request this scope stood for. It lives on the concrete scope rather than the contract, the shape the logger's own liveness question has: a caller that holds something merely promising Resolver or Scope asks through an interface assertion and treats a value that cannot answer as open. The lazy handle is the reader this exists for — a memoized value from a scope that answers true here is a dead request's state and must not be served again. It is the public spelling of the same question isScopeClosed answers for the collection, which stays unexported because a collection refusing a closed scope is a property of collecting, not of the scope's own surface. */
func (instance *scope) Closed() bool {
    return nil == instance.container.Load()
}

/* Close closes services built by this scope, leaving container singletons and borrowed overrides to their owners. It delegates to CloseWithContext with no deadline; explicitly scope-owned overrides close with the scope. */
func (instance *scope) Close() error {
    return instance.CloseWithContext(context.Background())
}

/* CloseWithContext closes the scope using one shared context for services that implement both Close and CloseWithContext. Services with only Close retain their own timing; the context cannot interrupt them. An expired context is still passed to each context-aware service, without falling back to Close. The optional method lives on the concrete scope so existing Scope implementations remain compatible. */
func (instance *scope) CloseWithContext(closeContext context.Context) error {

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

    instance.mutex.Unlock()

    return closeCreatedScopeInstances(closeContext, createdInstances, createdTypeInstances, createdAliasNodeKeys, dependencyGraph, evictedCreatedInstances, creationOrderByNodeKey)
}

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

    closeOrder, _, cycleNodeKeys := teardownCloseOrder(canonicalNodeKeys, canonicalEdges, canonicalCreationOrder)

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
            if _, supported := value.(contextCloser); false == supported { return }
        }

        closeErr := closeServiceValueWithin(closeContext, value, closeable)
        if nil != closeErr {
            failures[nodeKey] = errorText(closeErr)
        }
    }

    for _, nodeKey := range closeOrder {
        closeCandidateValue(nodeKey, valueOfNodeKey[nodeKey])
    }

    for evictedIndex, evictedValue := range evictedCreatedInstances {
        closeCandidateValue(fmt.Sprintf("scope.evictedInstance[%d]", evictedIndex), evictedValue)
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

/* ReferencesImplementing merges scoped and container registrations, suppressing container names shadowed by the scope and applying the shared ordering rule. */
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
