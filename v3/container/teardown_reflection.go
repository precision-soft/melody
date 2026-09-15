package container

import (
    "reflect"
    "sort"
    "sync"
)

const teardownWalkDepthLimit = 12

const teardownWalkElementLimit = 256

const teardownWalkNodeBudget = 65536

type heldPointer struct {
    identity  pointerIdentity
    keepAlive reflect.Value
}

func heldPointerIdentities(root any) []heldPointer {
    found := make([]heldPointer, 0)
    seen := make(map[pointerIdentity]struct{})

    type walkItem struct {
        value reflect.Value
        depth int
    }

    queue := []walkItem{{value: reflect.ValueOf(root), depth: 0}}
    queued := 1

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

        childrenInReach := item.depth < teardownWalkDepthLimit

        switch value.Kind() {
        case reflect.Pointer:

            target := value.Elem()
            if false == target.IsValid() {
                continue
            }

            identity := pointerIdentity{pointer: target.UnsafeAddr(), valueType: value.Type()}

            if _, already := seen[identity]; true == already {
                continue
            }

            seen[identity] = struct{}{}

            if false == isZeroSizePointerIdentity(identity) {
                found = append(found, heldPointer{identity: identity, keepAlive: target})
            }

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

                if false == typeCanHoldIdentity(field.Type()) {
                    continue
                }

                enqueue(field, item.depth+1)
            }
        case reflect.Array:
            if false == childrenInReach {
                continue
            }

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

func typeCanHoldIdentity(valueType reflect.Type) bool {
    if answer, known := typeIdentityCapability.Load(valueType); true == known {
        return answer.(bool)
    }

    answer := typeCanHoldIdentityUncached(valueType)

    typeIdentityCapability.Store(valueType, answer)

    return answer
}

var typeIdentityCapability sync.Map

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

func (instance *container) recordHeldIdentitiesOfBuiltServicesLocked() {
    for serviceName, value := range instance.instances {
        instance.recordHeldIdentitiesLocked(containerNameNodeKey(serviceName), value)
    }

    for registeredType, value := range instance.typeInstances {
        instance.recordHeldIdentitiesLocked(containerTypeNodeKey(registeredType), value)
    }
}

func (instance *container) teardownEdgesFromHeldIdentitiesLocked(valueOfNodeKey map[string]any, representativeOf map[string]string, canonicalEdges map[string]map[string]struct{}) (inferred [][2]string, unordered [][2]string) {
    if 0 == len(instance.heldIdentitiesByNodeKey) {
        return nil, nil
    }

    nodeKeyOfIdentity := make(map[pointerIdentity]string, len(valueOfNodeKey))

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

    for edge := range heldCanonical {
        if false == isMutual(edge) {
            addCombined(edge[0], edge[1])
        }
    }

    inferred = make([][2]string, 0)
    unordered = make([][2]string, 0)

    for edge := range heldCanonical {
        if true == isMutual(edge) || true == reachesNodeKey(combined, edge[1], edge[0]) {
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
    sort.Slice(
        pairs,
        func(leftIndex int, rightIndex int) bool {
            if pairs[leftIndex][0] != pairs[rightIndex][0] {
                return pairs[leftIndex][0] < pairs[rightIndex][0]
            }

            return pairs[leftIndex][1] < pairs[rightIndex][1]
        },
    )
}

func reachesNodeKey(edges map[string]map[string]struct{}, fromKey string, toKey string) bool {
    visited := make(map[string]struct{})
    pending := []string{fromKey}

    for 0 < len(pending) {
        current := pending[len(pending)-1]
        pending = pending[:len(pending)-1]

        if current == toKey {
            return true
        }

        if _, already := visited[current]; true == already {
            continue
        }

        visited[current] = struct{}{}

        for next := range edges[current] {
            pending = append(pending, next)
        }
    }

    return false
}
