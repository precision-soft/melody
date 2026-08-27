package container

import (
    "container/heap"
    "fmt"
    "reflect"
    "runtime/debug"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
)

/* recordCreationOrderLocked stamps a teardown node with the moment it came into being, the first time it does. The container mutex is held by every caller: the stamp is written on the same line the instance maps are written, so a node cannot exist without one. A node stamped twice would claim to have been created when it was merely re-filed — an override installed over a built instance is a new value under an old node, and it keeps the position the value it replaced held, because everything built after that node still depends on the NAME. */
func (instance *container) recordCreationOrderLocked(nodeKey string) {
    if _, stamped := instance.creationOrderByNodeKey[nodeKey]; true == stamped {
        return
    }

    instance.creationOrderCounter = instance.creationOrderCounter + 1
    instance.creationOrderByNodeKey[nodeKey] = instance.creationOrderCounter
}

/* IsClosed reports whether a Close already began tearing the container down. Because a repeated Close returns the first teardown's memoized error, a caller that closes defensively cannot tell a failure it just caused from one somebody else already discovered and reported; asking before closing is what keeps one failure from being presented as two incidents. */
func (instance *container) IsClosed() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.isClosed
}

/* Close tears the container down exactly once. A concurrent or repeated call blocks until the first teardown finishes and returns the same error, so a second caller never reports a premature success while services are still being closed.

   That blocking makes Close re-entrant-unsafe by construction: a service whose own Close calls back into container.Close re-enters the teardown that is waiting on it and deadlocks the whole shutdown. A service that closes defensively asks IsClosed first — the flag is set before the first service Close runs, so during the teardown it already answers true and the defensive caller skips. The scope resolves the same re-entrance by reading a closed scope instead of blocking, but its second caller may also return while services are still closing; this container keeps the stronger contract for its concurrent callers and leaves re-entrance to the IsClosed protocol. */
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

    /* the same instance can be created under several node keys (a named service that also registers its type lives under both "service:<name>" and "type:<T>"); collapse those aliases onto one representative so a dependency edge recorded against any alias constrains the close order of the shared instance and it is closed exactly once in dependent-before-dependency order. The "type:<T>" node is collapsed onto its backing "service:<name>" structurally (via typeRegistrationNamesByType), which is correct even for a value-type service whose dynamic contents are not hashable; pointer/value identity then groups any remaining same-instance aliases */
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
                /* keyed by position: the replaced instances carry no node key, and one shared constant key let a second replaced close that failed overwrite the first's record in the failure map, naming one failure where two happened */
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
            /* the node list survives alongside the failures: with it dropped, the one close that both failed and cycled reported WHICH services failed but not which ones cycled, and the operator got half the diagnosis */
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

    /* the second closing state is taken only now, after the last Close returned: from here a resolution is refused rather than answered out of the maps, which is what the whole teardown just emptied of meaning */
    instance.mutex.Lock()
    instance.teardownFinished = true
    instance.mutex.Unlock()

    return resultErr
}

/* contain a panicking Close() as a recorded failure so the teardown loop still closes the remaining services and closeErr is assigned.

   What the failure carries is the whole of what the operator will ever learn about it: this is a containment boundary, so nothing above it sees the panic and nothing below it survives. An error-shaped panic value therefore travels as the CAUSE rather than only as its own stringified message — kept only in a context slot it collapses to one line at the render boundary, so the context map and the cause chain of the very error the Close raised reached no record at all — and the stack is captured here, inside the recover, because it is the only place the frames that ran still exist. The recovery boundaries of the event dispatcher and of the http kernel make the same two decisions for the same reason. */
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

/* closesBefore answers which of two services with no edge between them is torn down first: the one created LATER. Creation order is the only order in this container that carries a causal claim — a service built during the construction of another was needed by it, whether or not the edge was declared, and a logger resolved at boot is beneath everything resolved afterwards. The comparison this replaced was on the node key descending, which is a string comparison nobody wrote and which decided, by nothing but spelling, that a worker named app.worker lost its shutdown records while the same worker renamed zz.worker kept them.

   A node with no recorded creation is ordered as if created first, which closes it last: the only nodes without one are those the maps gained outside a creation, and there the key keeps deciding, exactly as before. */
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

/* teardownCloseOrder puts a set of created services into the order they have to be closed in: a dependent before everything it depends on, so nothing is torn down while something still using it is alive. Ties are broken by creation order, latest first — see closesBefore for why that and not the node key.

   The edges are expected in the same key space as the nodes; an edge naming a node that was not created is dropped rather than followed, and a self-edge is ignored. What a cycle leaves behind is returned separately and appended last, so the caller can both close it and report it.

   The container and the request scope share this walk because they answer the same question about different sets: the container asks it of everything it built for the process, the scope of everything it built for one request. Two implementations of it would be two chances to order a teardown differently. */
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
