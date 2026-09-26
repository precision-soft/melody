package container

import (
    "reflect"
    "slices"
    "strings"
    "sync"
)

/* teardownWalkDepthLimit, teardownWalkElementLimit and teardownWalkNodeBudget bound the cost of the walk, never its correctness: an edge not reached is an edge not gained. The budget is charged when an item is queued, so the queue itself stays bounded. */
const teardownWalkDepthLimit = 12

const teardownWalkElementLimit = 256

const teardownWalkNodeBudget = 65536

/* heldPointer is one pointer a service was seen to hold: its identity and a reflect.Value of its target, which keeps the target alive so the address cannot be reused by another allocation. */
type heldPointer struct {
    identity  pointerIdentity
    keepAlive reflect.Value
}

/* heldPointerIdentities answers which pointer identities a freshly built service value holds, transitively, so the teardown can order a provider that captured its collaborator. It is walked where the value is filed, not at teardown. Every word is read as memory another goroutine may be writing, so only pointer words and the struct and array layouts around them are read: interfaces, strings, slices, maps, funcs, chans and unsafe pointers are never entered, and a collaborator reached only through one of them keeps the order it had. The walk is breadth-first, so every pointer is entered once, at its shallowest depth. */
func heldPointerIdentities(root any) []heldPointer {
    found := make([]heldPointer, 0)
    seen := make(map[pointerIdentity]struct{})

    type walkItem struct {
        value reflect.Value
        depth int
    }

    queue := []walkItem{{value: reflect.ValueOf(root), depth: 0}}
    queued := 1

    /* the budget is charged as an item is queued, so the queue never holds more than the budget */
    enqueue := func(value reflect.Value, depth int) {
        if queued >= teardownWalkNodeBudget {
            return
        }

        queued = queued + 1
        queue = append(queue, walkItem{value: value, depth: depth})
    }

    for 0 < len(queue) {
        item := queue[0]
        queue = queue[1:]

        value := item.value
        if false == value.IsValid() {
            continue
        }

        /* the children of a node at the depth limit are neither computed nor queued */
        childrenInReach := item.depth < teardownWalkDepthLimit

        switch value.Kind() {
        case reflect.Pointer:
            /* the pointer word is read once, since a concurrent writer may replace it between two reads */
            target := value.Elem()
            if false == target.IsValid() {
                continue
            }

            identity := pointerIdentity{pointer: target.UnsafeAddr(), valueType: value.Type()}

            /* a pointer already seen was seen at this depth or a shallower one, the queue being ordered by depth */
            if _, already := seen[identity]; true == already {
                continue
            }

            seen[identity] = struct{}{}

            found = append(found, heldPointer{identity: identity, keepAlive: target})

            if true == childrenInReach {
                enqueue(target, item.depth+1)
            }
        case reflect.Struct:
            if false == childrenInReach {
                continue
            }

            fieldCount := value.NumField()
            if fieldCount > teardownWalkElementLimit {
                fieldCount = teardownWalkElementLimit
            }

            for fieldIndex := 0; fieldIndex < fieldCount; fieldIndex = fieldIndex + 1 {
                field := value.Field(fieldIndex)

                /* a field whose type can hold no identity is not queued */
                if false == typeCanHoldIdentity(field.Type()) {
                    continue
                }

                enqueue(field, item.depth+1)
            }
        case reflect.Array:
            if false == childrenInReach {
                continue
            }

            /* an array of elements that can hold no identity is one node */
            if false == typeCanHoldIdentity(value.Type().Elem()) {
                continue
            }

            elementCount := value.Len()
            if elementCount > teardownWalkElementLimit {
                elementCount = teardownWalkElementLimit
            }

            for elementIndex := 0; elementIndex < elementCount; elementIndex = elementIndex + 1 {
                enqueue(value.Index(elementIndex), item.depth+1)
            }
        }
    }

    return found
}

/* typeCanHoldIdentity answers, once per type, whether the walk could find a pointer identity below a value of it: through a pointer, any struct field or an array element, never through what the walk does not enter. */
func typeCanHoldIdentity(valueType reflect.Type) bool {
    if answer, known := typeIdentityCapability.Load(valueType); true == known {
        return answer.(bool)
    }

    answer := typeCanHoldIdentityUncached(valueType)

    typeIdentityCapability.Store(valueType, answer)

    return answer
}

var typeIdentityCapability sync.Map

/* typeCanHoldIdentityUncached descends through arrays and struct fields only; Go refuses a struct holding itself by value, and a pointer answers true before any recursion. */
func typeCanHoldIdentityUncached(valueType reflect.Type) bool {
    switch valueType.Kind() {
    case reflect.Pointer:
        return true
    case reflect.Array:
        return typeCanHoldIdentityUncached(valueType.Elem())
    case reflect.Struct:
        for fieldIndex := 0; fieldIndex < valueType.NumField(); fieldIndex = fieldIndex + 1 {
            if true == typeCanHoldIdentityUncached(valueType.Field(fieldIndex).Type) {
                return true
            }
        }

        return false
    }

    return false
}

/* recordHeldIdentitiesLocked keeps what a service holds against its node, only while the waves are armed. One pointer value is walked once however many nodes it is filed under and the record is shared; a re-filed node replaces its record. */
func (instance *container) recordHeldIdentitiesLocked(nodeKey string, value any) {
    if false == instance.teardownInWaves {
        return
    }

    if nil == instance.heldIdentitiesByNodeKey {
        instance.heldIdentitiesByNodeKey = make(map[string][]heldPointer)
        instance.heldIdentitiesByValue = make(map[pointerIdentity][]heldPointer)
    }

    valueKey, hasPointer := pointerKeyOf(value)
    if true == hasPointer {
        if walked, already := instance.heldIdentitiesByValue[valueKey]; true == already {
            instance.heldIdentitiesByNodeKey[nodeKey] = walked

            return
        }
    }

    walked := heldPointerIdentities(value)

    instance.heldIdentitiesByNodeKey[nodeKey] = walked

    if true == hasPointer {
        instance.heldIdentitiesByValue[valueKey] = walked
    }
}

/* recordHeldIdentitiesOfBuiltServicesLocked walks what was built before arming; everything built later is walked where it is filed. */
func (instance *container) recordHeldIdentitiesOfBuiltServicesLocked() {
    for serviceName, value := range instance.instances {
        instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)
    }

    for registeredType, value := range instance.typeInstances {
        instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value)
    }
}

/* teardownEdgesFromHeldIdentitiesLocked turns the recorded identities into teardown edges: a service holding another service's pointer depends on it. A node holding itself is skipped. An inferred edge on a ring of the combined graph, in canonical keys and among created nodes, is not written, since it contradicts an ordering or is none; the pairs held both ways are handed back so a wave keeps them apart. Nothing is written into the container's graph: the edges belong to the plan being computed. */
func (instance *container) teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey map[string]any, representativeOf map[string]string, canonicalEdges map[string]map[string]struct{}) (inferred [][2]string, unordered [][2]string) {
    if 0 == len(instance.heldIdentitiesByNodeKey) {
        return nil, nil
    }

    nodeKeyOfIdentity := make(map[pointerIdentity]string, len(valueOfNodeKey))

    /* a zero-size pointer names no identity, since every zero-size allocation shares one address */
    for nodeKey, value := range valueOfNodeKey {
        identity, hasPointer := pointerKeyOf(value)
        if false == hasPointer || true == isZeroSizePointerIdentity(identity) {
            continue
        }

        nodeKeyOfIdentity[identity] = nodeKey
    }

    heldCanonical := make(map[[2]string]struct{}, len(valueOfNodeKey))

    for nodeKey := range valueOfNodeKey {
        for _, held := range instance.heldIdentitiesByNodeKey[nodeKey] {
            heldNodeKey, isNode := nodeKeyOfIdentity[held.identity]
            if false == isNode {
                continue
            }

            canonicalDependent, dependentCreated := representativeOf[nodeKey]
            canonicalDependency, dependencyCreated := representativeOf[heldNodeKey]

            if false == dependentCreated || false == dependencyCreated || canonicalDependent == canonicalDependency {
                continue
            }

            heldCanonical[[2]string{canonicalDependent, canonicalDependency}] = struct{}{}
        }
    }

    isMutual := func(edge [2]string) bool {
        _, mutual := heldCanonical[[2]string{edge[1], edge[0]}]

        return mutual
    }

    /* the way back is looked for in the plan's own canonical graph, handed in, plus the held edges that are no pair */
    combined := make(map[string]map[string]struct{}, len(canonicalEdges)+len(heldCanonical))

    addCombined := func(dependentKey string, dependencyKey string) {
        if nil == combined[dependentKey] {
            combined[dependentKey] = make(map[string]struct{})
        }

        combined[dependentKey][dependencyKey] = struct{}{}
    }

    for dependentKey, dependencySet := range canonicalEdges {
        for dependencyKey := range dependencySet {
            addCombined(dependentKey, dependencyKey)
        }
    }

    /* a pair held both ways is no ordering, so neither edge enters the graph */
    for edge := range heldCanonical {
        if false == isMutual(edge) {
            addCombined(edge[0], edge[1])
        }
    }

    /* a way back exists exactly when the two stand in one ring of the combined graph; the rings are computed once */
    combinedNodes := make(map[string]struct{}, len(combined))

    for dependentKey, dependencySet := range combined {
        combinedNodes[dependentKey] = struct{}{}

        for dependencyKey := range dependencySet {
            combinedNodes[dependencyKey] = struct{}{}
        }
    }

    ringOf := make(map[string]int)

    for ringIndex, ring := range stronglyConnectedRings(combinedNodes, combined) {
        for _, member := range ring {
            ringOf[member] = ringIndex
        }
    }

    liesOnARing := func(edge [2]string) bool {
        dependentRing, dependentOnRing := ringOf[edge[0]]
        dependencyRing, dependencyOnRing := ringOf[edge[1]]

        return true == dependentOnRing && true == dependencyOnRing && dependentRing == dependencyRing
    }

    inferred = make([][2]string, 0)
    unordered = make([][2]string, 0)

    for edge := range heldCanonical {
        if true == isMutual(edge) || true == liesOnARing(edge) {
            unordered = append(unordered, edge)

            continue
        }

        inferred = append(inferred, edge)
    }

    sortNodeKeyPairs(inferred)
    sortNodeKeyPairs(unordered)

    return inferred, unordered
}

func sortNodeKeyPairs(pairs [][2]string) {
    slices.SortFunc(pairs, func(left [2]string, right [2]string) int {
        if left[0] != right[0] {
            return strings.Compare(left[0], right[0])
        }

        return strings.Compare(left[1], right[1])
    })
}
