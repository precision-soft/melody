package container

import (
    "container/heap"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
)

/* recordCreationOrderLocked stamps a teardown node the first time it comes into being; every caller holds the container mutex and writes the stamp on the line that writes the instance maps. An override installed over a built instance keeps the position of the value it replaced, because everything built after that node depends on the name. */
func (instance *container) recordCreationOrderLocked(nodeKey string) {
    if _, stamped := instance.creationOrderByNodeKey[nodeKey]; true == stamped {
        return
    }

    instance.creationOrderCounter = instance.creationOrderCounter + 1
    instance.creationOrderByNodeKey[nodeKey] = instance.creationOrderCounter
}

/* IsClosed reports whether a Close already began tearing the container down. A repeated Close returns the first teardown's error, so a caller that closes defensively asks first, and one failure is not reported twice. */
func (instance *container) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.isClosed
}

/* resolutionsRefused reports whether this container has stopped answering resolutions, which happens later than IsClosed: the two differ for the whole teardown. */
func (instance *container) resolutionsRefused() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownFinished
}

/* Close tears the container down once; a concurrent or repeated call blocks until the first teardown finishes and returns its error. A service whose own Close calls container.Close therefore deadlocks the shutdown, so a service that closes defensively asks IsClosed first, which answers true for the whole teardown. */
func (instance *container) Close() error {
    instance.closeOnce.Do(func() {
        instance.closeErr = instance.closeInternal()
    })

    return instance.closeErr
}

func (instance *container) closeInternal() error {
    type closer interface {
        Close() error
    }

    type closeCandidate struct {
        nodeKey string
        value   any
    }

    instance.mutex.Lock()

    /* mark closed while still holding the lock so the resolver's creation guard refuses new creations for the whole teardown; the sync.Once in Close serializes repeated callers. */
    instance.isClosed = true

    typeStringToType := make(map[string]reflect.Type, len(instance.typeInstances))

    createdNodeKeys := make(
        []string,
        0,
        len(instance.instances)+len(instance.typeInstances),
    )

    for serviceName := range instance.instances {
        createdNodeKeys = append(createdNodeKeys, "service:"+serviceName)
    }

    for targetType := range instance.typeInstances {
        typeKey := typeIdentityKey(targetType)
        typeStringToType[typeKey] = targetType
        createdNodeKeys = append(createdNodeKeys, "type:"+typeKey)
    }

    sort.Slice(
        createdNodeKeys,
        func(leftIndex int, rightIndex int) bool {
            return createdNodeKeys[leftIndex] > createdNodeKeys[rightIndex]
        },
    )

    resolveNodeValue := func(nodeKey string) (any, bool) {
        if true == strings.HasPrefix(nodeKey, "service:") {
            serviceName := strings.TrimPrefix(nodeKey, "service:")
            instanceValue, exists := instance.instances[serviceName]
            return instanceValue, exists
        }

        if true == strings.HasPrefix(nodeKey, "type:") {
            typeKey := strings.TrimPrefix(nodeKey, "type:")
            targetType, typeExists := typeStringToType[typeKey]
            if false == typeExists {
                return nil, false
            }

            instanceValue, exists := instance.typeInstances[targetType]
            return instanceValue, exists
        }

        return nil, false
    }

    /* the same instance can be filed under several node keys (a named service that also registers its type lives under "service:<name>" and "type:<T>"); the aliases collapse onto one representative, so an edge recorded against any of them constrains the one close of the shared instance. The type node collapses onto its name node structurally, through typeRegistrationNamesByType, which holds for a value-type service whose contents are not hashable; pointer and value identity group the remaining aliases. */
    valueOfNodeKey := make(map[string]any, len(createdNodeKeys))
    representativeOf := make(map[string]string, len(createdNodeKeys))
    pointerRepresentative := make(map[pointerIdentity]string, len(createdNodeKeys))
    pointerGroupMembers := make(map[pointerIdentity][]string, len(createdNodeKeys))
    valueRepresentative := make(map[any]string, len(createdNodeKeys))
    canonicalNodeKeys := make([]string, 0, len(createdNodeKeys))

    nodesAreRelated := func(leftNodeKey string, rightNodeKey string) bool {
        if dependencies, exists := instance.dependencyGraph[leftNodeKey]; true == exists {
            if _, related := dependencies[rightNodeKey]; true == related {
                return true
            }
        }

        if dependencies, exists := instance.dependencyGraph[rightNodeKey]; true == exists {
            if _, related := dependencies[leftNodeKey]; true == related {
                return true
            }
        }

        return false
    }

    /* every zero-size allocation shares one address, so the address plus the type still cannot tell two distinct services of such a type apart; a genuine alias is one whose value came through the resolver from the other service, which is exactly the case the dependency graph records, so an unrelated node keeps its own representative and is closed on its own */
    zeroSizeAliasRepresentative := func(nodeKey string, pointerKey pointerIdentity) (string, bool) {
        for _, memberNodeKey := range pointerGroupMembers[pointerKey] {
            if true == nodesAreRelated(nodeKey, memberNodeKey) {
                return representativeOf[memberNodeKey], true
            }
        }

        return "", false
    }

    assignRepresentative := func(nodeKey string, value any) {
        if pointerKey, hasPointer := pointerKeyOf(value); true == hasPointer {
            if existingRepresentative, alreadyGrouped := pointerRepresentative[pointerKey]; true == alreadyGrouped {
                if false == isZeroSizePointerIdentity(pointerKey) {
                    representativeOf[nodeKey] = existingRepresentative
                    pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)

                    return
                }

                if aliasRepresentative, aliased := zeroSizeAliasRepresentative(nodeKey, pointerKey); true == aliased {
                    representativeOf[nodeKey] = aliasRepresentative
                    pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)

                    return
                }

                representativeOf[nodeKey] = nodeKey
                pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)
                canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

                return
            }

            pointerRepresentative[pointerKey] = nodeKey
            pointerGroupMembers[pointerKey] = append(pointerGroupMembers[pointerKey], nodeKey)
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            return
        }

        if true == isComparableValue(value) {
            if existingRepresentative, alreadyGrouped := valueRepresentative[value]; true == alreadyGrouped {
                representativeOf[nodeKey] = existingRepresentative

                return
            }

            valueRepresentative[value] = nodeKey
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            return
        }

        representativeOf[nodeKey] = nodeKey
        canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)
    }

    typeAliasRepresentative := func(typeNodeKey string) (string, bool) {
        typeKey := strings.TrimPrefix(typeNodeKey, "type:")
        targetType, typeExists := typeStringToType[typeKey]
        if false == typeExists {
            return "", false
        }

        for _, serviceName := range instance.typeRegistrationNamesByType[targetType] {
            if existingRepresentative, hasRepresentative := representativeOf["service:"+serviceName]; true == hasRepresentative {
                return existingRepresentative, true
            }
        }

        return "", false
    }

    for _, nodeKey := range createdNodeKeys {
        if false == strings.HasPrefix(nodeKey, "service:") {
            continue
        }

        value, exists := resolveNodeValue(nodeKey)
        if false == exists {
            continue
        }

        valueOfNodeKey[nodeKey] = value
        assignRepresentative(nodeKey, value)
    }

    for _, nodeKey := range createdNodeKeys {
        if false == strings.HasPrefix(nodeKey, "type:") {
            continue
        }

        value, exists := resolveNodeValue(nodeKey)
        if false == exists {
            continue
        }

        valueOfNodeKey[nodeKey] = value

        if aliasRepresentative, aliased := typeAliasRepresentative(nodeKey); true == aliased {
            representativeOf[nodeKey] = aliasRepresentative

            continue
        }

        assignRepresentative(nodeKey, value)
    }

    /* the graph is stated in the container's own node keys, so it is translated into the canonical keys the teardown walks — the ones an alias of the same instance was collapsed onto — before the shared ordering runs over it */
    canonicalEdges := make(map[string]map[string]struct{}, len(canonicalNodeKeys))

    for dependentKey, dependencySet := range instance.dependencyGraph {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        if false == dependentCreated {
            continue
        }

        for dependencyKey := range dependencySet {
            canonicalDependency, dependencyCreated := representativeOf[dependencyKey]
            if false == dependencyCreated {
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

    /* an alias group is as old as its OLDEST member: the same instance filed under a name and under a type came into being once, and the stamp of the later filing would claim it was built after services that were in fact built from it */
    canonicalCreationOrder := make(map[string]int, len(canonicalNodeKeys))

    for nodeKey, canonicalKey := range representativeOf {
        nodeOrder, stamped := instance.creationOrderByNodeKey[nodeKey]
        if false == stamped {
            continue
        }

        existingOrder, hasExisting := canonicalCreationOrder[canonicalKey]
        if false == hasExisting || nodeOrder < existingOrder {
            canonicalCreationOrder[canonicalKey] = nodeOrder
        }
    }

    closeOrder, remaining := teardownCloseOrder(canonicalNodeKeys, canonicalEdges, canonicalCreationOrder)

    dependencyCycleDetected := 0 < len(remaining)

    candidates := make([]closeCandidate, 0, len(closeOrder)+len(instance.replacedBuiltInstances))

    for _, nodeKey := range closeOrder {
        value, exists := valueOfNodeKey[nodeKey]
        if false == exists {
            continue
        }

        candidates = append(
            candidates,
            closeCandidate{
                nodeKey: nodeKey,
                value:   value,
            },
        )
    }

    /* the container-built instances an override evicted from the maps close after the ordered walk: they carry no edges anymore, and the identity marks below keep an instance a provider handed to several names from closing twice. */
    for replacedIndex, replacedValue := range instance.replacedBuiltInstances {
        candidates = append(
            candidates,
            closeCandidate{
                /* keyed by position: the replaced instances carry no node key, and each failed close keeps its own record in the failure map. */
                nodeKey: fmt.Sprintf("container.replacedInstance[%d]", replacedIndex),
                value:   replacedValue,
            },
        )
    }

    instance.mutex.Unlock()

    closedPointers := make(map[pointerIdentity]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    for _, candidate := range candidates {
        pointerKey, hasPointer := pointerKeyOf(candidate.value)
        if true == hasPointer && true == isZeroSizePointerIdentity(pointerKey) {
            hasPointer = false
        }

        comparableValue := false == hasPointer && false == isZeroSizeValue(candidate.value) && true == isComparableValue(candidate.value)

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                continue
            }
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[candidate.value]; true == alreadyClosed {
                continue
            }
        }

        closeable, isCloseable := candidate.value.(closer)
        if false == isCloseable {
            if true == hasPointer {
                closedPointers[pointerKey] = struct{}{}
            } else if true == comparableValue {
                closedValues[candidate.value] = struct{}{}
            }

            continue
        }

        closeErr := closeServiceValue(closeable)
        if nil != closeErr {
            failures[candidate.nodeKey] = errorText(closeErr)
        }

        if true == hasPointer {
            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            closedValues[candidate.value] = struct{}{}
        }
    }

    var resultErr error

    if true == dependencyCycleDetected {
        if 0 == len(failures) {
            resultErr = exception.NewError(
                "container close dependency cycle detected",
                exceptioncontract.Context{
                    "nodes": remaining,
                },
                nil,
            )
        } else {
            /* the node list is kept beside the failures, so a close that both failed and cycled reports which services cycled as well as which failed. */
            failures["container.dependencyCycle"] = "dependency cycle detected: " + strings.Join(remaining, ", ")
        }
    }

    if nil == resultErr && 0 < len(failures) {
        resultErr = exception.NewError(
            "failed to close container services",
            exceptioncontract.Context{
                "failures": failures,
            },
            nil,
        )
    }

    /* the second closing state is taken only after the last Close returned: from here a resolution is refused rather than answered out of the maps. */
    instance.mutex.Lock()
    instance.teardownFinished = true
    instance.mutex.Unlock()

    return resultErr
}

/* closeServiceValue contains a panicking Close as a recorded failure, so the teardown still closes the remaining services. It is a containment boundary: an error-shaped panic value travels as the cause, and the stack is captured inside the recover, the only place its frames still exist, as the event dispatcher and the http kernel do at theirs. */
func closeServiceValue(closeable interface{ Close() error }) (closeErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        closeErr = exception.NewError(
            "service close panicked",
            exceptioncontract.Context{
                "recoveredType":  fmt.Sprintf("%T", recoveredValue),
                "recoveredValue": fmt.Sprintf("%v", recoveredValue),
                "panicStack":     string(debug.Stack()),
            },
            exception.PanicCause(recoveredValue),
        )
    }()

    return closeable.Close()
}

/* a user error whose Error() panics must not abort the teardown loop, so the recorded failure text is produced under a recover. */
func errorText(err error) (text string) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        text = fmt.Sprintf("close error message panicked: %v", recoveredValue)
    }()

    return err.Error()
}

type nodeKeyHeap struct {
    items           []string
    creationOrderOf map[string]int
}

func (instance *nodeKeyHeap) Len() int {
    return len(instance.items)
}

func (instance *nodeKeyHeap) Less(leftIndex int, rightIndex int) bool {
    return closesBefore(instance.creationOrderOf, instance.items[leftIndex], instance.items[rightIndex])
}

/* closesBefore answers which of two services with no edge between them closes first: the one created later. Creation order is the only order here with a causal claim, since a service built during the construction of another was needed by it. A node with no recorded creation is ordered as created first, so it closes last, and between two such nodes the node key decides. */
func closesBefore(creationOrderOf map[string]int, leftNodeKey string, rightNodeKey string) bool {
    leftOrder := creationOrderOf[leftNodeKey]
    rightOrder := creationOrderOf[rightNodeKey]

    if leftOrder != rightOrder {
        return leftOrder > rightOrder
    }

    return leftNodeKey > rightNodeKey
}

func (instance *nodeKeyHeap) Swap(leftIndex int, rightIndex int) {
    instance.items[leftIndex], instance.items[rightIndex] = instance.items[rightIndex], instance.items[leftIndex]
}

func (instance *nodeKeyHeap) Push(value any) {
    instance.items = append(instance.items, value.(string))
}

func (instance *nodeKeyHeap) Pop() any {
    lastIndex := len(instance.items) - 1
    value := instance.items[lastIndex]
    instance.items = instance.items[:lastIndex]
    return value
}

func isComparableValue(value any) bool {
    if nil == value {
        return false
    }

    return reflect.ValueOf(value).Comparable()
}

type pointerIdentity struct {
    pointer   uintptr
    valueType reflect.Type
}

func isZeroSizePointerIdentity(identity pointerIdentity) bool {
    if nil == identity.valueType || reflect.Pointer != identity.valueType.Kind() {
        return false
    }

    return 0 == identity.valueType.Elem().Size()
}

func isZeroSizeValue(value any) bool {
    if true == internal.IsNilInterface(value) {
        return false
    }

    reflected := reflect.ValueOf(value)

    for reflect.Interface == reflected.Kind() {
        reflected = reflected.Elem()
    }

    if reflect.Pointer != reflected.Kind() || true == reflected.IsNil() {
        return false
    }

    return 0 == reflected.Type().Elem().Size()
}

func pointerKeyOf(value any) (pointerIdentity, bool) {
    if true == internal.IsNilInterface(value) {
        return pointerIdentity{}, false
    }

    reflected := reflect.ValueOf(value)

    for reflect.Interface == reflected.Kind() {
        reflected = reflected.Elem()
    }

    if reflect.Pointer == reflected.Kind() && false == reflected.IsNil() {
        return pointerIdentity{pointer: reflected.Pointer(), valueType: reflected.Type()}, true
    }

    return pointerIdentity{}, false
}

/* teardownCloseOrder orders a set of created services for closing: a dependent before everything it depends on, ties broken by creation order, latest first (see closesBefore). An edge naming a node that was not created is dropped, a self-edge is ignored, and what a cycle leaves is returned separately so the caller can close it and report it. The container and the request scope share this walk, so their two teardowns cannot order differently. */
func teardownCloseOrder(
    nodeKeys []string,
    edges map[string]map[string]struct{},
    creationOrderOf map[string]int,
) ([]string, []string) {
    if nil == creationOrderOf {
        creationOrderOf = map[string]int{}
    }

    adjacency := make(map[string]map[string]struct{}, len(nodeKeys))
    inDegree := make(map[string]int, len(nodeKeys))

    for _, nodeKey := range nodeKeys {
        inDegree[nodeKey] = 0
    }

    for dependentKey, dependencySet := range edges {
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
        items:           available,
        creationOrderOf: creationOrderOf,
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
            return closesBefore(creationOrderOf, cycleNodeKeys[leftIndex], cycleNodeKeys[rightIndex])
        },
    )

    closeOrder = append(closeOrder, cycleNodeKeys...)

    return closeOrder, cycleNodeKeys
}
